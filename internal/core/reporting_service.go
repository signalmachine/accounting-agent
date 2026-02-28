package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// ── Report types ──────────────────────────────────────────────────────────────

// StatementLine represents a single journal line in an account statement.
// RunningBalance is the cumulative net-debit position after this line
// (positive = net debit, negative = net credit).
type StatementLine struct {
	PostingDate    string
	DocumentDate   string
	Narration      string
	Reference      string
	Debit          decimal.Decimal
	Credit         decimal.Decimal
	RunningBalance decimal.Decimal
}

// AccountLine is a single account entry in a P&L or Balance Sheet report.
// Balance is expressed in the sign convention for that section:
//   - P&L Revenue:  positive = income received
//   - P&L Expenses: positive = cost incurred
//   - BS Assets:    positive = net debit (normal asset balance)
//   - BS Liabilities/Equity: positive = net credit (normal balance)
type AccountLine struct {
	Code    string
	Name    string
	Balance decimal.Decimal
}

// PLReport is the Profit & Loss report for one calendar period.
type PLReport struct {
	CompanyCode string
	Year        int
	Month       int
	Revenue     []AccountLine   // credit-dominant accounts (type = 'revenue')
	Expenses    []AccountLine   // debit-dominant accounts  (type = 'expense')
	NetIncome   decimal.Decimal // Revenue total - Expenses total
}

// BSReport is the Balance Sheet report as of a given date.
// IsBalanced is true when TotalAssets == TotalLiabilities + TotalEquity,
// which holds for any correctly posted double-entry ledger when all
// income/expense has been closed to retained earnings.
type BSReport struct {
	CompanyCode      string
	AsOfDate         string
	Assets           []AccountLine
	Liabilities      []AccountLine
	Equity           []AccountLine
	TotalAssets      decimal.Decimal
	TotalLiabilities decimal.Decimal
	TotalEquity      decimal.Decimal
	IsBalanced       bool
}

// ── Interface ─────────────────────────────────────────────────────────────────

// ReportingService provides read-only reporting queries over the ledger.
type ReportingService interface {
	// GetAccountStatement returns all journal lines for an account within the
	// given date range, ordered by posting_date ASC then entry id ASC.
	// fromDate and toDate are optional — pass empty string for no bound.
	// RunningBalance on each line is the cumulative (debit_base − credit_base).
	GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) ([]StatementLine, error)

	// GetProfitAndLoss returns the P&L report for the given year and month.
	// Revenue balances are expressed as positive credit-minus-debit amounts.
	// Expense balances are expressed as positive debit-minus-credit amounts.
	GetProfitAndLoss(ctx context.Context, companyCode string, year, month int) (*PLReport, error)

	// GetBalanceSheet returns the Balance Sheet as of the given date.
	// If asOfDate is empty, today's date is used.
	GetBalanceSheet(ctx context.Context, companyCode, asOfDate string) (*BSReport, error)

	// RefreshViews refreshes all materialized reporting views
	// (mv_account_period_balances and mv_trial_balance).
	RefreshViews(ctx context.Context) error
}

// ── Implementation ────────────────────────────────────────────────────────────

type reportingService struct {
	pool *pgxpool.Pool
}

// NewReportingService constructs a ReportingService backed by the given pool.
func NewReportingService(pool *pgxpool.Pool) ReportingService {
	return &reportingService{pool: pool}
}

// resolveCompanyID looks up the integer primary key for a company code.
func (s *reportingService) resolveCompanyID(ctx context.Context, companyCode string) (int, error) {
	var id int
	if err := s.pool.QueryRow(ctx,
		"SELECT id FROM companies WHERE company_code = $1", companyCode,
	).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("company %s not found", companyCode)
		}
		return 0, fmt.Errorf("failed to resolve company: %w", err)
	}
	return id, nil
}

// ── GetAccountStatement ───────────────────────────────────────────────────────

