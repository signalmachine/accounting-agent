-- Migration 022: Purchase orders and purchase order lines
-- Idempotent: uses IF NOT EXISTS and ON CONFLICT DO NOTHING

CREATE TABLE IF NOT EXISTS purchase_orders (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    vendor_id INT NOT NULL REFERENCES vendors(id),
    po_number VARCHAR(30) NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'DRAFT',
    po_date DATE NOT NULL,
    expected_delivery_date DATE NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    exchange_rate NUMERIC(15,6) NOT NULL DEFAULT 1,
    total_transaction NUMERIC(14,2) NOT NULL DEFAULT 0,
    total_base NUMERIC(14,2) NOT NULL DEFAULT 0,
    notes TEXT,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS purchase_order_lines (
    id SERIAL PRIMARY KEY,
    order_id INT NOT NULL REFERENCES purchase_orders(id),
    line_number INT NOT NULL,
    product_id INT NULL REFERENCES products(id),
    description TEXT NOT NULL,
    quantity NUMERIC(14,2) NOT NULL,
    unit_cost NUMERIC(14,2) NOT NULL,
    line_total_transaction NUMERIC(14,2) NOT NULL,
    line_total_base NUMERIC(14,2) NOT NULL,
    expense_account_code VARCHAR(20) NULL
);

CREATE INDEX IF NOT EXISTS idx_purchase_orders_company_status ON purchase_orders(company_id, status);
CREATE INDEX IF NOT EXISTS idx_purchase_orders_vendor ON purchase_orders(vendor_id);

-- PO document type (gapless per-FY numbering)
INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy)
VALUES ('PO', 'Purchase Order', false, false, false, false, 'per_fy', true)
ON CONFLICT (code) DO NOTHING;
