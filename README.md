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
| **Configurable Account Rules** | `account_rules` table replaces hardcoded account constants. `RuleEngine` resolves AR, AP, Inventory, COGS accounts per company with priority and effective-date support |
| **ApplicationService Layer** | Single interface (`app.ApplicationService`) that all UI adapters call — no business logic in REPL or CLI handlers |
| **CLI Adapter** | `internal/adapters/cli/` handles one-shot commands (`propose`, `validate`, `commit`, `bal`). `main.go` is pure wiring (48 lines) |
| **CLI & REPL** | Interactive command-line interface for proposing, committing, and managing the full order/inventory lifecycle |
| **PostgreSQL** | ACID-compliant persistence with constraint enforcement |
| **Structured Outputs** | Strict JSON Schema via `invopop/jsonschema` guarantees schema-safe AI responses |

---

## Architecture

The system uses a strict 4-layer architecture. Dependencies flow downward only — no layer imports anything above it.

```
Layer 4 — Interface Adapters
          internal/adapters/repl/   ← REPL commands, display, interactive wizards
          internal/adapters/cli/    ← CLI one-shot commands (propose/validate/commit/bal)
          cmd/app/                  ← Wiring only — 48 lines, no business logic
                    ↓
Layer 3 — Application Service
          internal/app/             ← Single ApplicationService interface + implementation
                    ↓               ← No fmt.Println. No ANSI codes. No display logic.
Layer 2 — Domain Core
          internal/core/            ← Ledger, OrderService, InventoryService,
                                       DocumentService, RuleEngine
                    ↓
Layer 1 — Infrastructure
          internal/db/              ← pgx connection pool
          internal/ai/              ← OpenAI GPT-4o agent (advisory only, never writes DB)
```

**Dependency rules (non-negotiable):**
- Adapters call `ApplicationService` only — they never call domain services directly.
- `ApplicationService` calls domain services and maps results to response types.
- Domain services (`OrderService`, `InventoryService`) call `Ledger`, `DocumentService`, and `RuleEngine`.
- `internal/ai` is called by `ApplicationService` and never touches the database.

### Key Design Decisions

**Immutable ledger.** `journal_entries` and `journal_lines` are append-only. Corrections use compensating entries, never `UPDATE`. Only `internal/core/ledger.go` may write to these tables.

**AI is advisory only.** `internal/ai/agent.go` returns a `core.Proposal`. The proposal must pass `Proposal.Validate()` and receive explicit user approval before `Ledger.Commit()` is called.

**One transaction currency per journal entry (SAP model).** A single `TransactionCurrency` and `ExchangeRate` apply to all lines of an entry. Mixed-currency entries are forbidden.

**Atomic cross-domain transactions.** `Ledger.CommitInTx(ctx, tx, proposal)` allows the inventory service to book a COGS entry inside the same PostgreSQL transaction that deducts stock and updates order status — no inconsistency window.

**Service products vs physical goods.** Products without an `inventory_item` record bypass stock checks and COGS booking transparently.

**Company-scoped base currency.** All accounting proposals use the company's `base_currency` resolved from the database at runtime — no hardcoded currency strings.

---

## Project Structure

