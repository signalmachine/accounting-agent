-- Seed a testuser FINANCE_MANAGER account for cloud demo / general access.
-- Default credentials: testuser / Test@1234 (bcrypt cost 10)
-- Full operational access: AI chat (propose + confirm), orders, POs, reports.
-- Cannot access Settings (user management) — that requires ADMIN.
INSERT INTO users (company_id, username, email, password_hash, role)
SELECT
    c.id,
    'testuser',
    'testuser@company1000.local',
    '$2a$10$TiQz88CGIPnWRqgS8/lifu6p0HW5qqWPxcd1VFZrA7DczXU/dWZru',
    'FINANCE_MANAGER'
FROM companies c
WHERE c.company_code = '1000'
ON CONFLICT DO NOTHING;
