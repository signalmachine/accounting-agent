// restore-seed is a one-shot tool to restore the live database seed data.
// Run it when the chart of accounts or company data has been accidentally wiped.
//
// Usage: go run ./cmd/restore-seed
package main

import (
	"context"
	"log"
	"os"

	"accounting-agent/internal/db"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	log.Println("Clearing test journal entries and lines...")
	_, err = tx.Exec(ctx, `
		DELETE FROM journal_lines WHERE entry_id IN (
			SELECT id FROM journal_entries WHERE company_id IN (
				SELECT id FROM companies WHERE company_code = '1000'
			)
		);
		DELETE FROM journal_entries WHERE company_id IN (
			SELECT id FROM companies WHERE company_code = '1000'
		);
	`)
	if err != nil {
		log.Fatalf("Failed to clear journal data: %v", err)
	}

	log.Println("Removing test accounts...")
	_, err = tx.Exec(ctx, `
		DELETE FROM accounts WHERE company_id IN (
			SELECT id FROM companies WHERE company_code = '1000'
		);
	`)
	if err != nil {
		log.Fatalf("Failed to delete test accounts: %v", err)
	}

	log.Println("Restoring company...")
	_, err = tx.Exec(ctx, `
		INSERT INTO companies (company_code, name, base_currency)
		VALUES ('1000', 'Local Operations India', 'INR')
		ON CONFLICT (company_code) DO UPDATE
		  SET name = EXCLUDED.name,
		      base_currency = EXCLUDED.base_currency;
	`)
	if err != nil {
		log.Fatalf("Failed to restore company: %v", err)
	}

	log.Println("Restoring chart of accounts...")
	_, err = tx.Exec(ctx, `
		INSERT INTO accounts (company_id, code, name, type)
		SELECT c.id, a.code, a.name, a.type
		FROM companies c
		CROSS JOIN (VALUES
		    ('1000', 'Cash',                'asset'),
		    ('1100', 'Bank Account',        'asset'),
		    ('1200', 'Accounts Receivable', 'asset'),
		    ('1300', 'Furniture & Fixtures','asset'),
		    ('1400', 'Inventory',           'asset'),
		    ('2000', 'Accounts Payable',    'liability'),
		    ('2100', 'Short-Term Loans',    'liability'),
		    ('3000', 'Owner Capital',       'equity'),
		    ('3100', 'Retained Earnings',   'equity'),
		    ('4000', 'Sales Revenue',       'revenue'),
		    ('4100', 'Service Revenue',     'revenue'),
		    ('5000', 'Cost of Goods Sold',  'expense'),
		    ('5100', 'Rent Expense',        'expense'),
		    ('5200', 'Salary Expense',      'expense'),
		    ('5300', 'Utilities Expense',   'expense')
		) AS a(code, name, type)
		WHERE c.company_code = '1000'
		ON CONFLICT (company_id, code) DO UPDATE
		  SET name = EXCLUDED.name,
		      type = EXCLUDED.type;
	`)
	if err != nil {
		log.Fatalf("Failed to restore accounts: %v", err)
	}

	log.Println("Restoring document types...")
	_, err = tx.Exec(ctx, `
		INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy)
		VALUES
		  ('JE', 'Journal Entry',    false, true, false, false, 'global', false),
		  ('SI', 'Sales Invoice',    true,  true, true,  false, 'per_fy', true),
		  ('PI', 'Purchase Invoice', true,  true, false, true,  'per_fy', true)
		ON CONFLICT (code) DO NOTHING;
	`)
	if err != nil {
		log.Fatalf("Failed to restore document types: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("Failed to commit: %v", err)
	}

	log.Println("✅ Seed data restored successfully.")
	os.Exit(0)
}