func (s *reportingService) GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) ([]StatementLine, error) {
	companyID, err := s.resolveCompanyID(ctx, companyCode)
	if err != nil {
		return nil, err
	}

	q := `
		SELECT je.posting_date::text,
		       je.document_date::text,
		       je.narration,
		       COALESCE(je.reference_id, ''),
		       jl.debit_base,
		       jl.credit_base
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id
		JOIN accounts a         ON a.id  = jl.account_id
		WHERE je.company_id = $1
		  AND a.code = $2`

	args := []any{companyID, accountCode}
	if fromDate != "" {
		args = append(args, fromDate)
		q += fmt.Sprintf(" AND je.posting_date >= $%d::date", len(args))
	}
	if toDate != "" {
		args = append(args, toDate)
		q += fmt.Sprintf(" AND je.posting_date <= $%d::date", len(args))
	}
	q += " ORDER BY je.posting_date ASC, je.id ASC"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query account statement: %w", err)
	}
	defer rows.Close()

	var lines []StatementLine
	running := decimal.Zero
	for rows.Next() {
		var sl StatementLine
		if err := rows.Scan(
			&sl.PostingDate, &sl.DocumentDate, &sl.Narration, &sl.Reference,
			&sl.Debit, &sl.Credit,
		); err != nil {
			return nil, fmt.Errorf("failed to scan statement line: %w", err)
		}
		running = running.Add(sl.Debit).Sub(sl.Credit)
		sl.RunningBalance = running
		lines = append(lines, sl)
	}
	return lines, nil
}

// ── GetProfitAndLoss ──────────────────────────────────────────────────────────

