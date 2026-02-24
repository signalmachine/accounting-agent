-- Migration 009: Inventory Engine
-- Adds warehouses, inventory_items (stock master), and inventory_movements (audit log).
-- inventory_items.qty_on_hand and qty_reserved are the authoritative running totals.
-- All stock mutations use row-level locks (FOR UPDATE on inventory_items).

-- ── Warehouses ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS warehouses (
    id          SERIAL PRIMARY KEY,
    company_id  INT          NOT NULL,
    code        VARCHAR(20)  NOT NULL,
    name        TEXT         NOT NULL,
    is_active   BOOLEAN      NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_warehouses_company FOREIGN KEY (company_id) REFERENCES companies(id),
    CONSTRAINT uq_warehouses_company_code UNIQUE (company_id, code)
);

-- ── Inventory Items (stock master) ────────────────────────────────────────────
-- One row per (company, product, warehouse).
-- qty_on_hand: physical stock on hand.
-- qty_reserved: soft-locked by CONFIRMED orders (not yet shipped).
-- Available = qty_on_hand - qty_reserved.
-- unit_cost: weighted average purchase cost, updated on each RECEIPT.

CREATE TABLE IF NOT EXISTS inventory_items (
    id           SERIAL PRIMARY KEY,
    company_id   INT             NOT NULL,
    product_id   INT             NOT NULL,
    warehouse_id INT             NOT NULL,
    qty_on_hand  NUMERIC(14,4)  NOT NULL DEFAULT 0,
    qty_reserved NUMERIC(14,4)  NOT NULL DEFAULT 0,
    unit_cost    NUMERIC(15,6)  NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_invitems_company   FOREIGN KEY (company_id)   REFERENCES companies(id),
    CONSTRAINT fk_invitems_product   FOREIGN KEY (product_id)   REFERENCES products(id),
    CONSTRAINT fk_invitems_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouses(id),
    CONSTRAINT uq_invitems_cpw UNIQUE (company_id, product_id, warehouse_id)
);

-- ── Inventory Movements (append-only audit log) ───────────────────────────────
-- movement_type values:
--   RECEIPT           — stock received (purchase / goods receipt)
--   RESERVATION       — soft-lock on order confirmation
--   RESERVATION_CANCEL — reservation released on order cancellation
--   SHIPMENT          — stock physically shipped (order shipped)
--   ADJUSTMENT        — manual correction
--
-- quantity: positive for stock increase, negative for stock decrease.

CREATE TABLE IF NOT EXISTS inventory_movements (
    id                 SERIAL PRIMARY KEY,
    company_id         INT             NOT NULL,
    inventory_item_id  INT             NOT NULL,
    movement_type      VARCHAR(30)     NOT NULL,
    quantity           NUMERIC(14,4)  NOT NULL,
    unit_cost          NUMERIC(15,6)  NOT NULL DEFAULT 0,
    total_cost         NUMERIC(15,2)  NOT NULL DEFAULT 0,
    order_id           INT,
    movement_date      DATE           NOT NULL,
    notes              TEXT,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_invmov_company   FOREIGN KEY (company_id)        REFERENCES companies(id),
    CONSTRAINT fk_invmov_item      FOREIGN KEY (inventory_item_id) REFERENCES inventory_items(id),
    CONSTRAINT fk_invmov_order     FOREIGN KEY (order_id)          REFERENCES sales_orders(id)
);

-- ── New document types ────────────────────────────────────────────────────────

INSERT INTO document_types (code, name, numbering_strategy, resets_every_fy)
VALUES
    ('GR', 'Goods Receipt', 'sequential', false),
    ('GI', 'Goods Issue',   'sequential', false)
ON CONFLICT (code) DO NOTHING;