```
.
├── cmd/
│   ├── app/                        # Entry point: CLI one-shot commands + REPL wiring
│   ├── server/                     # Entry point: HTTP web server (port 8080)
│   ├── verify-agent/               # Standalone AI integration test
│   ├── verify-db/                  # Runs all SQL migrations against the DB
│   └── restore-seed/               # Restore seed data
├── internal/
│   ├── adapters/
│   │   ├── cli/
│   │   │   └── cli.go              # CLI one-shot commands: propose, validate, commit, bal
│   │   ├── repl/
│   │   │   ├── repl.go             # REPL loop + slash command dispatcher (calls ApplicationService)
│   │   │   ├── display.go          # All print* functions (accept result types, no DB calls)
│   │   │   └── wizards.go          # Interactive order creation wizard
│   │   └── web/
│   │       ├── handlers.go         # chi router setup and all route registrations
│   │       ├── middleware.go       # RequestID, Logger, Recoverer, CORS middleware
│   │       └── errors.go           # writeError / writeJSON helpers, notImplemented stub
│   ├── app/
│   │   ├── service.go              # ApplicationService interface (the adapter contract)
│   │   ├── app_service.go          # ApplicationService implementation
│   │   ├── result_types.go         # Output types: TrialBalanceResult, OrderResult, AIResult …
│   │   └── request_types.go        # Input types: CreateOrderRequest, ReceiveStockRequest …
│   ├── ai/                         # OpenAI Responses API agent (GPT-4o, advisory only)
│   ├── core/
│   │   ├── ledger.go               # Double-entry commit, CommitInTx, balances, reversal
│   │   ├── document_service.go     # Gapless document numbering with row-level locks
│   │   ├── rule_engine.go          # RuleEngine: resolves account codes from account_rules table
│   │   ├── model.go                # Proposal, ProposalLine, Company, AccountBalance …
│   │   ├── order_model.go          # Customer, Product, SalesOrder domain models
│   │   ├── order_service.go        # Order state machine + invoice/payment accounting
│   │   ├── inventory_model.go      # Warehouse, StockLevel domain models
│   │   ├── inventory_service.go    # Stock receipts, reservations, COGS automation
│   │   ├── proposal_logic.go       # Proposal validation and normalization
│   │   ├── ledger_integration_test.go
│   │   ├── document_integration_test.go
│   │   ├── order_integration_test.go
│   │   ├── inventory_integration_test.go
│   │   └── rule_engine_integration_test.go
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
│   ├── 010_seed_inventory.sql      # MAIN warehouse + zero-stock items for P002, P003
│   ├── 011_account_rules.sql       # account_rules table + unique index
│   └── 012_seed_account_rules.sql  # 6 default rules for Company 1000
└── docs/
    └── Implementation_plan_upgrage.md  # Full phased roadmap (Tiers 0–5)
```

> [!NOTE]
> **Go test file convention**: Test files (`_test.go`) live *alongside the source files they test*. This is idiomatic Go — `go test` discovers tests by scanning each package directory.

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
One or more per company. The system picks the first active warehouse as the default for stock receipts.

#### `inventory_items`
One row per `(company, product, warehouse)`. Columns:
- `qty_on_hand` — physical stock on hand
- `qty_reserved` — soft-locked by CONFIRMED orders not yet shipped
- `unit_cost` — weighted average purchase cost, updated on each receipt
- Available = `qty_on_hand − qty_reserved`

Only **physical goods** (products with an `inventory_item` record) participate in stock checks and COGS. Service products are silently skipped.

#### `inventory_movements`
Append-only log. Movement types: `RECEIPT`, `RESERVATION`, `RESERVATION_CANCEL`, `SHIPMENT`.

---

### Configurable Account Rules

#### `account_rules`
Stores per-company account mappings that replace hardcoded constants in domain services. Queried at runtime by `RuleEngine.ResolveAccount()`.

| Column | Type | Notes |
|---|---|---|
| `company_id` | `INT` | FK to `companies` |
| `rule_type` | `VARCHAR(40)` | Key: `AR`, `AP`, `INVENTORY`, `COGS`, `BANK_DEFAULT`, `RECEIPT_CREDIT` |
| `account_code` | `VARCHAR(20)` | The resolved account code |
| `priority` | `INT` | Higher value wins when multiple rows match |
| `effective_from` / `effective_to` | `DATE` | Optional date range — `NULL` effective_to means no expiry |

**Seeded defaults for Company 1000:**

| Rule Type | Account | Description |
|---|---|---|
| `AR` | `1200` | Accounts Receivable (invoicing + payment receipt) |
| `AP` | `2000` | Accounts Payable |
| `INVENTORY` | `1400` | Inventory asset (goods receipts + COGS) |
| `COGS` | `5000` | Cost of Goods Sold |
| `BANK_DEFAULT` | `1100` | Default bank account |
| `RECEIPT_CREDIT` | `2000` | Default credit account for stock receipts |

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

# Required when multiple companies exist in the database.
# If only one company is present, this can be omitted.
COMPANY_CODE=1000
```

### Database Initialization
The database schema is exclusively managed via the built-in migration runner:

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
# CLI / REPL binary
go build -o app.exe ./cmd/app

# Web server binary
go build -o server.exe ./cmd/server
```

