-- Migration: SAP-like Multi-Company & Multi-Currency Architecture

-- 1. Create Companies Table
CREATE TABLE IF NOT EXISTS companies (
    id SERIAL PRIMARY KEY,
    company_code VARCHAR(4) UNIQUE NOT NULL,
    name TEXT NOT NULL,
    base_currency CHAR(3) NOT NULL
);

-- Seed Default Company (Company Code 1000, Base Currency INR)
INSERT INTO companies (company_code, name, base_currency)
VALUES ('1000', 'Local Operations India', 'INR')
ON CONFLICT (company_code) DO NOTHING;

-- 2. Modify Accounts Table
-- To attach existing accounts to the new company without breaking constraints,
-- we add the column, set the default, and then add the constraint.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS company_id INT;
UPDATE accounts SET company_id = (SELECT id FROM companies WHERE company_code = '1000') WHERE company_id IS NULL;
DO $$ BEGIN
    ALTER TABLE accounts ALTER COLUMN company_id SET NOT NULL;
EXCEPTION WHEN others THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE accounts ADD CONSTRAINT fk_accounts_company FOREIGN KEY (company_id) REFERENCES companies(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Enforce that (company_id, code) is unique across companies
DO $$ BEGIN
    ALTER TABLE accounts DROP CONSTRAINT accounts_code_key;
EXCEPTION WHEN undefined_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE accounts ADD CONSTRAINT accounts_company_code_key UNIQUE (company_id, code);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- 3. Modify Journal Entries base table
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS company_id INT;
UPDATE journal_entries SET company_id = (SELECT id FROM companies WHERE company_code = '1000') WHERE company_id IS NULL;
DO $$ BEGIN
    ALTER TABLE journal_entries ALTER COLUMN company_id SET NOT NULL;
EXCEPTION WHEN others THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE journal_entries ADD CONSTRAINT fk_journal_entries_company FOREIGN KEY (company_id) REFERENCES companies(id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- 4. Multi-Currency Journal Lines Expansion
-- Add the new SAP-style currency and exchange rate columns
ALTER TABLE journal_lines ADD COLUMN IF NOT EXISTS transaction_currency CHAR(3);
ALTER TABLE journal_lines ADD COLUMN IF NOT EXISTS exchange_rate NUMERIC(15, 6) DEFAULT 1.0;
ALTER TABLE journal_lines ADD COLUMN IF NOT EXISTS amount_transaction NUMERIC(14, 2);

-- Populate new columns with historic data (for existing data, assumes base currency)
UPDATE journal_lines SET transaction_currency = 'USD' WHERE transaction_currency IS NULL;
UPDATE journal_lines SET amount_transaction = debit WHERE amount_transaction IS NULL AND debit > 0;
UPDATE journal_lines SET amount_transaction = credit WHERE amount_transaction IS NULL AND credit > 0;

-- Rename legacy debit/credit columns to signify they are strictly Base Currency balances.
-- These renames are not idempotent in pure SQL, so we guard with DO blocks.
DO $$ BEGIN
    ALTER TABLE journal_lines RENAME COLUMN debit TO debit_base;
EXCEPTION WHEN undefined_column THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE journal_lines RENAME COLUMN credit TO credit_base;
EXCEPTION WHEN undefined_column THEN NULL;
END $$;

-- Apply constraints safely
DO $$ BEGIN
    ALTER TABLE journal_lines ALTER COLUMN transaction_currency SET NOT NULL;
EXCEPTION WHEN others THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE journal_lines ALTER COLUMN amount_transaction SET NOT NULL;
EXCEPTION WHEN others THEN NULL;
END $$;
