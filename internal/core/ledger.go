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
}

type Ledger struct {
	pool *pgxpool.Pool
}

func NewLedger(pool *pgxpool.Pool) *Ledger {
	return &Ledger{pool: pool}
}

func (l *Ledger) Commit(ctx context.Context, proposal Proposal) error {
	return l.execute(ctx, proposal, true)
}

func (l *Ledger) Validate(ctx context.Context, proposal Proposal) error {
	return l.execute(ctx, proposal, false)
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

	// 3. Resolve Account Codes to IDs
	accountMap := make(map[string]int)
	for _, line := range proposal.Lines {
		var id int
		err := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE code = $1", line.AccountCode).Scan(&id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("account code not found: %s", line.AccountCode)
			}
			return fmt.Errorf("failed to lookup account %s: %w", line.AccountCode, err)
		}
		accountMap[line.AccountCode] = id
	}

	// 4. Insert Journal Entry
	var entryID int
	err = tx.QueryRow(ctx, `
		INSERT INTO journal_entries (narration, reasoning, created_at)
		VALUES ($1, $2, NOW())
		RETURNING id
	`, proposal.Summary, proposal.Reasoning).Scan(&entryID)
	if err != nil {
		return fmt.Errorf("failed to insert journal entry: %w", err)
	}

	// 5. Insert Journal Lines
	for _, line := range proposal.Lines {
		_, err := tx.Exec(ctx, `
			INSERT INTO journal_lines (entry_id, account_id, debit, credit)
			VALUES ($1, $2, $3, $4)
		`, entryID, accountMap[line.AccountCode], line.Debit, line.Credit)
		if err != nil {
			return fmt.Errorf("failed to insert journal line: %w", err)
		}
	}

	// 6. Commit if requested
	if commit {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	return nil
}

type AccountBalance struct {
	Code    string
	Name    string
	Balance decimal.Decimal
}

func (l *Ledger) GetBalances(ctx context.Context) ([]AccountBalance, error) {
	rows, err := l.pool.Query(ctx, `
		SELECT a.code, a.name, COALESCE(SUM(jl.debit), 0) - COALESCE(SUM(jl.credit), 0) as balance 
		FROM accounts a 
		LEFT JOIN journal_lines jl ON a.id = jl.account_id 
		GROUP BY a.id, a.code, a.name 
		ORDER BY a.code
	`)
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
