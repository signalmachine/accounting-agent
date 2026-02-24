# Agentic Accounting Core

An AI-powered, enterprise-grade double-entry accounting system built with Go, PostgreSQL, and OpenAI's Responses API. Modeled after SAP's multi-company, multi-currency architecture.

## Overview

This system integrates a GPT-4o AI agent into a rigorous double-entry ledger. The agent interprets natural language business events and proposes structured journal entries. A human operator reviews and commits them via CLI or interactive REPL.

The system is built for multi-company, multi-currency scenarios where each company has a **base currency (local currency)**, and transactions may occur in any **transaction currency** with an explicit exchange rate.

By combining a deterministic accounting core with a sales order lifecycle and a physical inventory engine, the system functions as a **fully self-contained mini-ERP**.

---

## Features

| Feature | Description |
|---|---|
| **Double-Entry Ledger** | Strict enforcement of debit = credit balance in base currency |
| **Multi-Company** | Each transaction is scoped to a `Company Code` (like SAP's Company Code) |
| **Multi-Currency** | Captures `Transaction Currency`, `Exchange Rate`, and computes `Base Currency` amounts |
| **AI Agent** | GPT-4o via OpenAI Responses API generates structured `Proposal` objects |
| **Idempotency** | UUID-keyed idempotency prevents duplicate journal entries |
| **Reversals** | Atomic, auditable reversal of prior entries |
| **Document Types** | SAP-style document classification (`JE`, `SI`, `PI`, `SO`, `GR`, `GI`) mapping intent to effects |
| **Gapless Numbering** | High-concurrency sequence generation using PostgreSQL row-level locks |
| **Date Semantics** | Separation of accounting period (`posting_date`) from transaction date (`document_date`) |
| **Sales Order Lifecycle** | Full `DRAFT → CONFIRMED → SHIPPED → INVOICED → PAID` state machine with automated invoice and payment journal entries |
| **Inventory Engine** | Warehouse stock tracking, soft reservations on order confirmation, and automatic COGS booking at shipment |
| **Weighted Average Costing** | Purchase receipts update inventory unit cost; COGS is valued at current weighted average |
| **Atomic Cross-Domain TX** | `Ledger.CommitInTx` enables inventory deduction + COGS booking + order state update in a single PostgreSQL transaction |
| **CLI & REPL** | Interactive command-line interface for proposing, committing, and managing the full order/inventory lifecycle |
| **PostgreSQL** | ACID-compliant persistence with constraint enforcement |
| **Structured Outputs** | Strict JSON Schema via `invopop/jsonschema` guarantees schema-safe AI responses |

---

## Domain Architecture

Three cooperating domains, each with strict boundaries:

```
cmd/ (REPL/CLI)
  ├── Ledger        internal/core/ledger.go          — Journal entries, balances, reversals
  ├── OrderService  internal/core/order_service.go   — Sales order lifecycle + invoice/payment accounting
  ├── InventoryService internal/core/inventory_service.go — Stock levels, reservations, COGS
  └── DocumentService internal/core/document_service.go  — Gapless document numbering

internal/ai/         — GPT-4o agent (advisory only, never writes to DB)
internal/db/         — pgx connection pool
```

**Dependency rule:** Orders call `Ledger.Commit()`. Inventory calls `Ledger.CommitInTx()` within order transactions. Neither domain knows journal schema internals. AI never touches the database.

---

## Database Schema

### Core Ledger Tables

#### `companies`
| Column | Type | Notes |
|---|---|---|
| `company_code` | `VARCHAR(10)` | Unique identifier (e.g., `1000`) |
| `name` | `TEXT` | Display name |
| `base_currency` | `VARCHAR(3)` | ISO currency code (e.g., `INR`) |

#### `accounts`
Scoped to a company via `company_id`. Types: `asset`, `liability`, `equity`, `revenue`, `expense`.

**Seeded Chart of Accounts (Company 1000):**

| Code | Name | Type |
|---|---|---|
| 1000 | Cash | asset |
| 1100 | Bank Account | asset |
| 1200 | Accounts Receivable | asset |
| 1400 | Inventory | asset |
| 2000 | Accounts Payable | liability |
| 3000 | Owner Capital | equity |
| 4000 | Sales Revenue | revenue |
| 4100 | Service Revenue | revenue |
| 5000 | Cost of Goods Sold | expense |

#### `document_types`
| Code | Name | Purpose |
|---|---|---|
| `JE` | Journal Entry | General accounting entries |
| `SI` | Sales Invoice | AR creation on order invoice |
| `PI` | Purchase Invoice | AP creation on purchase |
| `SO` | Sales Order | Gapless order numbering at confirmation |
| `GR` | Goods Receipt | Inventory receipt: DR 1400 / CR 2000 |
| `GI` | Goods Issue | COGS at shipment: DR 5000 / CR 1400 |

#### `documents` and `journal_entries`
A `document` represents the business event and holds the gapless document number. A `journal_entry` holds the accounting impact and links back via `reference_id = document_number`.

#### `journal_lines`
| Column | Notes |
|---|---|
| `transaction_currency` | ISO code (shared by all lines in the entry) |
| `exchange_rate` | Header-level rate to base currency |
| `amount_transaction` | Line amount in transaction currency |
| `debit_base` / `credit_base` | Computed: `amount × rate` in base currency |

---

### Sales Order Tables

#### `customers`
| Column | Notes |
|---|---|
| `code` | Customer code (e.g., `C001`) |
| `credit_limit` | `NUMERIC(14,2)` — monetary limit |
| `payment_terms_days` | Integer payment terms |

#### `products`
| Column | Notes |
|---|---|
| `code` | Product code (e.g., `P001`) |
| `unit_price` | Default selling price |
| `revenue_account_code` | FK to `accounts.code` — revenue split per product |

#### `sales_orders` and `sales_order_lines`
Tracks the full order lifecycle. `order_number` (e.g., `SO-GLOBAL-00001`) is assigned at confirmation via gapless numbering.

---

### Inventory Tables

#### `warehouses`
One or more per company. The system picks the first active warehouse as the default.

#### `inventory_items`
One row per `(company, product, warehouse)`. Columns:
- `qty_on_hand` — physical stock on hand
- `qty_reserved` — soft-locked by CONFIRMED orders not yet shipped
- `unit_cost` — weighted average purchase cost, updated on each receipt
- Available = `qty_on_hand − qty_reserved`

Only **physical goods** (products with an `inventory_item` record) participate in stock checks and COGS. Service products are silently skipped.

#### `inventory_movements`
Append-only log. Movement types: `RECEIPT`, `RESERVATION`, `RESERVATION_CANCEL`, `SHIPMENT`, `ADJUSTMENT`.

---

## Project Structure

```
.
├── cmd/
│   ├── app/                        # Main CLI/REPL entry point
│   ├── verify-agent/               # Standalone AI integration test
│   └── verify-db/                  # Runs all SQL migrations against the DB
├── internal/
│   ├── ai/                         # OpenAI Responses API agent
│   ├── core/
│   │   ├── ledger.go               # Double-entry commit, CommitInTx, balances, reversal
│   │   ├── document_service.go     # Gapless document numbering
│   │   ├── order_model.go          # Customer, Product, SalesOrder domain models
│   │   ├── order_service.go        # Order state machine + invoice/payment accounting
│   │   ├── inventory_model.go      # Warehouse, StockLevel domain models
│   │   ├── inventory_service.go    # Stock receipts, reservations, COGS automation
│   │   ├── proposal_logic.go       # Proposal validation and normalization
│   │   ├── ledger_integration_test.go
│   │   ├── document_integration_test.go
│   │   ├── order_integration_test.go
│   │   └── inventory_integration_test.go
│   └── db/                         # Database connection pool (pgx)
├── migrations/
│   ├── 001_init.sql                # Base schema (accounts, journal_entries, journal_lines)
│   ├── 002_sap_currency.sql        # Multi-company & multi-currency upgrade
│   ├── 003_seed_data.sql           # Company 1000 + full chart of accounts
│   ├── 004_date_semantics.sql      # Separate posting_date from document_date
│   ├── 005_document_types_and_numbering.sql  # Documents, sequences, gapless locks
│   ├── 006_fix_documents_unique_index.sql    # Fix draft document uniqueness
│   ├── 007_sales_orders.sql        # Customers, products, sales_orders, sales_order_lines
│   ├── 008_seed_orders.sql         # Seed customers C001–C003, products P001–P004
│   ├── 009_inventory.sql           # Warehouses, inventory_items, inventory_movements
│   └── 010_seed_inventory.sql      # MAIN warehouse + zero-stock items for P002, P003
└── .agents/
    └── rules/                      # Architecture rules for AI agents
```

> [!NOTE]
> **Go test file convention**: Test files (`_test.go`) live *alongside the source files they test*. This is idiomatic Go — `go test` discovers tests by scanning each package directory. Moving them elsewhere would break automatic test discovery.

---

## Setup

### Prerequisites
- Go 1.21+
- PostgreSQL (running locally or remote)
- OpenAI API Key

### Environment
Create a `.env` file in the project root:
```env
DATABASE_URL=postgres://user:pass@localhost:5432/appdb?sslmode=disable
OPENAI_API_KEY=sk-...

# Required only for running integration tests (keeps live DB safe)
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/appdb_test?sslmode=disable
```

### Database Initialization
The database schema is exclusively managed via the built-in migration runner. Initialize or patch your database:

```powershell
go run ./cmd/verify-db
```

This runner automatically:
1. Scans `migrations/` for lexicographically sorted `.sql` scripts
2. Acquires a PostgreSQL advisory lock to prevent concurrent executions
3. Runs each new migration transactionally with SHA-256 checksum tracking
4. Skips already-applied migrations via the `schema_migrations` table

> [!NOTE]
> Run `go run ./cmd/verify-db` with `DATABASE_URL` set to your **test** database to apply new migrations before running integration tests.

---

## Usage

### Build
```powershell
go build -o app.exe ./cmd/app
```

### Interactive REPL
```powershell
./app.exe
```

#### REPL Command Reference

```
LEDGER
  /bal [company-code]              Trial balance
  /balances [company-code]         Alias for /bal

MASTER DATA
  /customers [company-code]        List customers
  /products  [company-code]        List products

SALES ORDERS
  /orders    [company-code]        List orders
  /new-order <customer-code>       Create order (interactive)
  /confirm   <order-ref>           DRAFT → CONFIRMED (assign SO number + reserve stock)
  /ship      <order-ref>           CONFIRMED → SHIPPED (deduct inventory + book COGS)
  /invoice   <order-ref>           SHIPPED → INVOICED (post SI + DR AR / CR Revenue)
  /payment   <order-ref> [bank]    INVOICED → PAID (DR Bank / CR AR)

INVENTORY
  /warehouses [company-code]       List warehouses
  /stock      [company-code]       View stock levels (on hand / reserved / available)
  /receive <product> <qty> <cost>  Receive stock → DR 1400 Inventory / CR 2000 AP

SESSION
  /help                            Show this help
  /exit                            Exit

AGENT MODE  (no / prefix)
  Type any business event in natural language.
  Example: "record $5000 payment received from Acme Corp"
```

#### Example Session

```
Accounting Agent
Company: 1000 — Local Operations India (INR)
----------------------------------------------------------------------

> /stock
  CODE     PRODUCT                WH        ON HAND   RESERVED  AVAILABLE  UNIT COST
  P002     Widget A               MAIN          0.00       0.00       0.00       0.00
  P003     Widget B               MAIN          0.00       0.00       0.00       0.00

> /receive P002 100 250.00
Received 100 units of P002 @ 250.00 into warehouse MAIN. DR 1400 Inventory, CR 2000.

> /new-order C001
Creating order for customer: C001
  Line 1: P002 20
  Line 2: done
> /confirm 1
Order CONFIRMED. Number: SO-GLOBAL-00001

> /stock
  P002   Widget A   MAIN   100.00   20.00   80.00   250.00

> /ship SO-GLOBAL-00001
Order SO-GLOBAL-00001 marked as SHIPPED. COGS booked if applicable.

> /bal
  1400   Inventory           17500.00   ← 25000 receipt − 5000 COGS (20 × 250)
  5000   Cost of Goods Sold   5000.00

> record payment of 10000 from Acme Corp for SO-GLOBAL-00001
[AI] Processing...
[Proposal: DR 1100 Bank 10000 / CR 1200 AR 10000]
Approve this transaction? (y/n): y
Transaction COMMITTED.
```

### CLI Commands
```powershell
# Propose a transaction (outputs JSON)
./app.exe propose "Paid $120 for software subscription"

# Validate a JSON proposal from stdin
Get-Content proposal.json | ./app.exe validate

# Commit a JSON proposal from stdin
Get-Content proposal.json | ./app.exe commit

# Show account balances
./app.exe balances
```

### Running Tests
```powershell
# All tests (integration tests require TEST_DATABASE_URL)
go test ./internal/core -v

# Unit tests only (no DB required)
go test ./internal/core -v -run TestProposal

# Inventory tests only
go test ./internal/core -v -run TestInventory

# Verify AI agent integration
go run ./cmd/verify-agent
```

> [!IMPORTANT]
> Integration tests truncate the database they connect to. Always use a dedicated test database — **never point `TEST_DATABASE_URL` at your live `appdb`**.
>
> After adding new migrations, apply them to the test DB before running tests:
> ```powershell
> $env:DATABASE_URL = $env:TEST_DATABASE_URL; go run ./cmd/verify-db
> ```

**27 tests currently passing** across ledger, document, order, and inventory domains.

---

## Accounting Flows

| Business Event | Document | Debit | Credit |
|---|---|---|---|
| Receive inventory from supplier | GR | 1400 Inventory | 2000 Accounts Payable |
| Ship goods (COGS) | GI | 5000 COGS | 1400 Inventory |
| Invoice customer | SI | 1200 Accounts Receivable | 4000/4100 Revenue |
| Record customer payment | JE | 1100 Bank | 1200 Accounts Receivable |

---

## Multi-Currency Workflow

Transactions follow the SAP model — **one currency per journal entry**:

> [!IMPORTANT]
> **No mixed-currency entries.** Every journal entry uses exactly one `TransactionCurrency`. If the event happened in USD, every line records an amount in USD. The line amounts are converted to the company's `BaseCurrency` using the single header-level `ExchangeRate`. Mixing USD debit with EUR credit in one entry is **forbidden**.

### Transaction Flow

1. **Event** (e.g., "Received $500 from a client")
2. **AI Proposal**: GPT-4o identifies:
   - `TransactionCurrency`: `USD` (header-level, applies to all lines)
   - `ExchangeRate`: e.g., `82.50` (USD → INR, header-level)
   - Each line has only `AccountCode`, `IsDebit`, `Amount` (in USD)
3. **Validation**: `Proposal.Validate()` verifies balance in base currency
4. **Commit**: `journal_lines` stores both transaction and base currency amounts

---

## Architecture Notes

- **Separation of Concerns**: The AI agent (`internal/ai`) is fully decoupled from the core ledger. The ledger does not know about AI. Orders do not know journal schema. Inventory does not know order schema.
- **Atomic Cross-Domain Transactions**: `Ledger.CommitInTx(ctx, tx, proposal)` allows the inventory service to book a COGS journal entry inside the same PostgreSQL transaction that deducts stock and marks an order as SHIPPED — no inconsistency window.
- **Immutable Ledger**: `journal_entries` and `journal_lines` are append-only. Business corrections use compensating entries (reversals), never `UPDATE`.
- **Strict Structured Outputs**: JSON Schema is dynamically generated from the `Proposal` Go struct via `invopop/jsonschema`. The `$schema` key is stripped before submission since OpenAI strict mode does not accept it.
- **No `omitempty` in Schema Structs**: OpenAI's `Strict: true` mode requires all schema properties in `required`. `omitempty` causes field exclusion and must not be used on structs submitted for schema generation.
- **Service Products vs. Physical Goods**: Products without an `inventory_item` record (e.g., consulting services) bypass stock checks and COGS booking transparently. Only products registered in a warehouse participate in inventory tracking.
- **Weighted Average Costing**: Each goods receipt updates the `unit_cost` in `inventory_items` using weighted average. COGS is computed as `quantity × current_unit_cost` at shipment time.
