-- 011_account_rules.sql
-- Creates the account_rules table: configurable account mappings per company.
-- Replaces hardcoded account constants in domain services (Phase 6 & 7).

CREATE TABLE IF NOT EXISTS account_rules (
    id              SERIAL PRIMARY KEY,
    company_id      INT NOT NULL REFERENCES companies(id),
    rule_type       VARCHAR(40) NOT NULL,
    account_code    VARCHAR(20) NOT NULL,
    qualifier_key   VARCHAR(40),
    qualifier_value VARCHAR(40),
    priority        INT DEFAULT 0,
    effective_from  DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to    DATE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_rules_lookup
    ON account_rules(company_id, rule_type,
        COALESCE(qualifier_key, ''), COALESCE(qualifier_value, ''));