// GetProfitAndLoss returns the P&L for the given year/month by querying
// journal_lines directly so the result is always current (not dependent on
// a materialized view refresh cycle).
func (s *reportingService) GetProfitAndLoss(ctx context.Context, companyCode string, year, month int) (*PLReport, error) {
	companyID, err := s.resolveCompanyID(ctx, companyCode)
	if err != nil {
		return nil, err
	}

	// Subquery aggregates only lines whose entry falls in the target period.
	const q = `
		SELECT a.code, a.name, a.type,
		       COALESCE(s.debit_total,  0) AS debit_total,
		       COALESCE(s.credit_total, 0) AS credit_total
		FROM accounts a
		JOIN companies c ON c.id = a.company_id
		LEFT JOIN (
		    SELECT jl.account_id,
		           SUM(jl.debit_base)  AS debit_total,
		           SUM(jl.credit_base) AS credit_total
		    FROM journal_lines jl
		    JOIN journal_entries je ON je.id = jl.entry_id
		    WHERE je.company_id = $1
		      AND EXTRACT(YEAR  FROM je.posting_date)::int = $2
		      AND EXTRACT(MONTH FROM je.posting_date)::int = $3
		    GROUP BY jl.account_id
		) s ON s.account_id = a.id
		WHERE c.id = $1
		  AND a.type IN ('revenue', 'expense')
		ORDER BY a.type, a.code`

	rows, err := s.pool.Query(ctx, q, companyID, year, month)
	if err != nil {
		return nil, fmt.Errorf("failed to query P&L: %w", err)
	}
	defer rows.Close()

	report := &PLReport{CompanyCode: companyCode, Year: year, Month: month}
	var totalRevenue, totalExpenses decimal.Decimal

	for rows.Next() {
		var code, name, accType string
		var debit, credit decimal.Decimal
		if err := rows.Scan(&code, &name, &accType, &debit, &credit); err != nil {
			return nil, fmt.Errorf("failed to scan P&L row: %w", err)
		}

		switch accType {
		case "revenue":
			// Positive balance = credit > debit (income received).
			bal := credit.Sub(debit)
			report.Revenue = append(report.Revenue, AccountLine{Code: code, Name: name, Balance: bal})
			totalRevenue = totalRevenue.Add(bal)
		case "expense":
			// Positive balance = debit > credit (cost incurred).
			bal := debit.Sub(credit)
			report.Expenses = append(report.Expenses, AccountLine{Code: code, Name: name, Balance: bal})
			totalExpenses = totalExpenses.Add(bal)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("P&L row iteration error: %w", err)
	}

	report.NetIncome = totalRevenue.Sub(totalExpenses)
	return report, nil
}

// ── GetBalanceSheet ───────────────────────────────────────────────────────────

// GetBalanceSheet returns the Balance Sheet as of the given date by querying
// journal_lines directly with a date ceiling filter.
func (s *reportingService) GetBalanceSheet(ctx context.Context, companyCode, asOfDate string) (*BSReport, error) {
	companyID, err := s.resolveCompanyID(ctx, companyCode)
	if err != nil {
		return nil, err
	}

	if asOfDate == "" {
		asOfDate = time.Now().Format("2006-01-02")
	}

	// Subquery aggregates lines whose entry was posted on or before asOfDate.
	const q = `
		SELECT a.code, a.name, a.type,
		       COALESCE(s.total_debit,  0) - COALESCE(s.total_credit, 0) AS net_balance
		FROM accounts a
		JOIN companies c ON c.id = a.company_id
		LEFT JOIN (
		    SELECT jl.account_id,
		           SUM(jl.debit_base)  AS total_debit,
		           SUM(jl.credit_base) AS total_credit
		    FROM journal_lines jl
		    JOIN journal_entries je ON je.id = jl.entry_id
		    WHERE je.company_id = $1
		      AND je.posting_date <= $2::date
		    GROUP BY jl.account_id
		) s ON s.account_id = a.id
		WHERE c.id = $1
		  AND a.type IN ('asset', 'liability', 'equity')
		ORDER BY a.type, a.code`

	rows, err := s.pool.Query(ctx, q, companyID, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query balance sheet: %w", err)
	}
	defer rows.Close()

	report := &BSReport{CompanyCode: companyCode, AsOfDate: asOfDate}

	for rows.Next() {
		var code, name, accType string
		var netBalance decimal.Decimal // debit - credit
		if err := rows.Scan(&code, &name, &accType, &netBalance); err != nil {
			return nil, fmt.Errorf("failed to scan balance sheet row: %w", err)
		}

		switch accType {
		case "asset":
			// Assets carry debit balances: positive net = normal.
			report.Assets = append(report.Assets, AccountLine{Code: code, Name: name, Balance: netBalance})
			report.TotalAssets = report.TotalAssets.Add(netBalance)
		case "liability":
			// Liabilities carry credit balances: negate so positive = normal liability.
			bal := netBalance.Neg()
			report.Liabilities = append(report.Liabilities, AccountLine{Code: code, Name: name, Balance: bal})
			report.TotalLiabilities = report.TotalLiabilities.Add(bal)
		case "equity":
			// Equity carries credit balances: same convention as liabilities.
			bal := netBalance.Neg()
			report.Equity = append(report.Equity, AccountLine{Code: code, Name: name, Balance: bal})
			report.TotalEquity = report.TotalEquity.Add(bal)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("balance sheet row iteration error: %w", err)
	}

	report.IsBalanced = report.TotalAssets.Equal(report.TotalLiabilities.Add(report.TotalEquity))
	return report, nil
}

// ── RefreshViews ──────────────────────────────────────────────────────────────

// RefreshViews refreshes both materialized reporting views concurrently.
// Requires that the unique indexes exist (created in migrations 014 and 015).
func (s *reportingService) RefreshViews(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx,
		"REFRESH MATERIALIZED VIEW CONCURRENTLY mv_account_period_balances",
	); err != nil {
		return fmt.Errorf("refresh mv_account_period_balances: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		"REFRESH MATERIALIZED VIEW CONCURRENTLY mv_trial_balance",
	); err != nil {
		return fmt.Errorf("refresh mv_trial_balance: %w", err)
	}
	return nil
}
