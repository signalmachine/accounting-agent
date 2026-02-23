-- Migration: SAP-like Multi-Company & Multi-Currency Architecture

-- 1. Create Companies Table
CREATE TABLE companies (
    id SERIAL PRIMARY KEY,
    company_code VARCHAR(4) UNIQUE NOT NULL,
    name TEXT NOT NULL,
    base_currency CHAR(3) NOT NULL
);

-- Seed Default Company (Company Code 1000, Base Currency INR)
INSERT INTO companies (company_code, name, base_currency) 
VALUES ('1000', 'Local Operations India', 'INR');

-- 2. Modify Accounts Table
-- To attach existing accounts to the new company without breaking constraints,
-- we add the column, set the default, and then add the constraint.
ALTER TABLE accounts ADD COLUMN company_id INT;
UPDATE accounts SET company_id = (SELECT id FROM companies WHERE company_code = '1000');
ALTER TABLE accounts ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE accounts ADD CONSTRAINT fk_accounts_company FOREIGN KEY (company_id) REFERENCES companies(id);

-- Enforce that (company_id, code) is unique, as different companies can have the same account code mapping
ALTER TABLE accounts DROP CONSTRAINT accounts_code_key;
ALTER TABLE accounts ADD CONSTRAINT accounts_company_code_key UNIQUE (company_id, code);

-- 3. Modify Journal Entries base table
ALTER TABLE journal_entries ADD COLUMN company_id INT;
UPDATE journal_entries SET company_id = (SELECT id FROM companies WHERE company_code = '1000');
ALTER TABLE journal_entries ALTER COLUMN company_id SET NOT NULL;
ALTER TABLE journal_entries ADD CONSTRAINT fk_journal_entries_company FOREIGN KEY (company_id) REFERENCES companies(id);

-- 4. Multi-Currency Journal Lines Expansion
-- Add the new SAP-style currency and exchange rate columns
ALTER TABLE journal_lines ADD COLUMN transaction_currency CHAR(3);
ALTER TABLE journal_lines ADD COLUMN exchange_rate NUMERIC(15, 6) DEFAULT 1.0;
ALTER TABLE journal_lines ADD COLUMN amount_transaction NUMERIC(14, 2);

-- Populate new columns with historic data assuming the historic data was in USD originally
-- (For existing data, transaction_currency matches base_currency if the exchange rate was 1.0)
UPDATE journal_lines SET transaction_currency = 'USD'; 
UPDATE journal_lines SET amount_transaction = debit WHERE debit > 0;
UPDATE journal_lines SET amount_transaction = credit WHERE credit > 0;

-- Rename legacy debit/credit columns to signify they are strictly Base Currency balances.
ALTER TABLE journal_lines RENAME COLUMN debit TO debit_base;
ALTER TABLE journal_lines RENAME COLUMN credit TO credit_base;

-- Apply constraints safely
ALTER TABLE journal_lines ALTER COLUMN transaction_currency SET NOT NULL;
ALTER TABLE journal_lines ALTER COLUMN amount_transaction SET NOT NULL;
