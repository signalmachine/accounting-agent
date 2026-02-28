-- Migration 020: Seed vendors for Company 1000
-- Idempotent: ON CONFLICT DO NOTHING

INSERT INTO vendors (company_id, code, name, contact_person, email, phone, address, payment_terms_days, ap_account_code, default_expense_account_code)
SELECT c.id, 'V001', 'Acme Supplies Pvt Ltd', 'Rajesh Kumar', 'rajesh@acmesupplies.in', '+91-98765-43210',
       '12, Industrial Area, Phase II, Chennai 600 002', 30, '2000', '5100'
FROM companies c WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;

INSERT INTO vendors (company_id, code, name, contact_person, email, phone, address, payment_terms_days, ap_account_code, default_expense_account_code)
SELECT c.id, 'V002', 'Global Tech Components', 'Priya Sharma', 'priya@globaltech.co.in', '+91-99887-76543',
       '45-B, Electronic City, Bengaluru 560 100', 45, '2000', '5100'
FROM companies c WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;

INSERT INTO vendors (company_id, code, name, contact_person, email, phone, address, payment_terms_days, ap_account_code, default_expense_account_code)
SELECT c.id, 'V003', 'Swift Logistics Ltd', 'Anand Mehta', 'anand@swiftlogistics.in', '+91-91234-56789',
       '8, Transport Nagar, Mumbai 400 018', 15, '2000', NULL
FROM companies c WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;
