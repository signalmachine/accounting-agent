-- Migration 008: Seed customers and products for company 1000
-- Idempotent: ON CONFLICT DO NOTHING guards on all inserts.

DO $$
DECLARE
    v_company_id INT;
BEGIN
    SELECT id INTO v_company_id FROM companies WHERE company_code = '1000';

    IF v_company_id IS NULL THEN
        RAISE NOTICE 'Company 1000 not found — skipping seed data for migration 008.';
        RETURN;
    END IF;

    -- Customers
    INSERT INTO customers (company_id, code, name, email, phone, address, credit_limit, payment_terms_days)
    VALUES
        (v_company_id, 'C001', 'Acme Corp',         'billing@acme.com',  '+91-9800000001', '12 MG Road, Bengaluru 560001', 100000.00, 30),
        (v_company_id, 'C002', 'Beta Industries',   'accounts@beta.in',  '+91-9800000002', '45 Linking Road, Mumbai 400050',  50000.00, 45),
        (v_company_id, 'C003', 'Gamma Enterprises', 'finance@gamma.co',  '+91-9800000003', '8 Connaught Place, Delhi 110001',  75000.00, 30)
    ON CONFLICT (company_id, code) DO NOTHING;

    -- Products (revenue_account_code references existing chart of accounts)
    INSERT INTO products (company_id, code, name, description, unit_price, unit, revenue_account_code)
    VALUES
        (v_company_id, 'P001', 'Consulting Services', 'Professional consulting and advisory services', 5000.00, 'hour',  '4100'),
        (v_company_id, 'P002', 'Widget A',             'Standard industrial widget, Type A',           500.00,  'unit',  '4000'),
        (v_company_id, 'P003', 'Widget B',             'Premium industrial widget, Type B',            1200.00, 'unit',  '4000'),
        (v_company_id, 'P004', 'Software License',    'Annual software license subscription',          15000.00,'license','4100')
    ON CONFLICT (company_id, code) DO NOTHING;
END;
$$;
