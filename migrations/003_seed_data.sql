-- Seed: Restore default company and full chart of accounts
-- Safe to re-run (uses ON CONFLICT DO NOTHING)

-- Company 1000: Local Operations India (INR base currency)
INSERT INTO companies (company_code, name, base_currency)
VALUES ('1000', 'Local Operations India', 'INR')
ON CONFLICT (company_code) DO NOTHING;

-- Chart of Accounts (scoped to company 1000)
INSERT INTO accounts (company_id, code, name, type)
SELECT c.id, a.code, a.name, a.type
FROM companies c
CROSS JOIN (VALUES
    ('1000', 'Cash',              'asset'),
    ('1100', 'Bank Account',      'asset'),
    ('1200', 'Accounts Receivable', 'asset'),
    ('1300', 'Furniture & Fixtures', 'asset'),
    ('1400', 'Inventory',         'asset'),
    ('2000', 'Accounts Payable',  'liability'),
    ('2100', 'Short-Term Loans',  'liability'),
    ('3000', 'Owner Capital',     'equity'),
    ('3100', 'Retained Earnings', 'equity'),
    ('4000', 'Sales Revenue',     'revenue'),
    ('4100', 'Service Revenue',   'revenue'),
    ('5000', 'Cost of Goods Sold','expense'),
    ('5100', 'Rent Expense',      'expense'),
    ('5200', 'Salary Expense',    'expense'),
    ('5300', 'Utilities Expense', 'expense')
) AS a(code, name, type)
WHERE c.company_code = '1000'
ON CONFLICT (company_id, code) DO NOTHING;
