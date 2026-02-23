-- Init Schema

CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    code TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('asset', 'liability', 'equity', 'revenue', 'expense'))
);

CREATE TABLE journal_entries (
    id SERIAL PRIMARY KEY,
    idempotency_key TEXT UNIQUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    narration TEXT NOT NULL,
    reference_type TEXT,
    reference_id TEXT,
    reasoning TEXT,
    reversed_entry_id INT REFERENCES journal_entries(id)
);

CREATE TABLE journal_lines (
    id SERIAL PRIMARY KEY,
    entry_id INT NOT NULL REFERENCES journal_entries(id),
    account_id INT NOT NULL REFERENCES accounts(id),
    debit NUMERIC(14, 2) NOT NULL DEFAULT 0,
    credit NUMERIC(14, 2) NOT NULL DEFAULT 0
);

-- Seed Data
INSERT INTO accounts (code, name, type) VALUES
('1000', 'Cash', 'asset'),
('1100', 'Bank', 'asset'),
('1200', 'Furniture', 'asset'),
('2000', 'Accounts Payable', 'liability'),
('3000', 'Owner Capital', 'equity'),
('4000', 'Revenue', 'revenue'),
('5000', 'Expenses', 'expense');
