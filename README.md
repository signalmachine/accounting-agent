# Agentic Accounting Core

An AI-powered, enterprise-grade double-entry accounting system built with Go, PostgreSQL, and OpenAI's Responses API. Modeled after SAP's multi-company, multi-currency architecture.

## Overview

This system integrates a GPT-4o AI agent into a rigorous double-entry ledger. The agent interprets natural language business events, executes read tools autonomously, and proposes structured journal entries or domain actions. A human operator reviews and confirms writes via the web UI, CLI, or interactive REPL.

The system is built for multi-company, multi-currency scenarios where each company has a **base currency**, and transactions may occur in any **transaction currency** with an explicit exchange rate.

By combining a deterministic accounting core with a sales order lifecycle, physical inventory engine, and procurement module, the system functions as a **fully self-contained mini-ERP**.

---

## Features

| Feature | Description |
|---|---|
| **Double-Entry Ledger** | Strict enforcement of debit = credit balance in base currency |
| **Multi-Company** | Every transaction is scoped to a `Company Code` (SAP-style) |
| **Multi-Currency** | Captures `Transaction Currency`, `Exchange Rate`, and computes base-currency amounts |
| **AI Agent** | GPT-4o via Responses API — interprets events, runs read tools autonomously, proposes write actions for human confirmation |
| **AI Tool Architecture** | `ToolRegistry` with 24 registered tools (18 read, 6 write). Agentic loop with max 5 iterations and `PreviousResponseID` multi-turn |
| **Idempotency** | UUID-keyed idempotency prevents duplicate journal entries |
| **Reversals** | Atomic, auditable reversal of prior entries via compensating entries |
| **Document Types** | SAP-style classification (`JE`, `SI`, `PI`, `SO`, `GR`, `GI`) |
| **Gapless Numbering** | High-concurrency sequence generation via PostgreSQL `ON CONFLICT DO UPDATE ... RETURNING` |
| **Sales Order Lifecycle** | Full `DRAFT → CONFIRMED → SHIPPED → INVOICED → PAID` state machine with automated journal entries |
| **Inventory Engine** | Warehouse stock tracking, soft reservations, weighted average costing, automatic COGS booking at shipment |
| **Procurement** | Vendor master, purchase orders (`DRAFT → APPROVED → RECEIVED → INVOICED → PAID`), goods receipt, AP payment |
| **Configurable Account Rules** | `account_rules` table + `RuleEngine` resolves AR/AP/Inventory/COGS accounts per company — no hardcoded constants |
| **Reporting** | Trial Balance (materialized view), P&L, Balance Sheet, Account Statement with CSV export |
| **Web UI** | Full server-rendered interface: templ + HTMX + Alpine.js + Tailwind CSS v4. Chat home, dashboard, accounting reports, order/PO lifecycle |
| **Authentication** | JWT HS256 with httpOnly cookies, bcrypt password hashing, `RequireAuth`/`RequireAuthBrowser` middleware |
| **Document Upload** | JPG/PNG/WEBP image attachments in AI chat (30-min TTL cleanup) |
| **ApplicationService Layer** | Single interface that all adapters call — no business logic in REPL, CLI, or web handlers |
| **PostgreSQL** | ACID-compliant persistence, row-level locking, hand-written SQL (no ORM) |

---

## Architecture

Strict 4-layer dependency flow — no layer imports anything above it.

```
Layer 4 — Interface Adapters
          internal/adapters/repl/   ← REPL commands, display, interactive wizards
          internal/adapters/cli/    ← CLI one-shot commands (propose/validate/commit/bal)
          internal/adapters/web/    ← chi router, page handlers, API handlers, SSE, auth middleware
          web/templates/            ← templ page/layout templates (server-rendered HTML)
                    ↓
Layer 3 — Application Service
          internal/app/             ← ApplicationService interface + implementation
                    ↓               ← No display logic. No HTTP types.
Layer 2 — Domain Core
          internal/core/            ← Ledger, OrderService, InventoryService,
                                       DocumentService, RuleEngine, ReportingService,
                                       VendorService, PurchaseOrderService, UserService
                    ↓
Layer 1 — Infrastructure
          internal/db/              ← pgx connection pool
          internal/ai/              ← OpenAI GPT-4o agent + ToolRegistry (advisory only, never writes DB)
```

**Dependency rules:**
- Adapters call `ApplicationService` only — never call domain services directly.
- Domain services call `Ledger`, `DocumentService`, and `RuleEngine`.
- `internal/ai` is called by `ApplicationService` and never touches the database.

