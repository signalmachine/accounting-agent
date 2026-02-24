-- Migration 007: Sales Order Management
-- Adds customers, products, sales_orders, sales_order_lines tables
-- and SO document type for gapless order numbering.

-- Customers master data
CREATE TABLE IF NOT EXISTS customers (
    id                 SERIAL PRIMARY KEY,
    company_id         INT            NOT NULL,
    code               VARCHAR(20)    NOT NULL,
    name               TEXT           NOT NULL,
    email              TEXT           NOT NULL DEFAULT '',
    phone              TEXT           NOT NULL DEFAULT '',
    address            TEXT           NOT NULL DEFAULT '',
    credit_limit       NUMERIC(14, 2) NOT NULL DEFAULT 0,
    payment_terms_days INT            NOT NULL DEFAULT 30,
    created_at         TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_customers_company  FOREIGN KEY (company_id) REFERENCES companies(id),
    CONSTRAINT uq_customers_company_code UNIQUE (company_id, code)
);

CREATE INDEX IF NOT EXISTS idx_customers_company_id ON customers(company_id);

-- Products / services catalog
CREATE TABLE IF NOT EXISTS products (
    id                   SERIAL PRIMARY KEY,
    company_id           INT            NOT NULL,
    code                 VARCHAR(20)    NOT NULL,
    name                 TEXT           NOT NULL,
    description          TEXT           NOT NULL DEFAULT '',
    unit_price           NUMERIC(14, 2) NOT NULL,
    unit                 TEXT           NOT NULL DEFAULT 'unit',
    revenue_account_code VARCHAR(20)    NOT NULL,
    is_active            BOOLEAN        NOT NULL DEFAULT true,
    created_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_products_company  FOREIGN KEY (company_id) REFERENCES companies(id),
    CONSTRAINT uq_products_company_code UNIQUE (company_id, code)
);

CREATE INDEX IF NOT EXISTS idx_products_company_id ON products(company_id);

-- Sales Order document type (for gapless SO numbering via existing document_sequences)
INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy)
VALUES ('SO', 'Sales Order', false, false, true, false, 'global', true)
ON CONFLICT (code) DO NOTHING;

-- Sales orders header
CREATE TABLE IF NOT EXISTS sales_orders (
    id                  SERIAL PRIMARY KEY,
    company_id          INT            NOT NULL,
    order_number        VARCHAR(50),
    customer_id         INT            NOT NULL,
    status              VARCHAR(20)    NOT NULL DEFAULT 'DRAFT',
    order_date          DATE           NOT NULL,
    currency            CHAR(3)        NOT NULL DEFAULT 'INR',
    exchange_rate       NUMERIC(15, 6) NOT NULL DEFAULT 1.0,
    total_transaction   NUMERIC(14, 2) NOT NULL DEFAULT 0,
    total_base          NUMERIC(14, 2) NOT NULL DEFAULT 0,
    notes               TEXT           NOT NULL DEFAULT '',
    invoice_document_id INT,
    created_at          TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    confirmed_at        TIMESTAMPTZ,
    shipped_at          TIMESTAMPTZ,
    invoiced_at         TIMESTAMPTZ,
    paid_at             TIMESTAMPTZ,
    CONSTRAINT fk_sales_orders_company  FOREIGN KEY (company_id)          REFERENCES companies(id),
    CONSTRAINT fk_sales_orders_customer FOREIGN KEY (customer_id)         REFERENCES customers(id),
    CONSTRAINT fk_sales_orders_invoice  FOREIGN KEY (invoice_document_id) REFERENCES documents(id),
    CONSTRAINT chk_sales_orders_status  CHECK (status IN ('DRAFT', 'CONFIRMED', 'SHIPPED', 'INVOICED', 'PAID', 'CANCELLED')),
    CONSTRAINT uq_sales_orders_number   UNIQUE (company_id, order_number)
);

CREATE INDEX IF NOT EXISTS idx_sales_orders_company_id  ON sales_orders(company_id);
CREATE INDEX IF NOT EXISTS idx_sales_orders_customer_id ON sales_orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_sales_orders_status      ON sales_orders(status);

-- Sales order lines
CREATE TABLE IF NOT EXISTS sales_order_lines (
    id                    SERIAL PRIMARY KEY,
    order_id              INT            NOT NULL,
    line_number           INT            NOT NULL,
    product_id            INT            NOT NULL,
    quantity              NUMERIC(14, 3) NOT NULL,
    unit_price            NUMERIC(14, 2) NOT NULL,
    line_total_transaction NUMERIC(14, 2) NOT NULL,
    line_total_base       NUMERIC(14, 2) NOT NULL,
    CONSTRAINT fk_sol_order   FOREIGN KEY (order_id)   REFERENCES sales_orders(id) ON DELETE CASCADE,
    CONSTRAINT fk_sol_product FOREIGN KEY (product_id) REFERENCES products(id),
    CONSTRAINT uq_sol_order_line UNIQUE (order_id, line_number)
);

CREATE INDEX IF NOT EXISTS idx_sales_order_lines_order_id ON sales_order_lines(order_id);
