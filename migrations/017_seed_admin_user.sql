-- Seed one ADMIN user for Company 1000.
-- Default password: Admin@1234 (bcrypt cost 10)
-- Change immediately after first login via PATCH /api/auth/password or by re-seeding.
INSERT INTO users (company_id, username, email, password_hash, role)
SELECT
    c.id,
    'admin',
    'admin@company1000.local',
    '$2a$10$X9wQ11OJqvi8fcGQQzwQt.mGs.GPwIPFZOH/r.eGIrIRZs8JkO.TW',
    'ADMIN'
FROM companies c
WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;