### Key Design Decisions

**Immutable ledger.** `journal_entries` and `journal_lines` are append-only. Corrections use compensating entries, never `UPDATE`. Only `internal/core/ledger.go` may write to these tables.

**AI is advisory only.** `internal/ai/agent.go` returns proposals or domain action results. All write actions require explicit user confirmation before `Ledger.Commit()` is called.

**One transaction currency per journal entry (SAP model).** A single `TransactionCurrency` and `ExchangeRate` apply to all lines of an entry. Mixed-currency entries are forbidden.

**Atomic cross-domain transactions.** `Ledger.CommitInTx(ctx, tx, proposal)` allows inventory deduction + COGS booking + order state update in a single PostgreSQL transaction — no inconsistency window.

**Service products vs physical goods.** Products without an `inventory_item` record bypass stock checks and COGS booking transparently.

**Company-scoped base currency.** All proposals resolve the company's `base_currency` from the database at runtime — no hardcoded currency strings.

---

## Project Structure

```
.
├── cmd/
│   ├── app/                        # Entry point: CLI one-shot commands + REPL
│   ├── server/                     # Entry point: HTTP web server (port 8080)
│   ├── verify-agent/               # Standalone AI integration smoke test
│   ├── verify-db/                  # Runs all SQL migrations
│   └── restore-seed/               # Restores seed data
├── internal/
│   ├── adapters/
│   │   ├── cli/cli.go              # CLI one-shot: propose, validate, commit, bal
│   │   ├── repl/
│   │   │   ├── repl.go             # REPL loop + slash command dispatcher
│   │   │   ├── display.go          # All print* display functions
│   │   │   └── wizards.go          # Interactive order creation wizard
│   │   └── web/
│   │       ├── handlers.go         # chi router setup + all route registrations
│   │       ├── auth.go             # JWT auth, login/logout handlers, RequireAuth middleware
│   │       ├── pages.go            # Browser page handlers (login, dashboard)
│   │       ├── accounting.go       # Accounting page + API handlers (reports, journal entry)
│   │       ├── orders.go           # Sales order + inventory page + API handlers
│   │       ├── vendors.go          # Vendor + purchase order page + API handlers
│   │       ├── ai.go               # AI chat page + SSE streaming + file upload handlers
│   │       ├── chat.go             # pendingStore (write-tool confirm/cancel with TTL)
│   │       ├── middleware.go       # RequestID, Logger, Recoverer, CORS, body limit
│   │       └── errors.go           # writeError / writeJSON helpers
│   ├── app/
│   │   ├── service.go              # ApplicationService interface (adapter contract)
│   │   ├── app_service.go          # ApplicationService implementation
│   │   ├── result_types.go         # Output types: TrialBalanceResult, OrderResult, AIResult …
│   │   └── request_types.go        # Input types: CreateOrderRequest, POLineInput …
│   ├── ai/
│   │   ├── agent.go                # InterpretEvent (journal entry) + InterpretDomainAction (agentic loop)
│   │   └── tools.go                # ToolRegistry: 18 read tools + 6 write tools
│   ├── core/
│   │   ├── ledger.go               # Double-entry commit, CommitInTx, balances, reversal
│   │   ├── document_service.go     # Gapless document numbering with row-level locks
│   │   ├── rule_engine.go          # Resolves account codes from account_rules table
│   │   ├── order_service.go        # Sales order state machine + invoice/payment accounting
│   │   ├── inventory_service.go    # Stock receipts, reservations, weighted-average COGS
│   │   ├── reporting_service.go    # Trial balance, P&L, balance sheet, account statement
│   │   ├── vendor_service.go       # Vendor CRUD + pg_trgm fuzzy search
│   │   ├── purchase_order_service.go # PO lifecycle: DRAFT → APPROVED → RECEIVED → INVOICED → PAID
│   │   ├── user_service.go         # AuthenticateUser (bcrypt), GetUser
│   │   ├── model.go                # Proposal, ProposalLine, Company, AccountBalance …
│   │   ├── order_model.go          # Customer, Product, SalesOrder domain models
│   │   ├── inventory_model.go      # Warehouse, StockLevel domain models
│   │   ├── vendor_model.go         # Vendor domain model
│   │   ├── purchase_order_model.go # PurchaseOrder, PurchaseOrderLine domain models
│   │   ├── user_model.go           # User domain model
│   │   ├── proposal_logic.go       # Proposal validation and normalization
│   │   └── *_integration_test.go   # Integration tests (ledger, order, inventory, reporting,
│   │                               #   document, rule_engine, vendor, purchase_order)
│   └── db/db.go                    # pgx connection pool
├── web/
│   ├── templates/
│   │   ├── layouts/                # app_layout, login_layout, chat_layout, modal_shell
│   │   └── pages/                  # 20 page templates (dashboard, reports, orders, POs, chat…)
│   └── static/                     # CSS (Tailwind), JS (HTMX, Alpine.js, Chart.js — vendored)
├── migrations/                     # 024 idempotent SQL migrations (lexicographic order)
├── docs/
│   ├── One_final_implementation_plan.md   # MVP roadmap (all phases complete — historical ref)
│   ├── Tax_Regulatory_Future_Plan.md      # Deferred: GST, TDS, GSTR (Phases 22–30)
│   └── archive/                           # Superseded planning documents
└── Makefile                        # generate (templ), css (tailwind), dev, build, test
```

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

