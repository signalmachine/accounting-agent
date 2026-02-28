CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(200) NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(30) NOT NULL DEFAULT 'ACCOUNTANT',
    is_active BOOL DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, username),
    UNIQUE(company_id, email)
);

-- Roles: ACCOUNTANT | FINANCE_MANAGER | ADMIN
-- ACCOUNTANT      — read all; create orders, receive stock, propose journal entries
-- FINANCE_MANAGER — all ACCOUNTANT + approve POs, commit AI proposals, cancel invoiced orders
-- ADMIN           — all FINANCE_MANAGER + manage users, edit account rules
