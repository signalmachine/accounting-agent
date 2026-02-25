-- 012_seed_account_rules.sql
-- Seeds account_rules for Company 1000 to match current hardcoded constants.
-- These rules will be read dynamically by RuleEngine in Phase 6 & 7.

INSERT INTO account_rules (company_id, rule_type, account_code)
SELECT c.id, rules.rule_type, rules.account_code
FROM companies c
CROSS JOIN (VALUES
    ('AR',              '1200'),   -- Accounts Receivable
    ('AP',              '2000'),   -- Accounts Payable
    ('INVENTORY',       '1400'),   -- Inventory Asset
    ('COGS',            '5000'),   -- Cost of Goods Sold
    ('BANK_DEFAULT',    '1100'),   -- Default Bank
    ('RECEIPT_CREDIT',  '2000')    -- Default credit on stock receipt
) AS rules(rule_type, account_code)
WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;
