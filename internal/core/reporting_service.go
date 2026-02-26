package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

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

// ReportingService provides read-only reporting queries over the ledger.
type ReportingService interface {
	// GetAccountStatement returns all journal lines for an account within the
	// given date range, ordered by posting_date ASC then entry id ASC.
	// fromDate and toDate are optional — pass empty string for no bound.
	// RunningBalance on each line is the cumulative (debit_base − credit_base).
	GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) ([]StatementLine, error)
}

type reportingService struct {
	pool *pgxpool.Pool
}

// NewReportingService constructs a ReportingService backed by the given pool.
func NewReportingService(pool *pgxpool.Pool) ReportingService {
	return &reportingService{pool: pool}
}

func (s *reportingService) GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) ([]StatementLine, error) {
	var companyID int
	if err := s.pool.QueryRow(ctx,
		"SELECT id FROM companies WHERE company_code = $1", companyCode,
	).Scan(&companyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("company %s not found", companyCode)
		}
		return nil, fmt.Errorf("failed to resolve company: %w", err)
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
