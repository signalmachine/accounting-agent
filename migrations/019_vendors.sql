-- Migration 019: Vendor master table
-- Idempotent: uses IF NOT EXISTS and ON CONFLICT DO NOTHING

CREATE TABLE IF NOT EXISTS vendors (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    code VARCHAR(20) NOT NULL,
    name VARCHAR(200) NOT NULL,
    contact_person VARCHAR(100),
    email VARCHAR(200),
    phone VARCHAR(40),
    address TEXT,
    payment_terms_days INT DEFAULT 30,
    ap_account_code VARCHAR(20) DEFAULT '2000',
    default_expense_account_code VARCHAR(20),
    is_active BOOL DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, code)
);
