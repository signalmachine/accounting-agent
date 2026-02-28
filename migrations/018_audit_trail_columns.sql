ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE sales_orders     ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE documents        ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