### Web Server

```powershell
# Start the HTTP server (default port 8080)
go run ./cmd/server

# Custom port
$env:SERVER_PORT = "9000"; go run ./cmd/server

# CORS for local frontend development
$env:ALLOWED_ORIGINS = "http://localhost:3000"; go run ./cmd/server
```

The web server and the CLI/REPL are **separate binaries** — both wire up the same `ApplicationService` underneath. Run them independently; they do not conflict.

| Endpoint | Status | Description |
|---|---|---|
| `GET /api/health` | ✅ Live | Returns `{"status":"ok","company":"<code>"}` |
| All other `/api/*` routes | 🔲 Stub (501) | Implemented incrementally in Phases WF2–WF4 |

### Interactive REPL
```powershell
./app.exe
```

#### How Input Routing Works

Every line you type at the `>` prompt follows one simple rule:

```
Input starts with /  →  Deterministic command dispatcher (no AI, instant)
Input has no /       →  AI agent (GPT-4o interprets it as a business event)
```

This rule applies to **all** input — single words, multi-word phrases, everything. There are no exceptions.

| What you type | What happens |
|---|---|
| `/bal` | Runs the trial balance command instantly |
| `/confirm SO-2026-00001` | Confirms the order — no AI involved |
| `record $500 received from Acme` | Sent to GPT-4o → proposal → you approve/reject |
| `bal` (no slash) | Also sent to GPT-4o — likely triggers a clarification request |

> [!IMPORTANT]
> Typing `bal` without a `/` does **not** run the balance command — it sends the word "bal" to the AI agent. Always use `/bal` or `/balances` for the trial balance.

#### AI Agent Flow

When you type without a `/` prefix, the REPL enters AI mode:

```
You type a business event description
         ↓
[AI] Processing...   (GPT-4o call)
         ↓
  ┌──────────────────────────────────────┐
  │  Clarification needed?               │
  │  AI asks a follow-up question        │
  │  You answer (or type cancel / "")    │──→  Cancelled. (back to > prompt)
  │  You type a /command                 │──→  (AI session cancelled)
  │                                      │     command runs, back to > prompt
  └────────────────┬─────────────────────┘
                   │  (repeat up to 3 rounds)
                   ↓
  Proposal displayed (accounts, amounts, reasoning)
         ↓
  Approve this transaction? (y/n):
  y → COMMITTED    n → Cancelled.   (back to > prompt)
```

**Clarification loop rules:**
- If you type a `/command` while the AI is waiting for your clarification answer, the AI session is **cancelled immediately** and the command runs. You are not stuck.
- Typing an empty line or the word `cancel` also cancels the session.
- After 3 rounds of clarification with no resolution, the session times out with a message suggesting you use a slash command instead.

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
  /exit  or  /quit                 Exit

AGENT MODE  (anything without a leading /)
  Describe any business event in natural language.
  GPT-4o will propose a double-entry journal entry.
  You review and approve before anything is written to the ledger.
  Example: "received INR 50000 from client ABC for consulting work"
  Example: "paid rent of 10000 for March"
```

#### Example Session — Order Lifecycle

```
Accounting Agent
Company: 1000 — Local Operations India (INR)
----------------------------------------------------------------------

> /stock
  CODE     PRODUCT    WH     ON HAND  RESERVED  AVAILABLE  UNIT COST
  P002     Widget A   MAIN      0.00      0.00       0.00       0.00

> /receive P002 100 250.00
Received 100 units of P002 @ 250.00. DR 1400 Inventory, CR 2000.

> /new-order C001
Creating order for customer: C001
  Line 1: P002 20
  Line 2: done
Order created (ID: 1, Status: DRAFT)

> /confirm 1
Order CONFIRMED. Number: SO-GLOBAL-00001

> /ship SO-GLOBAL-00001
Order SO-GLOBAL-00001 marked as SHIPPED. COGS booked if applicable.

> /invoice SO-GLOBAL-00001
Order SO-GLOBAL-00001 INVOICED. Journal entry committed (DR AR, CR Revenue).

> /payment SO-GLOBAL-00001
Payment recorded for order SO-GLOBAL-00001. Status: PAID.

