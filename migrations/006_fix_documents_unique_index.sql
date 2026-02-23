-- Drop the previous index that prevented multiple draft documents
DROP INDEX IF EXISTS documents_unique_number_idx;

-- Create the correct index that allows multiple NULL document_numbers
-- PostgreSQL treats multiple NULL values as distinct unless specified otherwise
CREATE UNIQUE INDEX documents_unique_number_idx ON documents (
    company_id, 
    type_code, 
    COALESCE(financial_year, -1), 
    COALESCE(branch_id, -1), 
    document_number
);
