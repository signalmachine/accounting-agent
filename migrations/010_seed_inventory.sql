-- Migration 010: Seed Inventory Data
-- Seeds the MAIN warehouse for company 1000 and zero-stock inventory items for
-- physical goods only (P002 Widget A, P003 Widget B).
-- Service products (P001 Consulting, P004 Software License) are excluded — no
-- inventory_item means they are treated as services and bypass stock checks/COGS.

-- MAIN warehouse for company 1000
INSERT INTO warehouses (company_id, code, name)
SELECT id, 'MAIN', 'Main Warehouse'
FROM companies
WHERE company_code = '1000'
ON CONFLICT (company_id, code) DO NOTHING;

-- Zero-stock inventory items for physical goods (P002, P003)
INSERT INTO inventory_items (company_id, product_id, warehouse_id, qty_on_hand, qty_reserved, unit_cost)
SELECT c.id, p.id, w.id, 0, 0, 0
FROM companies c
JOIN products p ON p.company_id = c.id AND p.code IN ('P002', 'P003')
JOIN warehouses w ON w.company_id = c.id AND w.code = 'MAIN'
WHERE c.company_code = '1000'
ON CONFLICT (company_id, product_id, warehouse_id) DO NOTHING;