#### `documents` and `journal_entries`
A `document` represents the business event and holds the gapless document number. A `journal_entry` holds the accounting impact and links back via `reference_id = document_number`.

**Document types:** `JE`, `SI` (sales invoice), `PI` (purchase invoice), `SO` (sales order), `GR` (goods receipt), `GI` (goods issue/COGS)

#### `journal_lines`
| Column | Notes |
|---|---|
| `transaction_currency` | ISO code (shared by all lines in the entry) |
| `exchange_rate` | Header-level rate to base currency |
| `amount_transaction` | Line amount in transaction currency |
| `debit_base` / `credit_base` | Computed: `amount × rate` in base currency |

### Sales and Inventory Tables

- **`customers`** — code, credit_limit, payment_terms_days
- **`products`** — code, unit_price, revenue_account_code (per-product revenue split)
- **`sales_orders` / `sales_order_lines`** — full order lifecycle; `order_number` (e.g., `SO-2026-00001`) assigned at confirmation
- **`warehouses`** — one or more per company
- **`inventory_items`** — `(company, product, warehouse)`: qty_on_hand, qty_reserved, unit_cost (weighted average)
- **`inventory_movements`** — append-only log: `RECEIPT`, `RESERVATION`, `RESERVATION_CANCEL`, `SHIPMENT`

### Procurement Tables

- **`vendors`** — code, name, contact info; pg_trgm GIN index for fuzzy search
- **`purchase_orders` / `purchase_order_lines`** — full PO lifecycle; gapless `PO-YYYY-NNNNN` numbering

### Configurable Account Rules

#### `account_rules`
Replaces hardcoded account constants. Queried at runtime by `RuleEngine.ResolveAccount()`.

| Rule Type | Account | Description |
|---|---|---|
| `AR` | `1200` | Accounts Receivable |
| `AP` | `2000` | Accounts Payable |
| `INVENTORY` | `1400` | Inventory asset |
| `COGS` | `5000` | Cost of Goods Sold |
| `BANK_DEFAULT` | `1100` | Default bank account |
| `RECEIPT_CREDIT` | `2000` | Credit account for stock receipts |

### Reporting Views

- **`mv_account_period_balances`** — aggregated debits/credits per account per period
- **`mv_trial_balance`** — current balance per account; refreshed via `REFRESH MATERIALIZED VIEW`

### Authentication

- **`users`** — username, password_hash (bcrypt), created_at
- Default admin: `admin` / `Admin@1234`

---

## Setup

### Prerequisites
- Go 1.25+
- PostgreSQL 12+
- OpenAI API Key

### Environment
Create a `.env` file in the project root:
```env
DATABASE_URL=postgres://user:pass@localhost:5432/appdb?sslmode=disable
OPENAI_API_KEY=sk-...

# Required for integration tests (keeps live DB safe)
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/appdb_test?sslmode=disable

# Required when multiple companies exist
COMPANY_CODE=1000

# Web server
JWT_SECRET=your-secret-key
SERVER_PORT=8080                          # optional, default 8080
ALLOWED_ORIGINS=http://localhost:3000     # optional, for CORS
UPLOAD_DIR=/tmp/uploads                   # optional, for chat image uploads
```

### Database Initialization
```bash
go run ./cmd/verify-db
```

This runner:
1. Scans `migrations/` lexicographically
2. Acquires a PostgreSQL advisory lock
3. Runs each new migration transactionally with SHA-256 checksum tracking
4. Skips already-applied migrations via the `schema_migrations` table

