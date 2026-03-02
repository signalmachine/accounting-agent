# One Final Implementation Plan

> **Purpose**: Single source of truth for the remaining work to ship a production-grade MVP.
> **Scope**: Typical company — inventory, sales orders, purchase orders, standard accounting.
> **Cut from scope**: Service job costing, rental billing, tax compliance (deferred to `Tax_Regulatory_Future_Plan.md`).
> **Last updated**: 2026-02-28
> **Current state**: Phases 1–14 + WF1–WF5 + WD0–WD1 complete. 70 integration tests passing. 24 migrations applied. MVP complete.

---

## Current System State

**Completed:**
- Tier 0 (bug fixes), Phases 1–7.5: ApplicationService, REPL/CLI adapters, RuleEngine wired into OrderService + InventoryService, AI Tool Architecture (ToolRegistry, InterpretDomainAction, 5 read tools, agentic loop)
- Phase 8: ReportingService — `GetAccountStatement`, `/statement` REPL command, read tools
- Phases 9–10: Materialized views (migrations 014–015), `GetProfitAndLoss`, `GetBalanceSheet`, `RefreshViews`, `/pl` `/bs` `/refresh` REPL commands, 4 new integration tests
- Phase WF1: Web server (chi router), `POST /api/chat/message` SSE endpoint, `POST /api/chat/confirm`, embedded static chat shell
- Phase WF2: Authentication — `users` table + admin seed (migrations 016–018), `UserService`, `AuthenticateUser`/`GetUser` on `ApplicationService`, JWT HS256 httpOnly cookie (1-hour), `RequireAuth` middleware, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`, audit columns on `journal_entries`/`sales_orders`/`documents`. Default admin: `admin` / `Admin@1234`.
- Phase WF3: Frontend scaffold — `github.com/a-h/templ v0.3.977` + CLI. HTMX 2.x, htmx-ext-sse, Alpine.js 3.x, Chart.js 4.x vendored in `web/static/js/`. Tailwind CSS v4.2.1 standalone CLI (`tailwindcss.exe`), `web/static/css/input.css`, `app.css` (25 KB) generated. Layouts: `login_layout`, `app_layout` (collapsible sidebar, header, flash, live chat slide-over), `chat_layout`, `modal_shell`. Pages: `login`, `dashboard` (quick-action cards). `RequireAuthBrowser` middleware (302 → `/login`). `internal/adapters/web/pages.go` (form login/logout, dashboard, `buildAppLayoutData`). `handlers.go` updated: `/static/*` file server, `GET`/`POST /login`, `POST /logout`, `/dashboard` guard, all domain page stubs. `Makefile` (`generate`, `css`, `dev`, `build`, `test`).
- Phase WF4: Core accounting screens — `internal/adapters/web/accounting.go` (page + API handlers). Five pages: `trial_balance.templ` (debit/credit split, balance indicator), `pl_report.templ` (year/month selector, revenue/expense sections, net income), `balance_sheet.templ` (date picker, assets/liabilities/equity, IsBalanced indicator), `account_statement.templ` (account code + date range filter, CSV export via `format=csv`), `journal_entry.templ` (Alpine.js dynamic lines, validate + post buttons). API endpoints: `GET trial-balance`, `GET accounts/{code}/statement`, `GET reports/pl`, `GET reports/balance-sheet`, `POST reports/refresh`, `POST journal-entries`, `POST journal-entries/validate`. Dashboard updated with Journal Entry shortcut card. REPL reporting commands superseded.
- Phase 11: Vendor master — migrations 019 (vendors table), 020 (3 seed vendors: V001/V002/V003), 021 (GIN trigram index on vendors.name). `internal/core/vendor_model.go` (`Vendor`, `VendorInput` structs, `VendorService` interface). `internal/core/vendor_service.go` (`CreateVendor`, `GetVendors`, `GetVendorByCode`). `ListVendors` + `CreateVendor` on `ApplicationService`. `VendorsResult`, `VendorResult`, `CreateVendorRequest` types. `vendorService` wired into `NewAppService` in both `cmd/app` and `cmd/server`. 4 AI tools registered: `get_vendors`, `search_vendors` (pg_trgm), `get_vendor_info` (read), `create_vendor` (write). 6 integration tests, all passing. 45 total tests.
- Phase 12: Purchase Orders — migration 022 (purchase_orders + purchase_order_lines + PO document type 'per_fy'). `internal/core/purchase_order_model.go` (`PurchaseOrder`, `PurchaseOrderLine`, `PurchaseOrderLineInput`, `PurchaseOrderService` interface). `internal/core/purchase_order_service.go` (`CreatePO`, `ApprovePO` with gapless PO-YYYY-NNNNN numbering via DocService, `GetPO`, `GetPOs`). `ListPurchaseOrders`, `CreatePurchaseOrder`, `ApprovePurchaseOrder` on `ApplicationService`. `PurchaseOrdersResult`, `PurchaseOrderResult`, `CreatePurchaseOrderRequest`, `POLineInput` types. `purchaseOrderService` wired into `NewAppService` in both `cmd/app` and `cmd/server`. 4 AI tools: `get_purchase_orders` (read), `get_open_pos` (read), `create_purchase_order` (write/nil), `approve_po` (write/nil). 7 integration tests in `purchase_order_integration_test.go`. 52 total tests.
- Phase 13: Goods Receipt Against PO — migration 023 (`po_line_id` on `inventory_movements` + `received_at` on `purchase_orders`). `ReceivePO` on `PurchaseOrderService`. `ReceivePurchaseOrder` on `ApplicationService`. `POReceiptResult`, `ReceivePORequest`, `ReceivedLineInput` types. `InventoryService.ReceiveStock` extended with `poLineID *int`. 2 AI tools: `check_stock_availability` (read), `receive_po` (write/nil). 4 new subtests in `TestPurchaseOrder_ReceivePO`. 65 total tests.
- Phase 14: Vendor Invoice + AP Payment — migration 024 (`invoice_number`, `invoice_date`, `invoice_amount`, `pi_document_number`, `invoiced_at`, `paid_at` on `purchase_orders`). `RecordVendorInvoice` (validates RECEIVED, posts PI document with gapless `PI-YYYY-NNNNN` number, warns on >5% amount deviation, status → INVOICED). `PayVendor` (validates INVOICED, posts `DR AP / CR Bank` atomically, status → PAID). Both on `ApplicationService` with `VendorInvoiceRequest`/`PayVendorRequest`/`VendorInvoiceResult`/`PaymentResult`. 4 AI tools: `get_ap_balance`, `get_vendor_payment_history` (read), `record_vendor_invoice`, `pay_vendor` (write/nil). `TestPurchaseOrder_FullLifecycle` (5 subtests). 70 total tests.
- Phase WF5: AI Chat Home + Document Upload — `GET /` serves `chat_home.templ` (full-screen chat, `ChatLayout`). `POST /chat` SSE streaming (answer/clarification/action_card/proposal/done events). `POST /chat/upload` image upload (JPG/PNG/WEBP, UUID filename, `UPLOAD_DIR`, 30-min cleanup). `POST /chat/confirm` executes write tools via `ExecuteWriteTool` (approve_po, create_vendor, create_purchase_order, receive_po, record_vendor_invoice, pay_vendor). `POST /chat/clear` stateless reset. `Attachment` struct in `internal/ai` and `internal/app`; `InterpretDomainAction` variadic (`...Attachment`) for backward compat; vision content list built from base64 data URLs. `pendingStore` TTL 15 min + background purge. `sessionStorage['chat_history']` shared between chat home and app-layout slide-over. New files: `internal/adapters/web/ai.go`, `web/templates/pages/chat_home.templ`. `go.mod`: `github.com/a-h/templ` promoted to direct dependency.
- Phase WD0: Sales Order Web UI — `GetOrder(ctx, ref, companyCode)` added to `ApplicationService`. `internal/adapters/web/orders.go` (page + API handlers for customers, products, stock, orders, lifecycle actions). Templates: `customers_list.templ`, `products_list.templ`, `stock_levels.templ`, `orders_list.templ` (status filter pills), `order_detail.templ` (Alpine.js lifecycle buttons → POST API → reload), `order_wizard.templ` (Alpine.js dynamic lines, auto-fill unit price), `order_shared.templ` (`orderStatusBadge`). All `notImplemented` stubs replaced for WD0 routes. `POST /sales/orders/new` form submit → redirect to detail. API: GET customers/products/orders, GET/POST orders/{ref}, POST confirm/ship/invoice/payment.

**Tech stack:** Go 1.25.3 · PostgreSQL 12+ (pgx, no ORM) · OpenAI GPT-4o (Responses API, strict JSON schema) · `shopspring/decimal` · `google/uuid` · `joho/godotenv`

**Architecture (strictly enforced):**
```
Layer 4 — Adapters:  internal/adapters/repl/  internal/adapters/cli/  internal/adapters/web/
Layer 3 — App:       internal/app/   (ApplicationService interface + impl — no fmt.Println, no HTTP types)
Layer 2 — Core:      internal/core/  (Ledger, OrderService, InventoryService, RuleEngine, ReportingService)
Layer 1 — Infra:     internal/db/    internal/ai/
```

**Non-negotiable rules:**
- Adapters call ApplicationService only — never domain services directly
- No ORM — raw SQL, parameterized queries, pgx
- Immutable ledger — append-only; corrections via compensating entries
- AI is advisory only — every write requires explicit human confirmation
- Company scoping on every business query (`company_id` filter)
- `shopspring/decimal` for all monetary values — no `float64`

---

## Migration Map

| File | Phase | Status | Description |
|------|-------|--------|-------------|
| 001–012 | Phases 1–6 | ✅ Applied | Core schema, seed data, account_rules |
| 013_pg_trgm_search.sql | Phase 7.5 | ✅ Applied | pg_trgm + GIN indexes |
| 014_reporting_views.sql | Phase 9 | ✅ Applied | mv_account_period_balances |
| 015_trial_balance_view.sql | Phase 10 | ✅ Applied | mv_trial_balance |
| 016_users.sql | Phase WF2 | ✅ Applied | Users table |
| 017_seed_admin_user.sql | Phase WF2 | ✅ Applied | Seed admin for Company 1000 |
| 018_audit_trail_columns.sql | Phase WF2 | ✅ Applied | created_by_user_id audit columns |
| 019_vendors.sql | Phase 11 | ✅ Applied | Vendor master table |
| 020_seed_vendors.sql | Phase 11 | ✅ Applied | Seed 3 vendors for Company 1000 (V001/V002/V003) |
| 021_vendor_trgm_index.sql | Phase 11 | ✅ Applied | GIN trigram index on vendors.name |
| 022_purchase_orders.sql | Phase 12 | ✅ Applied | Purchase orders + PO lines + PO doc type |
| 023_po_link.sql | Phase 13 | ✅ Applied | po_line_id on inventory_movements + received_at on purchase_orders |
| 024_vendor_invoice_payment.sql | Phase 14 | ✅ Applied | Invoice + payment columns on purchase_orders |

---

## Phase Sequence

```
WF2  →  WF3  →  WF4  →  Phase 11  →  Phase 12  →  Phase 13  →  Phase 14  →  WF5  →  WD0  →  WD1
 ✅      ✅      ✅        ✅            ✅            ✅            ✅          ✅      ✅      ✅
auth   templ  acctg      vendor        PO          GR from        AP        AI chat  sales   vendor
       stack  screens    master       create        PO            pay        home    order    PO
                                                                             +docs    UI      UI
```

> **WF5 position**: WF5 (AI chat home + document upload) is placed after the buy-and-resell domain phases so that the document upload pipeline can surface vendor invoices, purchase receipts, and bank statements naturally. WF5 can begin in parallel with Phase 14 once WF4 is complete.
>
> **WD0 / WD1 position**: Domain UI phases follow after WF5 so users get the full AI-assisted + form-based experience from day one.

---

## Phase WF2 — Authentication

**Goal**: Secure multi-user access. JWT-based stateless sessions. Audit trail on business records.

**Pre-requisites**: Phase WF1 complete.

### Migrations

**`migrations/016_users.sql`**
```sql
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(200) NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(30) NOT NULL DEFAULT 'ACCOUNTANT',
    is_active BOOL DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, username),
    UNIQUE(company_id, email)
);
```
Roles: `ACCOUNTANT` · `FINANCE_MANAGER` · `ADMIN`

**`migrations/017_seed_admin_user.sql`**
Seed one admin user for Company 1000. bcrypt-hash from `ADMIN_INITIAL_PASSWORD` env var, or print a random default at first boot.

**`migrations/018_audit_trail_columns.sql`**
```sql
ALTER TABLE journal_entries ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE sales_orders     ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE documents        ADD COLUMN IF NOT EXISTS created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL;
```

### Role Capabilities

| Role | Rights |
|------|--------|
| `ACCOUNTANT` | Read all; create orders, receive stock, propose journal entries |
| `FINANCE_MANAGER` | All ACCOUNTANT + approve POs, commit AI proposals, cancel invoiced orders, lock periods |
| `ADMIN` | All FINANCE_MANAGER + manage users, edit account rules |

Role enforcement lives in `ApplicationService` methods, not only in HTTP middleware.

### API Endpoints

- `POST /api/auth/login` — bcrypt-verify credentials, issue JWT in httpOnly `Set-Cookie` (1-hour expiry)
- `POST /api/auth/logout` — clear cookie
- `GET /api/auth/me` — return `{ username, role, company_code }`

JWT payload: `{ user_id, company_id, role, exp }`

Auth middleware: all `/api/` routes require valid JWT cookie → return 401 otherwise.

### ApplicationService additions

```go
AuthenticateUser(ctx context.Context, username, password string) (*UserSession, error)
GetUser(ctx context.Context, userID int) (*UserResult, error)
```

### Tasks

- [x] `migrations/016_users.sql` — users table
- [x] `migrations/017_seed_admin_user.sql` — admin seed
- [x] `migrations/018_audit_trail_columns.sql` — audit columns
- [x] `internal/adapters/web/auth.go` — login, logout, me handlers + JWT middleware
- [x] `AuthenticateUser` + `GetUser` on `ApplicationService` interface + `appService` implementation
- [ ] Unit tests: JWT generation, validation (valid / expired / tampered token)
- [x] Apply migrations to test DB

### Acceptance Criteria

- `POST /api/auth/login` with valid credentials sets JWT cookie
- `GET /api/orders` without cookie returns 401
- Logout clears cookie
- `created_by_user_id` populated on journal entries posted via web

---

## Phase WF3 — Frontend Scaffold

**Goal**: Establish the Go/templ app shell. Login page, nav, layout. No Node.js — single `go build` binary.

**Pre-requisites**: Phase WF2 complete.

### Tech Stack

| Component | Choice |
|-----------|--------|
| Template engine | `a-h/templ` (type-safe Go templates, compile-time errors) |
| Interactivity | HTMX 2.x (partial page updates, SSE, forms) |
| Local UI state | Alpine.js 3.x (dropdowns, modals, toggles, chat state) |
| Styling | Tailwind CSS v4 (utility-first; CSS file committed, no runtime build) |
| Charts | Chart.js 4.x (~200 KB, no framework deps) |
| Icons | Heroicons (SVG inline, no JS icon library) |

JS libraries vendored into `web/static/js/` — no npm, no Node.js.

### Directory Structure

```
web/
  templates/
    layouts/
      login_layout.templ      (standalone auth layout)
      app_layout.templ        (sidebar + header + chat slide-over)
      chat_layout.templ       (AI chat home — minimal header, no sidebar)
      modal_shell.templ       (Alpine.js popup overlay)
    pages/                    (full-page components)
    partials/                 (HTMX swap targets)
    components/               (reusable UI components)
  static/
    css/
      input.css               (Tailwind source)
      app.css                 (Tailwind output — committed)
    js/
      htmx.min.js
      htmx-ext-sse.js
      alpine.min.js
      chart.min.js

internal/adapters/web/
  handlers.go     middleware.go    auth.go
  accounting.go   orders.go        vendors.go
  errors.go       ctx.go
```

### Layouts

**`login_layout.templ`** — centred card, company logo, no navigation. HTMX swaps error message in-place on POST failure.

**`app_layout.templ`** — all accounting app screens use this:
- `<body hx-boost="true">` — HTMX-enhanced navigation (no full page reloads)
- Left sidebar: collapsible via Alpine.js toggle. Sections: Sales ▼, Purchases ▼, Inventory ▼, Reports ▼, Settings ▼. Active item highlighted. "⌂ Home" at top.
- Top header: company name + FY badge, breadcrumb trail, "Ask AI" button (opens chat slide-over), user avatar + dropdown, hamburger for mobile.
- Flash message area — Alpine.js `x-show` with auto-dismiss.
- Chat slide-over: right-side drawer (`x-show`, `x-transition`) with full AI chat. Persists across page navigations via `sessionStorage`.
- Responsive: sidebar hidden on mobile, hamburger toggle; slide-over full-screen on mobile.

**`chat_layout.templ`** — AI chat home. Minimal header, no sidebar, full-height chat.

**`modal_shell.templ`** — full-screen Alpine.js overlay for popup forms (used in WF5 action cards).

### Makefile Targets

```makefile
generate:   templ generate ./web/templates/...
css:        tailwindcss -i web/static/css/input.css -o web/static/css/app.css --minify
dev:        make generate && make css && go run ./cmd/server   # or parallel watch commands
build:      make generate && make css && go build -o app-server.exe ./cmd/server
```

### Tasks

- [x] Install `templ` CLI: `go install github.com/a-h/templ/cmd/templ@latest`
- [x] Add `github.com/a-h/templ` to `go.mod`
- [x] Vendor HTMX 2.x, htmx-ext-sse, Alpine.js 3.x, Chart.js 4.x into `web/static/js/`
- [x] Install Tailwind CSS standalone CLI (`tailwindcss.exe`); create `web/static/css/input.css`; commit initial `app.css`
- [x] Create `web/templates/layouts/login_layout.templ`
- [x] Create `web/templates/layouts/app_layout.templ` (sidebar, header, flash, chat slide-over)
- [x] Create `web/templates/layouts/chat_layout.templ`
- [x] Create `web/templates/layouts/modal_shell.templ`
- [x] Auth middleware: `RequireAuthBrowser` redirects unauthenticated requests to `/login`; post-login redirect to `/dashboard`
- [x] Static files served at `/static/*` from `//go:embed web/static`
- [x] Makefile targets: `generate`, `css`, `dev`, `build`, `test`

### Acceptance Criteria

- `make dev` starts server with hot-reloading
- Login page renders; login succeeds and redirects to `/`
- Auth guard: unauthenticated `/dashboard` → 302 to `/login`
- Sidebar navigation renders; active item highlighted
- `hx-boost` navigation — browser network tab shows partial HTML, not full page reloads
- Template compile errors caught at `make generate`, not at runtime

---

## Phase WF4 — Core Accounting Screens ✅

**Goal**: Web screens replacing the REPL's primary accounting commands. After WF4, REPL is redundant for reporting.

**Status**: Complete.

### Delivered

**API endpoints** (all under `/api/companies/:code/`):
- `GET /api/companies/:code/trial-balance`
- `GET /api/companies/:code/accounts/:accountCode/statement?from=YYYY-MM-DD&to=YYYY-MM-DD`
- `GET /api/companies/:code/reports/pl?year=YYYY&month=MM`
- `GET /api/companies/:code/reports/balance-sheet?date=YYYY-MM-DD`
- `POST /api/companies/:code/reports/refresh`
- `POST /api/companies/:code/journal-entries` — commit manual JE
- `POST /api/companies/:code/journal-entries/validate` — validate without committing

**Browser pages**:
- `GET /reports/trial-balance` — full account table; debit/credit columns; balance indicator (green ✓ / red ⚠)
- `GET /reports/pl?year=&month=` — year/month selector; revenue + expense sections; net income card (green/red)
- `GET /reports/balance-sheet?date=` — date picker; assets/liabilities/equity sections; IsBalanced indicator
- `GET /reports/statement?account=&from=&to=&format=` — account code + date range filter; running balance column; CSV export (`format=csv`)
- `GET /accounting/journal-entry` — Alpine.js dynamic line form; live debit/credit balance check; Validate + Post buttons

**Files created**:
- `internal/adapters/web/accounting.go` — all page + API handlers, `buildProposal` helper
- `web/templates/pages/trial_balance.templ` + `_templ.go`
- `web/templates/pages/pl_report.templ` + `_templ.go`
- `web/templates/pages/balance_sheet.templ` + `_templ.go`
- `web/templates/pages/account_statement.templ` + `_templ.go`
- `web/templates/pages/journal_entry.templ` + `_templ.go`

**Files updated**:
- `handlers.go` — WF4 stubs replaced; journal-entries + validate routes added
- `dashboard.templ` — "coming soon" notice replaced with Journal Entry shortcut card

---

## Phase 11 — Vendor Master

**Goal**: Vendor master data. Foundation for purchase orders and AP cycle.

**Pre-requisites**: None (Vendor is an independent domain).

### Migration: `019_vendors.sql`

```sql
CREATE TABLE IF NOT EXISTS vendors (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    code VARCHAR(20) NOT NULL,
    name VARCHAR(200) NOT NULL,
    contact_person VARCHAR(100),
    email VARCHAR(200),
    phone VARCHAR(40),
    address TEXT,
    payment_terms_days INT DEFAULT 30,
    ap_account_code VARCHAR(20) DEFAULT '2000',
    default_expense_account_code VARCHAR(20),
    is_active BOOL DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, code)
);
```

### Migration: `020_seed_vendors.sql`

Seeds 3 vendors for Company 1000 (`ON CONFLICT DO NOTHING`): V001 Acme Supplies, V002 Global Tech Components, V003 Swift Logistics.

### Migration: `021_vendor_trgm_index.sql`

```sql
CREATE INDEX IF NOT EXISTS idx_vendors_name_trgm ON vendors USING gin(name gin_trgm_ops);
```

Consistent with migration 013 which added pg_trgm GIN indexes for accounts, customers, and products.

### Domain

- `internal/core/vendor_model.go` — `Vendor` struct (nullable pointer fields for optional columns), `VendorInput` creation input, `VendorService` interface
- `internal/core/vendor_service.go` — `NewVendorService(pool)` constructor + 3 interface methods:
  - `CreateVendor(ctx, companyID int, input VendorInput) (*Vendor, error)` — defaults APAccountCode=2000, PaymentTermsDays=30
  - `GetVendors(ctx, companyID int) ([]Vendor, error)` — filters is_active=true, ordered by code
  - `GetVendorByCode(ctx, companyID int, code string) (*Vendor, error)`

### ApplicationService additions

```go
ListVendors(ctx context.Context, companyCode string) (*VendorsResult, error)
CreateVendor(ctx context.Context, req CreateVendorRequest) (*VendorResult, error)
```

Supporting types in `internal/app/`:
- `result_types.go`: `VendorsResult{Vendors []core.Vendor}`, `VendorResult{Vendor *core.Vendor}`
- `request_types.go`: `CreateVendorRequest` (9 fields: CompanyCode, Code, Name, ContactPerson, Email, Phone, Address, PaymentTermsDays, APAccountCode, DefaultExpenseAccountCode)

### AI Tools registered in `buildToolRegistry`

| Tool | Type | Description |
|------|------|-------------|
| `get_vendors` | read | List all active vendors for the company |
| `search_vendors` | read | Trigram similarity search on name + ILIKE on code; uses GIN index from migration 021 |
| `get_vendor_info` | read | Get full details for a vendor by code |
| `create_vendor` | write | Propose creating a new vendor (requires confirmation; Handler=nil) |

Private helpers in `appService`: `getVendorsJSON`, `searchVendors` (uses `v.name % $2` similarity operator), `getVendorInfoJSON`, `vendorsToJSON`.

### Tasks

- [x] `migrations/019_vendors.sql`
- [x] `migrations/020_seed_vendors.sql`
- [x] `migrations/021_vendor_trgm_index.sql`
- [x] `internal/core/vendor_model.go`
- [x] `internal/core/vendor_service.go` + implementation
- [x] Wire `VendorService` into `NewAppService` constructor (both `cmd/app` and `cmd/server`)
- [x] `ListVendors` + `CreateVendor` on `ApplicationService` interface + `appService`
- [x] Register 4 AI tools
- [x] Integration test: create vendor, list vendors, company isolation (6 subtests, all passing)

### Acceptance Criteria

- ✅ `ListVendors` returns seeded vendors scoped to company
- ✅ `CreateVendor` fails if code already exists for that company (unique constraint enforced)
- ✅ AI agent can look up vendors via `get_vendors`, `search_vendors`, `get_vendor_info` tool calls
- ✅ AI agent can propose creating a vendor via `create_vendor` write tool (human confirmation required)

---

## Phase 12 — Purchase Orders

**Goal**: PO creation and approval with gapless PO number assigned on approval.

**Pre-requisites**: Phase 11 (vendor master exists).

### Migration: `022_purchase_orders.sql`

```sql
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

-- PO document type (gapless per-FY numbering)
INSERT INTO document_types (company_id, type_code, description, numbering_strategy)
SELECT id, 'PO', 'Purchase Order', 'per_fy'
FROM companies WHERE code = '1000'
ON CONFLICT DO NOTHING;
```

### Domain

`internal/core/purchase_order_model.go` — `PurchaseOrder`, `PurchaseOrderLine`, `PurchaseOrderLineInput` structs

`internal/core/purchase_order_service.go` — `PurchaseOrderService` interface:
- `CreatePO(ctx, companyID, vendorID int, poDate time.Time, lines []PurchaseOrderLineInput, notes string) (*PurchaseOrder, error)` — creates DRAFT, computes line totals
- `ApprovePO(ctx, poID int, docService DocumentService) error` — row-locks PO, assigns gapless PO number via `DocumentService`, sets `status = 'APPROVED'`, sets `approved_at`
- `GetPO(ctx, poID int) (*PurchaseOrder, error)`
- `GetPOs(ctx, companyID int, status string) ([]PurchaseOrder, error)`

### ApplicationService additions

```go
ListPurchaseOrders(ctx context.Context, companyCode, status string) (*PurchaseOrdersResult, error)
CreatePurchaseOrder(ctx context.Context, req CreatePurchaseOrderRequest) (*PurchaseOrderResult, error)
ApprovePurchaseOrder(ctx context.Context, poID int) (*PurchaseOrderResult, error)
```

### AI Tools

| Tool | Type |
|------|------|
| `get_purchase_orders` | read |
| `get_open_pos` | read |
| `create_purchase_order` | write |
| `approve_po` | write |

### Tasks

- [x] `migrations/022_purchase_orders.sql`
- [x] `internal/core/purchase_order_model.go`
- [x] `internal/core/purchase_order_service.go` + implementation
- [x] Wire into `NewAppService`
- [x] `ApplicationService` additions
- [x] Register 4 AI tools
- [x] Integration test: `CreatePO` → `ApprovePO` → assert PO number assigned, status `APPROVED`, company isolation

### Acceptance Criteria

- `CreatePO` returns a DRAFT PO with computed line totals
- `ApprovePO` assigns a gapless PO number (e.g. `PO-2026-00001`) and moves to APPROVED
- Approving an already-APPROVED PO is a no-op (idempotent)

---

## Phase 13 — Goods Receipt Against PO

**Goal**: Receive goods against an APPROVED PO, update inventory, book `DR Inventory / CR AP`.

**Pre-requisites**: Phase 12 (PO exists in APPROVED state). Phase 7 (RuleEngine resolves inventory account).

### Migration: `023_po_link.sql`

```sql
ALTER TABLE inventory_movements ADD COLUMN IF NOT EXISTS po_line_id INT NULL REFERENCES purchase_order_lines(id);
```

### Domain

Add to `PurchaseOrderService`:

`ReceivePO(ctx context.Context, poID, warehouseID int, receivedLines []ReceivedLine, ledger Ledger, docService DocumentService, inv InventoryService) error`
- Validate PO is APPROVED
- For each physical-goods line: call `InventoryService.ReceiveStock()` (weighted average cost update), set `po_line_id` on the movement record
- For each service/expense line: post `DR expense_account_code / CR AP` via `Ledger.Commit()`
- Status → `RECEIVED`; set `received_at`

### ApplicationService addition

```go
ReceivePurchaseOrder(ctx context.Context, req ReceivePORequest) (*POReceiptResult, error)
```

### AI Tools

| Tool | Type |
|------|------|
| `check_stock_availability` | read (enhanced with PO context) |
| `receive_po` | write |

### Tasks

- [x] `migrations/023_po_link.sql`
- [x] `ReceivePO` on `PurchaseOrderService`
- [x] `ReceivePurchaseOrder` on `ApplicationService` interface + `appService`
- [x] Register 2 AI tools
- [x] Integration test: `ApprovePO` → `ReceivePO` → verify `qty_on_hand` increased, `inventory_movements.po_line_id` set, `DR Inventory / CR AP` journal entry posted

### Acceptance Criteria

- Receiving a PO updates stock levels and creates the correct journal entry
- `inventory_movements` rows reference the PO line via `po_line_id`
- Cannot receive a PO that is not APPROVED

---

## Phase 14 — Vendor Invoice + AP Payment

**Goal**: Record vendor bill and make payment. Completes the procurement cycle.

**Pre-requisites**: Phase 13 (PO received).

### Domain

Add to `PurchaseOrderService`:

`RecordVendorInvoice(ctx context.Context, poID int, invoiceNumber string, invoiceDate time.Time, invoiceAmount decimal.Decimal, ledger Ledger, docService DocumentService) error`
- Creates and posts a `PI` document (gapless number)
- Posts: `DR Inventory / CR AP` (goods) or `DR Expense / CR AP` (services)
- Logs warning (not error) if `invoiceAmount` deviates > 5% from PO total
- Status → `INVOICED`; set `invoiced_at`

`PayVendor(ctx context.Context, poID int, bankAccountCode string, paymentDate time.Time, ledger Ledger) error`
- Posts: `DR AP / CR Bank`
- Status → `PAID`

### ApplicationService additions

```go
RecordVendorInvoice(ctx context.Context, req VendorInvoiceRequest) (*VendorInvoiceResult, error)
PayVendor(ctx context.Context, req PayVendorRequest) (*PaymentResult, error)
```

### AI Tools

| Tool | Type |
|------|------|
| `get_tds_cumulative` | read |
| `check_tds_threshold` | read |
| `record_vendor_invoice` | write |
| `pay_vendor` | write |

### Tasks

- [x] `RecordVendorInvoice` + `PayVendor` on `PurchaseOrderService`
- [x] `ApplicationService` additions
- [x] Register 4 AI tools (`get_ap_balance`, `get_vendor_payment_history`, `record_vendor_invoice`, `pay_vendor`)
- [x] Integration test: full lifecycle `CreatePO` → `ApprovePO` → `ReceivePO` → `RecordVendorInvoice` → `PayVendor`. Verify AP balance changes after payment.

### Acceptance Criteria

- Full procurement cycle works end-to-end
- AP balance clears after `PayVendor`
- Invoice amount deviation > 5% from PO total produces a logged warning (not a block)
- AI agent can propose vendor payment via tool call

---

## Phase WF5 — AI Chat Home + Document Upload

**Goal**: Full-screen AI chat at `/`. Natural language → action cards → web forms. Image/PDF/CSV document attachment.

**Pre-requisites**: Phase WF4. Phases 8–10 (ReportingService) complete.

### Chat Home Layout (`GET /`)

- Header: logo (left), company name + FY badge (centre), "Open App" ⊞ + user avatar (right)
- Welcome state: centred greeting, 2×3 quick-shortcut chips (Trial Balance, New Order, Stock Levels, etc.)
- Chat thread area: replaces welcome state on first message; scrollable
- Input bar (pinned bottom): multi-line textarea, paperclip button, send button

### Response Modes

**Mode A — Text Reply (conversational)**
- Streaming token-by-token via SSE
- Rendered as plain text bubble
- Used for: balance queries, lookups, clarifications, explanations

**Mode B — Action Card (write operations)**
- Rendered as structured summary card (not a text bubble)
- Entity icon, key fields, optional amber compliance warning banner
- Three buttons: **Edit & Submit (page)** · **Open in popup** · **Cancel**

```
┌──────────────────────────────────────────────────────┐
│  🧾 Sales Invoice                                    │
│  Customer:    Acme Corp (C001)                       │
│  Order ref:   SO-2026-00012                          │
│  Net:         ₹85,000                                │
│  Total AR:    ₹85,000                                │
│  Due date:    2026-03-25 (Net 30)                    │
│  [✏ Edit & Submit]  [⧉ Open in popup]  [✕ Cancel]  │
└──────────────────────────────────────────────────────┘
```

**Edit & Submit (page)**: navigate to full form page pre-populated with AI's proposed values.
**Open in popup**: load form in Alpine.js modal overlay; chat context visible behind; submit via HTMX without page reload.

### Proposal Store

- `sync.Map` keyed by UUID; value `ProposalEntry{Type, Params, ExpiresAt}`
- 15-minute TTL; background goroutine purges expired entries every 5 minutes
- `proposal_id` embedded in action card data attributes
- If expired when user clicks: form opens empty with "AI suggestion expired — please fill manually" flash

### SSE Events

`POST /chat` streams:

| Event | When |
|-------|------|
| `status` | Processing started |
| `answer` | Mode A token stream |
| `clarification` | Agent needs more info |
| `action_card` | Mode B — full action card HTML partial |
| `error` | Unrecoverable error |
| `done` | Stream complete |

### Session History

Stored in browser `sessionStorage` (not Alpine.js `x-data` — that is destroyed on hx-boost navigation).
On chat home load, Alpine initialises history array from `sessionStorage`. On every turn, updated history written back. Conversation survives navigation to `/dashboard` and back.

### Document Attachment

**Supported formats (initial rollout):**

| Phase | Formats | Processing |
|-------|---------|-----------|
| WF5 initial | JPG, PNG, WEBP | GPT-4o vision API (base64-encoded image_url block) |
| WF5 follow-on | PDF (text-based) | Text extraction via `github.com/ledongthuc/pdf` |
| WF5 follow-on | Scanned PDF | Page → PNG via `github.com/gen2brain/go-fitz` (CGo/MuPDF) |
| WF5 follow-on | XLSX/XLS, CSV | Parsed to markdown table via `github.com/xuri/excelize/v2` |

**Upload endpoint `POST /chat/upload`:**
- Accepts multipart form; max 10 MB per file; max 5 files per message
- MIME validated via `net/http.DetectContentType` (trust bytes, not extension)
- Whitelist: `image/jpeg`, `image/png`, `image/webp`, `application/pdf`, Excel MIME types, `text/csv`, `text/plain`
- Stored with UUID filename in `UPLOAD_DIR`; cleaned up 30 minutes after upload
- Returns: `{ attachment_id, filename, file_type, size_bytes, preview_text }`

### Templ Partials

- `chat_message_user.templ` — right-aligned user bubble
- `chat_message_ai_text.templ` — left-aligned AI text bubble; streamed token-by-token
- `chat_action_card.templ` — Mode B action card
- `chat_compliance_warning.templ` — amber banner (optional, shown on action card)
- `chat_result_card.templ` — green success card after form commit
- `chat_stream_cursor.templ` — blinking cursor during stream

### API Endpoints

- `GET /` — chat home page (not a redirect)
- `POST /chat` — calls `InterpretDomainAction` / `InterpretEvent`; SSE response
- `POST /chat/upload` — file upload; returns attachment metadata
- `POST /chat/confirm` — `{ token, action: "confirm"|"cancel" }` — executes via `CommitProposal`
- `POST /chat/clear` — clears session context

### Tasks

- [x] `web/templates/pages/chat_home.templ` (welcome state + thread area + input bar)
- [x] Chat partial templates (action_card, proposal cards rendered inline in chat_home.templ and app_layout.templ slide-over)
- [x] `internal/adapters/web/ai.go` — GET `/`, POST `/chat`, POST `/chat/upload`, POST `/chat/confirm`, POST `/chat/clear`
- [x] Proposal store (`sync.Map`, TTL 15 min, background purge goroutine every 5 min)
- [x] Update `app_layout.templ`: "Ask AI" button opens chat slide-over; shared `sessionStorage['chat_history']`
- [x] Remove WF4 placeholder: `GET /` serves chat home (not redirect to `/dashboard`)
- [x] `UPLOAD_DIR` env var; temp file cleanup goroutine (30-min TTL, purge every 10 min)
- [x] WF5 initial: image upload only (JPG/PNG/WEBP) — PDF/Excel parsers deferred to follow-on

### Acceptance Criteria

- `GET /` serves chat home (not a redirect)
- Text messages return streaming SSE; bubbles appear token by token
- Uploading an image shows file chip; AI context includes image
- Journal entry proposal renders action card with both submission paths
- "Open in popup" opens modal with form pre-filled from `proposal_id`
- Navigate to `/dashboard` and back → conversation restored from `sessionStorage`
- "Ask AI" slide-over in app header has same conversation history

---

## Phase WD0 — Sales Order Web UI

**Goal**: Web screens for customers, products, and the sales order lifecycle. No new domain code — all `ApplicationService` methods already exist.

**Pre-requisites**: Phase WF5 complete. Phases 6–7 (order + inventory domain) already complete.

### Screens

**Customers** (`/sales/customers`)
- List: code, name, outstanding AR balance, payment terms
- Detail `/sales/customers/:code`: contact info, order history, outstanding invoices
- Create: form with validation (code, name, email, payment terms)

**Products** (`/sales/products`)
- List: code, name, unit price, stock on hand (from `GetStockLevels`)
- Detail `/sales/products/:code`: description, price, stock history, linked inventory item

**Sales Orders** (`/sales/orders`)
- List: filterable by status (DRAFT / CONFIRMED / SHIPPED / INVOICED / PAID)
- Detail `/sales/orders/:ref`: header (customer, date, status badge), line items table, lifecycle action buttons
- New order wizard: customer select → product line items → submit

**Sales Order Lifecycle Actions** (HTMX partial updates — no page reload):
- DRAFT → CONFIRMED: "Confirm Order" button
- CONFIRMED → SHIPPED: "Ship Order" button (deducts stock, posts `DR COGS / CR Inventory`)
- SHIPPED → INVOICED: "Invoice Order" button (posts `DR AR / CR Revenue`)
- INVOICED → PAID: "Record Payment" button (posts `DR Bank / CR AR`)

### API Endpoints

- `GET /api/companies/:code/customers`
- `POST /api/companies/:code/customers`
- `GET /api/companies/:code/customers/:customerCode`
- `GET /api/companies/:code/orders`
- `POST /api/companies/:code/orders`
- `GET /api/companies/:code/orders/:ref`
- `POST /api/companies/:code/orders/:id/confirm`
- `POST /api/companies/:code/orders/:id/ship`
- `POST /api/companies/:code/orders/:id/invoice`
- `POST /api/companies/:code/orders/:id/payment`
- `GET /api/companies/:code/products`
- `GET /api/companies/:code/products/:productCode`

### Tasks

- [x] `internal/adapters/web/orders.go` — all customer, product, stock, and order handler functions (page + API)
- [x] `web/templates/pages/customers_list.templ` — table with code, name, email, credit limit, payment terms
- [x] `web/templates/pages/products_list.templ` — table with on-hand stock lookup; link to Stock Levels
- [x] `web/templates/pages/stock_levels.templ` — on-hand/reserved/available with color indicator; link to Product Catalog
- [x] `web/templates/pages/orders_list.templ` — status filter pills (All/Draft/Confirmed/Shipped/Invoiced/Paid), New Order button
- [x] `web/templates/pages/order_detail.templ` — header card, lifecycle buttons (Alpine.js fetch POST + reload), line items table, timeline
- [x] `web/templates/pages/order_wizard.templ` — Alpine.js dynamic lines, auto-fill unit price from product, form POST → redirect to detail
- [x] `web/templates/pages/order_shared.templ` — `orderStatusBadge` component (replaces separate partials file)
- [x] `GetOrder(ctx, ref, companyCode string) (*OrderResult, error)` added to `ApplicationService` interface + `appService`
- [x] Wire all endpoints into router (replaced all `notImplemented` stubs for WD0 routes)
- [ ] `customer_detail.templ`, `customer_form.templ` — deferred to WD0 follow-on
- [ ] `product_detail.templ` — deferred to WD0 follow-on

### Acceptance Criteria

- Sales order lifecycle (DRAFT → PAID) completable entirely from the web UI
- Stock levels update after Ship Order
- AR balance updates after Invoice Order
- Dashboard KPI cards reflect changes immediately after each lifecycle action
- REPL commands `/orders`, `/new-order`, `/confirm`, `/ship`, `/invoice`, `/payment` are superseded

---

## Phase WD1 — Vendor & Purchase Order Web UI

**Goal**: Web screens for vendors, purchase orders, goods receipt, and vendor payments. No new domain code — Phases 11–14 methods already exist.

**Pre-requisites**: Phase WD0 complete. Phases 11–14 (vendor + PO domain) complete.

### Screens

**Vendors** (`/purchases/vendors`)
- List: code, name, payment terms, outstanding AP balance
- Detail `/purchases/vendors/:code`: contact info, PO history, outstanding invoices
- Create: form (code, name, email, payment terms, AP account)

**Purchase Orders** (`/purchases/orders`)
- List: filterable by status (DRAFT / APPROVED / RECEIVED / INVOICED / PAID)
- Detail `/purchases/orders/:ref`: header (vendor, date, status badge), line items table, lifecycle action buttons
- New PO wizard: vendor select → product/expense line items → submit

**Purchase Order Lifecycle Actions** (HTMX partial updates):
- DRAFT → APPROVED: "Approve PO" button (assigns gapless PO number)
- APPROVED → RECEIVED: "Receive Goods" form (per-line quantity received)
- RECEIVED → INVOICED: "Record Vendor Invoice" form (invoice number, date, amount)
- INVOICED → PAID: "Pay Vendor" button (bank account select, payment date)

### API Endpoints

- `GET /api/companies/:code/vendors`
- `POST /api/companies/:code/vendors`
- `GET /api/companies/:code/vendors/:vendorCode`
- `GET /api/companies/:code/purchase-orders`
- `POST /api/companies/:code/purchase-orders`
- `GET /api/companies/:code/purchase-orders/:ref`
- `POST /api/companies/:code/purchase-orders/:id/approve`
- `POST /api/companies/:code/purchase-orders/:id/receive`
- `POST /api/companies/:code/purchase-orders/:id/invoice`
- `POST /api/companies/:code/purchase-orders/:id/pay`

### Tasks

- [x] `internal/adapters/web/vendors.go` — all vendor and PO handler functions (page + API)
- [x] `web/templates/pages/vendors_list.templ`, `vendor_form.templ` — vendor list and create form
- [x] `web/templates/pages/po_list.templ`, `po_detail.templ`, `po_wizard.templ` — PO lifecycle screens
- [x] Lifecycle forms inline in `po_detail.templ` (receive, invoice, pay) — partials not needed
- [x] `GetPurchaseOrder` added to `ApplicationService` interface + `appService` implementation
- [x] Wire all endpoints into router (browser + API routes)
- [ ] `vendor_detail.templ` — deferred to follow-on
- [ ] Dashboard KPI cards (AP balance, Cash) — deferred to follow-on

### Acceptance Criteria

- Full procurement lifecycle (PO → Approve → Receive → Invoice → Pay) completable from web UI
- Stock levels update after Receive Goods
- AP balance updates after Record Vendor Invoice
- AP balance clears after Pay Vendor
- Dashboard KPI cards (AP balance, Cash) reflect changes

---

## Summary Checklist

| Phase | Deliverable | Status |
|-------|-------------|--------|
| WF2 | Authentication (JWT, users table, audit trail) | ✅ |
| WF3 | Frontend scaffold (templ + HTMX + Alpine + Tailwind) | ✅ |
| WF4 | Core accounting screens (TB, P&L, BS, statement, JE) | ✅ |
| 11   | Vendor master | ✅ |
| 12   | Purchase orders (DRAFT → APPROVED) | ✅ |
| 13   | Goods receipt against PO | ✅ |
| 14   | Vendor invoice + AP payment | ✅ |
| WF5  | AI chat home + document upload | ✅ |
| WD0  | Sales order web UI (customers, products, SO lifecycle) | ✅ |
| WD1  | Vendor & PO web UI (vendor, PO, GR, invoice, pay) | ✅ |

**At WD1 completion, the system covers:**
- Multi-user web UI with login and audit trail
- Full double-entry accounting (journal entries, trial balance, P&L, balance sheet)
- Sales order lifecycle end-to-end (quote → invoice → payment)
- Procurement lifecycle end-to-end (PO → receive → vendor invoice → payment)
- Inventory management (stock levels, COGS, weighted average cost)
- AI-assisted entry via natural language + document upload
