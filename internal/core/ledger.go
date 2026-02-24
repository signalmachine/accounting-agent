package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type LedgerService interface {
	Commit(ctx context.Context, proposal Proposal) error
	Validate(ctx context.Context, proposal Proposal) error
	GetBalances(ctx context.Context, companyCode string) ([]AccountBalance, error)
	Reverse(ctx context.Context, entryID int, reasoning string) error
}

type Ledger struct {
	pool       *pgxpool.Pool
	docService DocumentService
}

func NewLedger(pool *pgxpool.Pool, docService DocumentService) *Ledger {
	return &Ledger{pool: pool, docService: docService}
}

func (l *Ledger) Commit(ctx context.Context, proposal Proposal) error {
	return l.execute(ctx, proposal, true)
}

func (l *Ledger) Validate(ctx context.Context, proposal Proposal) error {
	return l.execute(ctx, proposal, false)
}

// CommitInTx executes a proposal within an already-open transaction.
// It does NOT call tx.Begin() or tx.Commit() — the caller owns the TX.
// Use this when the ledger commit must be atomic with other DB operations in the caller's TX.
func (l *Ledger) CommitInTx(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	return l.executeCore(ctx, tx, proposal, true)
}

func (l *Ledger) execute(ctx context.Context, proposal Proposal, commit bool) error {
	// 1. Structural Validation
	if err := proposal.Validate(); err != nil {
		return fmt.Errorf("proposal validation failed: %w", err)
	}

	// 2. Database Transaction
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := l.executeCore(ctx, tx, proposal, commit); err != nil {
		return err
	}

	// 3. Commit if requested
	if commit {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	return nil
}

// executeCore performs all ledger operations within the provided TX.
// It does not Begin or Commit — that is the caller's responsibility.
func (l *Ledger) executeCore(ctx context.Context, tx pgx.Tx, proposal Proposal, createDoc bool) error {
	// Validation (re-run even for CommitInTx callers — cheap and safe)
	if err := proposal.Validate(); err != nil {
		return fmt.Errorf("proposal validation failed: %w", err)
	}

	// Resolve Company ID from Company Code
	var companyID int
	err := tx.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", proposal.CompanyCode).Scan(&companyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("company code %s not found", proposal.CompanyCode)
		}
		return fmt.Errorf("failed to fetch company ID: %w", err)
	}

	var documentNumber *string
	var referenceType *string

	if createDoc {
		// Create Draft Document and Post — within the caller's transaction.
		var draftDocID int
		err = tx.QueryRow(ctx, `
			INSERT INTO documents (company_id, type_code, status, financial_year, branch_id)
			VALUES ($1, $2, $3, NULL, NULL)
			RETURNING id
		`, companyID, proposal.DocumentTypeCode, string(DocumentStatusDraft)).Scan(&draftDocID)
		if err != nil {
			return fmt.Errorf("failed to create draft document: %w", err)
		}

		if err = l.docService.PostDocumentTx(ctx, tx, draftDocID); err != nil {
			return fmt.Errorf("failed to post document: %w", err)
		}

		err = tx.QueryRow(ctx, "SELECT document_number FROM documents WHERE id = $1", draftDocID).Scan(&documentNumber)
		if err != nil {
			return fmt.Errorf("failed to retrieve posted document number: %w", err)
		}

		refType := "DOCUMENT"
		referenceType = &refType
	}

	// Insert Journal Entry
	var entryID int
	if proposal.IdempotencyKey != "" {
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_entries (company_id, narration, posting_date, document_date, reasoning, reference_type, reference_id, idempotency_key, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
			ON CONFLICT (idempotency_key) DO NOTHING
			RETURNING id
		`, companyID, proposal.Summary, proposal.PostingDate, proposal.DocumentDate, proposal.Reasoning, referenceType, documentNumber, proposal.IdempotencyKey).Scan(&entryID)
	} else {
		err = tx.QueryRow(ctx, `
			INSERT INTO journal_entries (company_id, narration, posting_date, document_date, reasoning, reference_type, reference_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			RETURNING id
		`, companyID, proposal.Summary, proposal.PostingDate, proposal.DocumentDate, proposal.Reasoning, referenceType, documentNumber).Scan(&entryID)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("duplicate proposal: idempotency key %s already exists", proposal.IdempotencyKey)
		}
		return fmt.Errorf("failed to insert journal entry: %w", err)
	}

	// Insert Journal Lines
	// Rate is header-level: all lines share the same TransactionCurrency and ExchangeRate (SAP model).
	rate, _ := decimal.NewFromString(proposal.ExchangeRate)

	for _, line := range proposal.Lines {
		var accountID int
		err := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE company_id = $1 AND code = $2", companyID, line.AccountCode).Scan(&accountID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("account code %s not found for company %s", line.AccountCode, proposal.CompanyCode)
			}
			return fmt.Errorf("failed to fetch account ID for code %s: %w", line.AccountCode, err)
		}

		amt, _ := decimal.NewFromString(line.Amount)
		baseAmt := amt.Mul(rate)

		var debitBase, creditBase decimal.Decimal
		if line.IsDebit {
			debitBase = baseAmt
			creditBase = decimal.Zero
		} else {
			debitBase = decimal.Zero
			creditBase = baseAmt
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO journal_lines (entry_id, account_id, transaction_currency, exchange_rate, amount_transaction, debit_base, credit_base)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, entryID, accountID, proposal.TransactionCurrency, proposal.ExchangeRate, line.Amount, debitBase, creditBase)
		if err != nil {
			return fmt.Errorf("failed to insert journal line: %w", err)
		}
	}

	return nil
}

type AccountBalance struct {
	Code    string
	Name    string
	Balance decimal.Decimal
}

func (l *Ledger) GetBalances(ctx context.Context, companyCode string) ([]AccountBalance, error) {
	var companyID int
	err := l.pool.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", companyCode).Scan(&companyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("company code %s not found", companyCode)
		}
		return nil, fmt.Errorf("failed to fetch company ID: %w", err)
	}

	rows, err := l.pool.Query(ctx, `
		SELECT a.code, a.name, COALESCE(SUM(jl.debit_base), 0) - COALESCE(SUM(jl.credit_base), 0) AS balance
		FROM accounts a
		LEFT JOIN journal_lines jl ON a.id = jl.account_id
		LEFT JOIN journal_entries je ON jl.entry_id = je.id
		WHERE a.company_id = $1
		GROUP BY a.id, a.code, a.name
		ORDER BY a.code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var balances []AccountBalance
	for rows.Next() {
		var b AccountBalance
		if err := rows.Scan(&b.Code, &b.Name, &b.Balance); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		balances = append(balances, b)
	}
	return balances, nil
}

func (l *Ledger) Reverse(ctx context.Context, entryID int, reasoning string) error {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var narration string
	var companyID int
	err = tx.QueryRow(ctx, "SELECT company_id, narration FROM journal_entries WHERE id = $1", entryID).Scan(&companyID, &narration)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("entry %d not found", entryID)
		}
		return fmt.Errorf("failed to fetch entry %d: %w", entryID, err)
	}

	var count int
	err = tx.QueryRow(ctx, "SELECT count(*) FROM journal_entries WHERE reversed_entry_id = $1", entryID).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check reversal status: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("entry %d is already reversed", entryID)
	}

	reversalNarration := fmt.Sprintf("Reversal of entry %d: %s", entryID, narration)
	var newEntryID int
	// For reversals, use the same posting_date and document_date as the original (or pass new ones if API expands in future)
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries (company_id, narration, posting_date, document_date, reasoning, reversed_entry_id, created_at)
		SELECT company_id, $1, posting_date, document_date, $2, $3, NOW()
		FROM journal_entries WHERE id = $3
		RETURNING id
	`, reversalNarration, reasoning, entryID).Scan(&newEntryID)
	if err != nil {
		return fmt.Errorf("failed to insert reversal entry: %w", err)
	}

	rows, err := tx.Query(ctx, "SELECT account_id, transaction_currency, exchange_rate, amount_transaction, debit_base, credit_base FROM journal_lines WHERE entry_id = $1", entryID)
	if err != nil {
		return fmt.Errorf("failed to fetch lines for entry %d: %w", entryID, err)
	}
	defer rows.Close()

	type lineData struct {
		accountID           int
		transactionCurrency string
		exchangeRate        decimal.Decimal
		amountTransaction   decimal.Decimal
		debitBase           decimal.Decimal
		creditBase          decimal.Decimal
	}
	var lines []lineData

	for rows.Next() {
		var l lineData
		if err := rows.Scan(&l.accountID, &l.transactionCurrency, &l.exchangeRate, &l.amountTransaction, &l.debitBase, &l.creditBase); err != nil {
			return fmt.Errorf("failed to scan line: %w", err)
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating lines: %w", err)
	}

	for _, line := range lines {
		// Invert debits and credits for the reversal
		_, err := tx.Exec(ctx, `
			INSERT INTO journal_lines (entry_id, account_id, transaction_currency, exchange_rate, amount_transaction, debit_base, credit_base)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, newEntryID, line.accountID, line.transactionCurrency, line.exchangeRate.String(), line.amountTransaction, line.creditBase, line.debitBase)
		if err != nil {
			return fmt.Errorf("failed to insert inverted line: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit reversal: %w", err)
	}

	return nil
}
