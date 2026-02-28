-- Migration 021: GIN trigram index on vendors.name for fast full-text search
-- Consistent with migration 013 which added pg_trgm indexes for accounts, customers, products.
-- pg_trgm extension was already enabled in migration 013.

CREATE INDEX IF NOT EXISTS idx_vendors_name_trgm ON vendors USING gin(name gin_trgm_ops);
