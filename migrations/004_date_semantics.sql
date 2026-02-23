-- Migration: Document & Posting Dates
-- Add separate date columns to separate accounting period control (posting_date)
-- and real-world transaction date (document_date) from system creation date (created_at)

-- 1. Add posting_date and document_date columns
ALTER TABLE journal_entries ADD COLUMN posting_date DATE;
ALTER TABLE journal_entries ADD COLUMN document_date DATE;

-- 2. Backfill existing records (Migration Safe)
UPDATE journal_entries SET posting_date = created_at::date, document_date = created_at::date;

-- 3. Apply NOT NULL constraints after backfilling
ALTER TABLE journal_entries ALTER COLUMN posting_date SET NOT NULL;
ALTER TABLE journal_entries ALTER COLUMN document_date SET NOT NULL;

-- 4. Add index for faster queries by posting_date (beneficial for fiscal reporting)
CREATE INDEX idx_journal_entries_posting_date ON journal_entries(posting_date);
