-- Migration 023: Link inventory movements to purchase order lines
-- Idempotent: uses IF NOT EXISTS

ALTER TABLE inventory_movements ADD COLUMN IF NOT EXISTS po_line_id INT NULL REFERENCES purchase_order_lines(id);

ALTER TABLE purchase_orders ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS idx_inventory_movements_po_line ON inventory_movements(po_line_id) WHERE po_line_id IS NOT NULL;
