-- Migration 013: Enable pg_trgm extension and create GIN indexes for similarity search.
-- Used by Phase 7.5 AI read tools: search_accounts, search_customers, search_products.
-- pg_trgm is a standard PostgreSQL extension available in all PostgreSQL 12+ installations.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- GIN index on accounts.name for fast trigram similarity search.
CREATE INDEX IF NOT EXISTS idx_accounts_name_trgm ON accounts USING gin(name gin_trgm_ops);

-- GIN index on customers.name for fast trigram similarity search.
CREATE INDEX IF NOT EXISTS idx_customers_name_trgm ON customers USING gin(name gin_trgm_ops);

-- GIN index on products.name for fast trigram similarity search.
CREATE INDEX IF NOT EXISTS idx_products_name_trgm ON products USING gin(name gin_trgm_ops);