> [!NOTE]
> Run with `DATABASE_URL` pointed at your **test** database before running integration tests.

---

## Usage

### Build
```bash
# CLI / REPL binary
go build -o app.exe ./cmd/app

# Web server binary
go build -o server.exe ./cmd/server

# Or use Make
make build
```

### Web Server
```bash
go run ./cmd/server
# or
make dev   # runs templ generate + go run ./cmd/server
```

The web server starts on port 8080. Open `http://localhost:8080` — you will be redirected to `/login`.

**Default credentials:** `admin` / `Admin@1234`

#### Browser Pages

| Route | Description |
|---|---|
| `GET /` | AI chat home — full-screen conversational interface (primary entry point) |
| `GET /dashboard` | KPI cards + P&L chart |
| `GET /reports/trial-balance` | Trial balance |
| `GET /reports/pl` | Profit & Loss |
| `GET /reports/balance-sheet` | Balance Sheet |
| `GET /reports/statement` | Account statement with CSV export |
| `GET /accounting/journal-entry` | Manual journal entry form |
| `GET /sales/orders` | Sales order list + status filter |
| `GET /sales/orders/new` | New order wizard |
| `GET /sales/orders/{ref}` | Order detail + lifecycle actions |
| `GET /inventory/stock` | Stock levels |
| `GET /purchases/vendors` | Vendor list |
| `GET /purchases/orders` | Purchase order list |
| `GET /purchases/orders/new` | New PO wizard |
| `GET /purchases/orders/{id}` | PO detail + inline lifecycle forms |

#### REST API

All API routes are under `/api/companies/{code}/` and require JWT auth (`Authorization: Bearer <token>` or session cookie).

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check (public) |
| `POST` | `/api/auth/login` | Authenticate, returns JWT |
| `GET` | `/api/companies/{code}/trial-balance` | Trial balance JSON |
| `GET` | `/api/companies/{code}/reports/pl` | P&L JSON |
| `GET` | `/api/companies/{code}/reports/balance-sheet` | Balance Sheet JSON |
| `GET` | `/api/companies/{code}/accounts/{code}/statement` | Account statement JSON |
| `POST` | `/api/companies/{code}/reports/refresh` | Refresh materialized views |
| `POST` | `/api/companies/{code}/journal-entries` | Post a journal entry |
| `POST` | `/api/companies/{code}/journal-entries/validate` | Validate without committing |
| `GET/POST` | `/api/companies/{code}/orders` | List / create orders |
| `POST` | `/api/companies/{code}/orders/{ref}/confirm\|ship\|invoice\|payment` | Order lifecycle |
| `GET/POST` | `/api/companies/{code}/vendors` | List / create vendors |
| `GET/POST` | `/api/companies/{code}/purchase-orders` | List / create POs |
| `POST` | `/api/companies/{code}/purchase-orders/{id}/approve\|receive\|invoice\|pay` | PO lifecycle |
| `POST` | `/chat` | AI chat message (SSE streaming) |
| `POST` | `/chat/confirm` | Execute a pending write tool action |
| `POST` | `/chat/upload` | Upload image attachment (JPG/PNG/WEBP, max 50 MB) |

### Interactive REPL
```bash
./app.exe
```

#### Input Routing

```
Input starts with /  →  Deterministic command dispatcher (instant, no AI)
Input has no /       →  AI agent (GPT-4o) — regardless of length or content
```

| What you type | What happens |
|---|---|
| `/bal` | Trial balance — instant |
| `/confirm SO-2026-00001` | Confirms the order — no AI |
| `record $500 received from Acme` | Sent to GPT-4o → proposal → you approve/reject |
| `bal` (no slash) | Sent to GPT-4o — likely triggers a clarification request |

> [!IMPORTANT]
> `bal` without `/` goes to the AI, not the balance command. Always use `/bal` or `/balances`.

#### AI Agent Flow

```
You type a business event description
         ↓
[AI] Processing...   (GPT-4o call with tool loop)
         ↓
  ┌──────────────────────────────────────────────┐
  │  Clarification needed?                       │
  │  AI asks a follow-up question                │
  │  You answer (or type cancel / empty line)    │──→  Cancelled.
  │  You type a /command                         │──→  AI cancelled, command runs
  └────────────────┬─────────────────────────────┘
                   │  (up to 3 rounds)
                   ↓
  Proposal displayed (accounts, amounts, reasoning)
         ↓
  Approve this transaction? (y/n):
  y → COMMITTED    n → Cancelled.
```

