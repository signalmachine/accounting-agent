-- Seed a demo ACCOUNTANT user for public/general access testing.
-- Default credentials: demo / Demo@1234 (bcrypt cost 10)
-- This user has read-only + order-creation access but cannot approve POs,
-- confirm AI write actions, or manage users.
-- Change or disable after testing by setting is_active = false.
INSERT INTO users (company_id, username, email, password_hash, role)
SELECT
    c.id,
    'demo',
    'demo@company1000.local',
    '$2a$10$QZ99Ql4n4WQp/87rGCRzqeUcgZ7xicK7wyHDdK0ZlVngdREmfbn2G',
    'ACCOUNTANT'
FROM companies c
WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;