> /bal
  1100   Bank Account            25000.00
  1200   Accounts Receivable         0.00
  1400   Inventory               17500.00
  5000   Cost of Goods Sold       5000.00
```

#### Example Session — AI Agent Mode

```
> received 50000 from client ABC for consulting services
[AI] Processing...

SUMMARY:    Customer receipt for consulting services
DOC TYPE:   SI
CURRENCY:   INR @ rate 1
ENTRIES:
  [DR] Account 1200       50000.00 INR
  [CR] Account 4100       50000.00 INR

Approve this transaction? (y/n): y
Transaction COMMITTED.
```

#### Example Session — Clarification + Slash Command Escape

```
> bal
[AI] Processing...

[AI]: Please specify the transaction type and amount. Did you mean to
      check your balance? If so, type /bal or /balances.

> /bal
(AI session cancelled)

==============================================================
  TRIAL BALANCE
  Company  : 1000 — Local Operations India
  Currency : INR
==============================================================
  1100   Bank Account        500000.00
  ...
==============================================================
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

**32 tests currently passing** across ledger, document, order, inventory, and rule engine domains.

---

## Accounting Flows

Account codes are resolved at runtime from the `account_rules` table via `RuleEngine`. The values below are the seeded defaults for Company 1000.

| Business Event | Document | Debit | Credit |
|---|---|---|---|
| Receive inventory from supplier | GR | `INVENTORY` rule → 1400 | `RECEIPT_CREDIT` rule → 2000 AP |
| Ship goods (COGS) | GI | `COGS` rule → 5000 | `INVENTORY` rule → 1400 |
| Invoice customer | SI | `AR` rule → 1200 | 4000/4100 Revenue (per product) |
| Record customer payment | JE | 1100 Bank | `AR` rule → 1200 |

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

## Implementation Status

| Tier | Phase | Description | Status |
|---|---|---|---|
| 0 | — | Bug fixes: hardcoded currency, non-deterministic company load, AI loop depth | ✅ Done |
| 1 | 1 | Result types + ApplicationService contract (`internal/app/`) | ✅ Done |
| 1 | 2 | ApplicationService implementation (`app_service.go`) | ✅ Done |
| 1 | 3 | REPL adapter extraction (`internal/adapters/repl/`) | ✅ Done |
| 1 | 4 | CLI adapter (`internal/adapters/cli/`) + slim `main.go` (48 lines) | ✅ Done |
| 2 | 5 | `account_rules` table + 6 seed rules for Company 1000 | ✅ Done |
| 2 | 6 | `RuleEngine` service + wired into `OrderService` (AR account dynamic) | ✅ Done |
| 2 | 7 | `RuleEngine` wired into `InventoryService` (Inventory/COGS/RECEIPT_CREDIT) | Pending |
| 2 | 8–10 | Account statement, P&L, Balance Sheet reports | Pending |
| 2.5 | WF1 | REST API foundation: `cmd/server`, chi router, middleware, stub handlers, `/api/health` | ✅ Done |
| 2.5 | WF2 | Authentication: JWT, users table, login/logout/me endpoints, user management API | Pending |
| 2.5 | WF3 | Frontend scaffold: templ + HTMX + Alpine.js + Tailwind, login page, app shell, sidebar | Pending |
| 2.5 | WF4 | Core accounting screens: dashboard, trial balance, account statement, P&L, balance sheet | Pending |
| 2.5 | WF5 | AI chat home (`/`): full-screen conversational UI, SSE streaming, action cards, file upload | Pending |
| 3 | 11–14 | Procurement: vendor master, purchase orders, goods receipt, AP payment | Pending |
| 3 | 15–18 | Service jobs: job orders, progress, invoicing, inventory consumption | Pending |
| 3 | 19–21 | Rentals: asset master, contracts, billing, depreciation | Pending |
| 4 | 22–30 | Tax framework: GST, TDS/TCS, period locking, GSTR export | Pending |
| 5 | 31–35 | AI expansion, approvals, integrations, multi-branch | Pending |

Full phase-by-phase details: [`docs/Implementation_plan_upgrage.md`](docs/Implementation_plan_upgrage.md)
Web UI architecture and phases: [`docs/web_ui_plan.md`](docs/web_ui_plan.md)
