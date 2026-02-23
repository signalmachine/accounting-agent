CREATE TABLE document_types (
    code VARCHAR(10) PRIMARY KEY,
    name TEXT NOT NULL,
    affects_inventory BOOLEAN NOT NULL DEFAULT false,
    affects_gl BOOLEAN NOT NULL DEFAULT true,
    affects_ar BOOLEAN NOT NULL DEFAULT false,
    affects_ap BOOLEAN NOT NULL DEFAULT false,
    numbering_strategy VARCHAR(20) NOT NULL, -- 'global', 'per_fy', 'per_branch'
    resets_every_fy BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE documents (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    type_code VARCHAR(10) NOT NULL REFERENCES document_types(code),
    status VARCHAR(20) NOT NULL, -- DRAFT, POSTED, CANCELLED
    document_number VARCHAR(50), -- Assigned at POST time
    financial_year INT,          -- e.g., 2026
    branch_id INT,               -- NULL if not branching
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    posted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX documents_unique_number_idx ON documents (
    company_id, 
    type_code, 
    COALESCE(financial_year, -1), 
    COALESCE(branch_id, -1), 
    COALESCE(document_number, 'UNASSIGNED')
);

CREATE TABLE document_sequences (
    company_id INT NOT NULL REFERENCES companies(id),
    type_code VARCHAR(10) NOT NULL REFERENCES document_types(code),
    financial_year INT,  -- CAN BE NULL if resets_every_fy is false
    branch_id INT,       -- CAN BE NULL if strategy is not branch-specific
    last_number BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX document_sequences_unique_idx ON document_sequences (
    company_id, 
    type_code, 
    COALESCE(financial_year, -1), 
    COALESCE(branch_id, -1)
);

-- Seed basic document types
INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy) VALUES
('JE', 'Journal Entry', false, true, false, false, 'global', true),
('SI', 'Sales Invoice', true, true, true, false, 'global', true),
('PI', 'Purchase Invoice', true, true, false, true, 'global', true);