#### REPL Command Reference

```
LEDGER
  /bal [company-code]                      Trial balance
  /balances [company-code]                 Alias for /bal

MASTER DATA
  /customers [company-code]                List customers
  /products  [company-code]                List products

SALES ORDERS
  /orders    [company-code]                List orders
  /new-order <customer-code>               Create order (interactive)
  /confirm   <order-ref>                   DRAFT → CONFIRMED (assign SO number + reserve stock)
  /ship      <order-ref>                   CONFIRMED → SHIPPED (deduct inventory + book COGS)
  /invoice   <order-ref>                   SHIPPED → INVOICED (post SI + DR AR / CR Revenue)
  /payment   <order-ref> [bank]            INVOICED → PAID (DR Bank / CR AR)

INVENTORY
  /warehouses [company-code]               List warehouses
  /stock      [company-code]               View stock levels (on hand / reserved / available)
  /receive <product> <qty> <cost>          Receive stock → DR Inventory / CR AP

REPORTS
  /statement <account-code> [from] [to]   Account statement with running balance
  /pl [year] [month]                       Profit & Loss report
  /bs [as-of-date]                         Balance Sheet as of date
  /refresh                                 Refresh materialized reporting views

SESSION
  /help                                    Show this help
  /exit  or  /quit                         Exit
```

### CLI Commands
```bash
# Propose a transaction (outputs JSON)
./app.exe propose "Paid $120 for software subscription"

# Validate a JSON proposal from stdin
cat proposal.json | ./app.exe validate

# Commit a JSON proposal from stdin
cat proposal.json | ./app.exe commit

# Show account balances
./app.exe balances
```

### Running Tests
```bash
# All tests (integration tests require TEST_DATABASE_URL)
go test ./internal/core -v

# Unit tests only (no DB required)
go test ./internal/core -v -run TestProposal

# Specific domain
go test ./internal/core -v -run TestInventory
go test ./internal/core -v -run TestPurchaseOrder

# Verify AI agent integration
go run ./cmd/verify-agent
```

> [!IMPORTANT]
> Integration tests truncate the test database. Always use a dedicated `TEST_DATABASE_URL` — never point it at your live database.
>
> After adding migrations, apply them to the test DB:
> ```bash
> DATABASE_URL="postgres://..." go run ./cmd/verify-db
> ```

**70 tests passing** across ledger, document, order, inventory, rule engine, reporting, vendor, and purchase order domains.

---

## Accounting Flows

Account codes are resolved at runtime from the `account_rules` table via `RuleEngine`. Values below are the seeded defaults for Company 1000.

| Business Event | Document | Debit | Credit |
|---|---|---|---|
| Receive inventory from supplier | GR | `INVENTORY` → 1400 | `RECEIPT_CREDIT` → 2000 AP |
| Ship goods (COGS) | GI | `COGS` → 5000 | `INVENTORY` → 1400 |
| Invoice customer | SI | `AR` → 1200 | 4000/4100 Revenue (per product) |
| Record customer payment | JE | 1100 Bank | `AR` → 1200 |
| Receive vendor invoice | PI | Expense/Inventory | `AP` → 2000 |
| Pay vendor | JE | `AP` → 2000 | `BANK_DEFAULT` → 1100 |

---

## Multi-Currency Workflow

**One currency per journal entry (SAP model):**

> [!IMPORTANT]
> Every journal entry uses exactly one `TransactionCurrency`. If an event happened in USD, every line records an amount in USD. Line amounts are converted to `BaseCurrency` using the single header-level `ExchangeRate`. Mixed-currency entries within one posting are forbidden.

**Transaction Flow:**

1. **Event** — e.g., "Received $500 from a client"
2. **AI Proposal** — GPT-4o identifies `TransactionCurrency: USD`, `ExchangeRate: 82.50`, and per-line `AccountCode` + `Amount` (in USD)
3. **Validation** — `Proposal.Validate()` verifies balance in base currency
4. **Commit** — `journal_lines` stores both transaction-currency and base-currency amounts

---

## Roadmap

**MVP is complete.** All core accounting, order management, inventory, procurement, reporting, authentication, and web UI phases are delivered.

Next phases are tax and compliance features. See [`docs/Tax_Regulatory_Future_Plan.md`](docs/Tax_Regulatory_Future_Plan.md) for GST, TDS/TCS, period locking, and GSTR export (Phases 22–30).
