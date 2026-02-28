-- Migration 024: Add vendor invoice and payment tracking columns to purchase_orders.
-- Idempotent: uses IF NOT EXISTS.

ALTER TABLE purchase_orders
    ADD COLUMN IF NOT EXISTS invoice_number    VARCHAR(100)  NULL,
    ADD COLUMN IF NOT EXISTS invoice_date      DATE          NULL,
    ADD COLUMN IF NOT EXISTS invoice_amount    NUMERIC(14,2) NULL,
    ADD COLUMN IF NOT EXISTS pi_document_number VARCHAR(30)  NULL,
    ADD COLUMN IF NOT EXISTS invoiced_at       TIMESTAMPTZ   NULL,
    ADD COLUMN IF NOT EXISTS paid_at           TIMESTAMPTZ   NULL;
