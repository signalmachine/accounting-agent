# Accounting Agent — Implementation Plan

> **Purpose**: Incremental roadmap for evolving this system into a full-featured SMB accounting platform.
> **Key Principle**: Each phase is independently deliverable. Completing one phase makes the next measurably easier.
> **Last Updated**: 2026-02-25
> **Last Reviewed**: 2026-02-25 — see [Review Findings](#review-findings-2026-02-25) section below.

**Companion documents — read before implementing the affected phases:**

| Document | What it covers |
|---|---|
| [`docs/web_ui_plan.md`](web_ui_plan.md) | **Read first.** Web UI as primary interface: tech stack (React + Go REST), auth, Tier 2.5 web foundation phases (WF1–WF4), domain UI phases (WD0–WD3), AI chat panel, screen inventory, REPL deprecation timeline, CLI scope. Supersedes Phase 32. |
| [`docs/plan_gaps.md`](plan_gaps.md) | Known gaps, under-specified areas, and missing features in this plan. Tier 4 tax phases are under-specified and must be expanded before coding begins. Credit notes, stock adjustments, and opening balances are missing entirely. |
| [`docs/ai_agent_upgrade.md`](ai_agent_upgrade.md) | Required expansion of the AI agent role. The current Phase 31 plan is insufficient for non-expert users. AI capabilities should be woven into each tier as it is built, not deferred to Tier 5. |

---

## Design Philosophy

This system targets **small and medium-sized businesses** across a wide spectrum:

| Business Model | Examples | Key Needs |
|---|---|---|
| Inventory-based | Retail, distribution | Stock, COGS, purchase orders |
| Service-based | Consulting, agency | Time/material billing, project invoices |
| Job/Workshop | Auto repair, tailoring | Work orders, parts + labour, job costing |
| Rental/Asset | Equipment hire, property | Asset register, contracts, recurring billing |
| Mixed | Auto repair + spare parts | All of the above, by transaction type |

**Non-negotiable principles:**
- **Modular opt-in** — a pure service business runs without inventory or rental modules.
- **Configuration over code** — tax rates, account mappings, and business rules live in the database, not in `if/else` blocks.
- **UI is always a thin adapter** — no business logic in REPL, CLI, or Web handlers.
- **AI is advisory only** — every AI-proposed entry requires explicit human approval before any ledger write.
- **Immutable ledger** — corrections via compensating entries, never UPDATEs.
- **Core first, AI gradual** — accounting, inventory, and order management correctness is always the first priority. AI agent capabilities are upgraded in parallel but strictly incrementally and need-based: a domain's integration tests must pass before any AI tooling is added for that domain. If there is tension between core correctness and an AI feature, core correctness wins without exception.

---

## Current State

| Capability | Status |
|---|---|
| Core double-entry ledger, AI agent (GPT-4o), CLI/REPL | ✅ Complete |
| Technical debt: idempotent migrations, company-scoped balances, `CommitInTx` | ✅ Complete |
| Sales Order lifecycle (`DRAFT → CONFIRMED → SHIPPED → INVOICED → PAID`) | ✅ Complete |
| Inventory engine: warehouses, stock levels, reservations, COGS automation | ✅ Complete |
| **Tier 0**: Bug fixes — hardcoded currency, non-deterministic company load, AI loop depth | ✅ Complete |
| **Phase 1**: `internal/app/` — `ApplicationService` interface, result types, request types | ✅ Complete |
| **Phase 2**: `ApplicationService` implementation (`app_service.go`) | ✅ Complete |
| **Phase 3**: REPL adapter extraction — `internal/adapters/repl/` (repl, display, wizards) | ✅ Complete (minor caveat — see review notes) |
| **Phase 4**: CLI adapter — `internal/adapters/cli/` + slim `main.go` (<60 lines) | ✅ Complete |

**Tier 0 bugs — resolved:**

| # | Bug | Location | Fix Applied |
|---|---|---|---|
| 1 | `"INR"` hardcoded in GR and COGS proposals | `inventory_service.go:283, 513` | ✅ Now reads `baseCurrency` from DB |
| 2 | Company loaded with `LIMIT 1` — non-deterministic with multiple companies | `cmd/app/main.go:126–132` | ✅ Count check + `COMPANY_CODE` env var required when >1 company |
| 3 | AI clarification loop has no depth limit — can run forever | `adapters/repl/repl.go:227–231` | ✅ 3-round cap with redirect message |

---

## Architecture Target

```
Layer 4 — Interface Adapters (REPL / CLI / Web)
          parse input → call AppService → format output
                   ↓
Layer 3 — Application Service  [internal/app/]
          single interface all adapters call; no fmt.Println
                   ↓
Layer 2 — Domain Core  [internal/core/]
          Ledger, OrderService, InventoryService, RuleEngine, TaxEngine …
                   ↓
Layer 1 — Infrastructure  [internal/db/, internal/ai/]
          pgx pool, OpenAI client
```

---

## Phase Overview

```
Tier 0  Immediate bug fixes                        (do now, no phase number)

Tier 1  Foundation — UI decoupling
        Phase 1   Result types + AppService contract
        Phase 2   AppService implementation
        Phase 3   REPL adapter extraction
        Phase 4   CLI adapter + slim main.go

Tier 2  Business rules + basic reporting
        Phase 5   Rule engine schema + seed rules
        Phase 6   RuleEngine service + wire into OrderService
        Phase 7   Wire RuleEngine into InventoryService
        Phase 8   Account statement report
        Phase 9   Materialized views + P&L report
        Phase 10  Balance Sheet report

Tier 2.5  Web Foundation  ← see docs/web_ui_plan.md
        Phase WF1  REST API foundation (chi router, OpenAPI spec, JSON error format)
        Phase WF2  Authentication (JWT, users table, login/logout) + audit trail columns (created_by_user_id) + multi-user user lifecycle (invite, deactivate, role change)
        Phase WF3  Frontend scaffold (templ + HTMX + Alpine.js app shell)
        Phase WF4  Core accounting screens (dashboard, trial balance, statement, P&L, BS)
        Phase WF5  AI chat panel (SSE streaming, proposed action cards, document attachment: image upload → PDF → Excel/CSV phased rollout)

Tier 3  Business domain expansion — procurement
        Phase 11  Vendor master
        Phase WD0  Web UI: customers, products, sales orders (existing domains)
        Phase 12  Purchase order DRAFT + APPROVED
        Phase 13  Goods receipt against purchase order
        Phase 14  Vendor invoice + AP payment
        Phase WD1  Web UI: vendors, purchase order full lifecycle

Tier 3  Business domain expansion — service jobs
        Phase 15  Service categories + job order DRAFT/CONFIRMED
        Phase 16  Job progress: start + add lines
        Phase 17  Job completion + invoice + payment
        Phase 18  Inventory consumption for job material lines
        Phase WD2  Web UI: job orders full lifecycle (incl. material consumption)

Tier 3  Business domain expansion — rentals
        Phase 19  Rental asset master + contract DRAFT/ACTIVE
        Phase 20  Rental billing + asset return
        Phase 21  Security deposit + asset depreciation
        Phase WD3  Web UI: rental assets, contracts, billing, deposit refund
        [REPL deprecated and removed after WD3 — see docs/web_ui_plan.md §9]

Tier 4  Tax framework
        Phase 22  Tax rate schema + TaxEngine service (no invoicing changes yet)
        Phase 23  Tax-aware invoice posting (SalesOrder)
        Phase 24  Input tax on purchases (PurchaseOrder)
        Phase 25  GST rate seeds + jurisdiction resolver
        Phase 26  GST special cases: RCM, SEZ, composition dealer
        Phase 27  TDS schema + deduction on vendor payments
        Phase 28  TCS on customer receipts + TDS settlement
        Phase 29  Period locking
        Phase 30  GSTR-1 + GSTR-3B export

Tier 5  Scale & governance
        Phase 31  AI expansion: tool-calling architecture + web chat panel (full skills)
        Phase 32  [Superseded by Tier 2.5 — see docs/web_ui_plan.md]
        Phase 33  Workflow + approvals (role enforcement; user table already exists from WF2)
        Phase 34  External integrations
        Phase 35  Multi-branch support
```

---

## Review Findings (2026-02-25)

Full codebase review conducted against this plan. Status of all claimed completions verified.

### ✅ Verified Complete (Tier 0 + Phases 1–3)

All four items are implemented correctly and consistent with plan specifications.

### Issues Found

**Issue 1 — Phase 4 not started (documentation gap)**

`CLAUDE.md` listed Phase 4 as "Next up" implying it was in progress. A codebase review confirmed it has not been started:
- `internal/adapters/cli/` directory does not exist.
- `cmd/app/main.go` is 196 lines (target: <60); still contains the CLI `switch os.Args[1]` block and display helpers (`fetchCoA`, `fetchDocumentTypes`, `printTrialBalance`).

**Action:** Phase 4 status corrected to "Not started" in the Current State table above. No code changes required now — Phase 4 implementation deferred.

---

**Issue 2 — Wrong phase number in domain-service TODO comments**

The hardcoded account constants in domain services are annotated `TODO(phase4)`, but according to this plan they are not replaced until Phase 6 (OrderService) and Phase 7 (InventoryService):

```go
// internal/core/order_service.go:14
const arAccountCode = "1200"          // TODO(phase4) ← should be TODO(phase6)

// internal/core/inventory_service.go:14–22
const inventoryAccountCode = "1400"   // TODO(phase4) ← should be TODO(phase7)
const cogsAccountCode = "5000"        // TODO(phase4) ← should be TODO(phase7)
const defaultCreditAccountCode = "2000" // (no annotation — should be TODO(phase7))
```

**Action:** Fix TODO labels when implementing Phase 6 and Phase 7 respectively. No functional impact — constants are correct values.

---

**Issue 3 — Phase 3 acceptance criteria partially unmet**

Phase 3 acceptance criteria states: *"main.go no longer imports `internal/core` directly."*

`cmd/app/main.go` still imports `internal/core` for the `*core.Company` type (used in `loadDefaultCompany()` return value and passed to `repl.Run()`). This is a direct import of the domain package from what should be a wiring-only file.

**Action:** Resolve during Phase 4 — when the CLI adapter is extracted and `main.go` is slimmed to <60 lines, the `internal/core` import should be eliminated from `main.go` entirely.

---

**Issue 4 — Test count stated as 27, observed ~21**

`CLAUDE.md` and this plan reference "27 tests". The test runner output shows 21 distinct test function names. The discrepancy is likely due to subtests counted individually in the stated figure.

**Action:** After Phase 4 is implemented, run `go test ./internal/core -v -count=1` and update the canonical test count in both `CLAUDE.md` and this document.

---

## Tier 0 — Immediate Bug Fixes

**✅ All three bugs resolved. Section retained for historical reference.**

- [x] In `inventory_service.go` lines 283 and 513: replaced `"INR"` with DB query for `companies.base_currency`.
- [x] In `cmd/app/main.go:126–132`: replaced `LIMIT 1` with a company count check; requires `COMPANY_CODE` env var when multiple companies exist.
- [x] In `adapters/repl/repl.go:227–231`: added `rounds` counter; breaks after 3 rounds with message *"Could not produce a proposal. Try a slash command instead — type /help."*
- [x] `go test ./internal/core -v` — all tests pass.

---

## Tier 1 — UI Foundation

### Phase 1: Result Types + Application Service Contract

**Goal**: Define the interface boundary between UI adapters and business logic. No implementation yet.

**Pre-requisites**: Tier 0 fixes committed.

**Why first**: Every future phase adds REPL commands. Establishing the contract now means all future phases just add methods to one interface rather than patching `main.go`.

**Tasks:**

- [ ] Create `internal/app/result_types.go`. Define output structs:
  - `TrialBalanceResult{CompanyCode, CompanyName, Currency string, Accounts []AccountBalance}`
  - `OrderResult{Order *core.SalesOrder}`
  - `OrderListResult{Orders []core.SalesOrder, CompanyCode string}`
  - `StockResult{Levels []core.StockLevel, CompanyCode string}`
  - `CustomerListResult{Customers []core.Customer}`
  - `ProductListResult{Products []core.Product}`
  - `WarehouseListResult{Warehouses []core.Warehouse}`
  - `AIResult{Proposal *core.Proposal, ClarificationMessage string, IsClarification bool}`
- [ ] Create `internal/app/request_types.go`. Define input structs:
  - `CreateOrderRequest{CompanyCode, CustomerCode, Currency, OrderDate, Notes string; ExchangeRate decimal; Lines []OrderLineInput}`
  - `ReceiveStockRequest{CompanyCode, ProductCode, WarehouseCode, CreditAccountCode, MovementDate string; Qty, UnitCost decimal}`
- [ ] Create `internal/app/service.go`. Define `ApplicationService` interface with these methods (no implementation):
  - `GetTrialBalance(ctx, companyCode string) (*TrialBalanceResult, error)`
  - `ListCustomers(ctx, companyCode string) (*CustomerListResult, error)`
  - `ListProducts(ctx, companyCode string) (*ProductListResult, error)`
  - `ListOrders(ctx, companyCode string, status *string) (*OrderListResult, error)`
  - `CreateOrder(ctx context.Context, req CreateOrderRequest) (*OrderResult, error)`
  - `ConfirmOrder(ctx, ref, companyCode string) (*OrderResult, error)`
  - `ShipOrder(ctx, ref, companyCode string) (*OrderResult, error)`
  - `InvoiceOrder(ctx, ref, companyCode string) (*OrderResult, error)`
  - `RecordPayment(ctx, ref, bankCode, companyCode string) (*OrderResult, error)`
  - `ListWarehouses(ctx, companyCode string) (*WarehouseListResult, error)`
  - `GetStockLevels(ctx, companyCode string) (*StockResult, error)`
  - `ReceiveStock(ctx context.Context, req ReceiveStockRequest) error`
  - `InterpretEvent(ctx context.Context, text, companyCode string) (*AIResult, error)`
  - `CommitProposal(ctx context.Context, proposal core.Proposal) error`
- [ ] `go build ./...` compiles.

**Acceptance criteria**: `internal/app` package compiles. No implementation exists yet — this is only contracts.

---

### Phase 2: Application Service Implementation

**Goal**: Implement `ApplicationService` so all existing REPL behaviour is callable through the new interface.

**Pre-requisites**: Phase 1 complete.

**Tasks:**

- [ ] Create `internal/app/app_service.go`.
- [ ] Define `appService` struct:
  ```go
  type appService struct {
      pool             *pgxpool.Pool
      ledger           *core.Ledger
      docService       core.DocumentService
      orderService     core.OrderService
      inventoryService core.InventoryService
      agent            *ai.Agent
  }
  ```
- [ ] Implement `NewAppService(...)` constructor.
- [ ] Implement each method in the interface:
  - Call the corresponding domain service.
  - Map returned domain types to result types.
  - Move `resolveOrderByRef()` logic (numeric ID vs order number lookup) from `main.go` into here.
  - `InterpretEvent()` calls `agent.InterpretEvent()`, fetches CoA + document types from DB, maps to `AIResult`.
- [ ] No `fmt.Println`, no ANSI codes, no display logic anywhere in this file.
- [ ] `go build ./...` compiles. Existing 27 tests still pass (tests call domain services directly, unaffected).

**Acceptance criteria**: `appService` fully satisfies the `ApplicationService` interface. No behaviour changes yet.

---

### Phase 3: REPL Adapter Extraction

**Goal**: Move the REPL loop and all display formatting out of `main.go` into a dedicated package.

**Pre-requisites**: Phase 2 complete.

**Tasks:**

- [ ] Create `internal/adapters/repl/display.go`. Move all `print*` functions from `main.go` verbatim. Change them to accept domain/result types as parameters — no direct DB or service calls inside them.
- [ ] Create `internal/adapters/repl/wizards.go`. Move `handleNewOrder()` interactive wizard. Replace direct `orderService` calls with calls to `ApplicationService`.
- [ ] Create `internal/adapters/repl/repl.go`. Move `runREPL()` and `dispatchSlash()`. Replace all direct domain service calls with `ApplicationService` calls. The REPL struct takes `app.ApplicationService` + `*bufio.Reader`.
- [ ] The AI clarification loop lives here (it is a UI concern). The 3-round cap from Tier 0 must be enforced here.
- [ ] Update `cmd/app/main.go` to call `repl.Run(appService, reader, company)` instead of `runREPL(...)`.
- [ ] Manual smoke test: boot REPL, run `/bal`, `/orders`, confirm an order, type a natural language event.

**Acceptance criteria**: REPL behaviour is identical to before. `main.go` no longer imports `internal/core` directly. All 27 tests pass.

> **Review note (2026-02-25):** Phase 3 is complete with one open item: `cmd/app/main.go` still imports `internal/core` for the `*core.Company` type. This will be fully resolved in Phase 4 when `main.go` is slimmed to a wiring-only file.

---

### Phase 4: CLI Adapter + Slim Main ✅ Complete

**Goal**: Extract CLI argument parsing and reduce `main.go` to a wiring file.

**Pre-requisites**: Phase 3 complete.

**Tasks:**

- [x] Create `internal/adapters/cli/cli.go`. CLI `switch os.Args[1]` logic moved here. All domain calls go through `ApplicationService`.
- [x] Added `LoadDefaultCompany` and `ValidateProposal` to `ApplicationService` interface and `appService` implementation.
- [x] Updated `repl.Run()` to remove `*core.Company` parameter — REPL loads company internally via `svc.LoadDefaultCompany()`.
- [x] Updated `cmd/app/main.go`: wiring only. Dispatches to `cli.Run` or `repl.Run`. 48 lines.
- [x] `go build ./...` compiles. All 27 tests pass.

**Acceptance criteria**: `main.go` < 60 lines ✅ (48 lines). No business logic in any adapter ✅. `internal/core` not imported by adapter packages directly ✅.

---

## Tier 2 — Business Rules + Basic Reporting

### Phase 5: Rule Engine Schema + Seed Rules

**Goal**: Create the database foundation to store configurable account mappings. No Go code changes yet.

**Pre-requisites**: Phase 4 complete.

**Tasks:**

- [ ] Create `migrations/011_account_rules.sql`:
  ```sql
  CREATE TABLE IF NOT EXISTS account_rules (
      id SERIAL PRIMARY KEY,
      company_id INT NOT NULL REFERENCES companies(id),
      rule_type VARCHAR(40) NOT NULL,
      account_code VARCHAR(20) NOT NULL,
      qualifier_key VARCHAR(40),
      qualifier_value VARCHAR(40),
      priority INT DEFAULT 0,
      effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
      effective_to DATE
  );
  CREATE UNIQUE INDEX IF NOT EXISTS idx_account_rules_lookup
      ON account_rules(company_id, rule_type,
          COALESCE(qualifier_key,''), COALESCE(qualifier_value,''));
  ```
- [ ] Create `migrations/012_seed_account_rules.sql`. Insert rules for Company 1000 matching current hardcoded constants:

  | rule_type | account_code | Notes |
  |---|---|---|
  | `AR` | `1200` | Accounts Receivable |
  | `AP` | `2000` | Accounts Payable |
  | `INVENTORY` | `1400` | Inventory asset |
  | `COGS` | `5000` | Cost of Goods Sold |
  | `BANK_DEFAULT` | `1100` | Default bank |
  | `RECEIPT_CREDIT` | `2000` | Default credit on stock receipt |

  All with `ON CONFLICT DO NOTHING`.

- [ ] Apply migrations to both live and test DBs.
- [ ] Verify: `go test ./internal/core -v` — all 27 tests still pass (nothing wired yet).

**Acceptance criteria**: `account_rules` table exists in DB with 6 seed rows. No Go code changes.

---

### Phase 6: RuleEngine Service + Wire into OrderService

**Goal**: Implement the RuleEngine and eliminate the hardcoded AR account constant in `OrderService`.

**Pre-requisites**: Phase 5 complete.

**Tasks:**

- [ ] Create `internal/core/rule_engine.go`. Define interface and implementation:
  ```go
  type RuleEngine interface {
      ResolveAccount(ctx context.Context, companyID int, ruleType string) (string, error)
  }
  ```
  Implementation queries `account_rules` by `(company_id, rule_type)`, ordered by `priority DESC`, returns first match. Returns a descriptive error if no rule exists.
- [ ] Add `RuleEngine` parameter to `NewOrderService(pool, ruleEngine)` constructor.
- [ ] In `order_service.go`: replace `arAccountCode` constant usage in `InvoiceOrder()` and `RecordPayment()` with `ruleEngine.ResolveAccount(ctx, companyID, "AR")`.
- [ ] Delete the `arAccountCode = "1200"` constant.
- [ ] Update `ApplicationService` constructor to create and inject `RuleEngine`.
- [ ] Update integration tests: they rely on seeded rules being present. Run `DATABASE_URL=$TEST_DATABASE_URL go run ./cmd/verify-db` first.
- [ ] Add unit test for `RuleEngine`: correct resolution, company isolation, missing rule returns error.
- [ ] All tests pass.

**Acceptance criteria**: `arAccountCode` constant removed. `InvoiceOrder` and `RecordPayment` resolve AR account dynamically. All 27+ tests pass.

---

### Phase 7: Wire RuleEngine into InventoryService

**Goal**: Eliminate the hardcoded Inventory and COGS account constants in `InventoryService`.

**Pre-requisites**: Phase 6 complete (RuleEngine exists).

**Tasks:**

- [ ] Add `RuleEngine` parameter to `NewInventoryService(pool, ruleEngine)` constructor.
- [ ] In `inventory_service.go`:
  - Replace `inventoryAccountCode = "1400"` in `ReceiveStock()` and `ShipStockTx()` with `ruleEngine.ResolveAccount(ctx, companyID, "INVENTORY")`.
  - Replace `cogsAccountCode = "5000"` in `ShipStockTx()` with `ruleEngine.ResolveAccount(ctx, companyID, "COGS")`.
  - Replace `defaultReceiptCreditAccountCode = "2000"` in `ReceiveStock()` with `ruleEngine.ResolveAccount(ctx, companyID, "RECEIPT_CREDIT")`.
- [ ] Delete the three constant declarations.
- [ ] Update `ApplicationService` constructor to pass `RuleEngine` to `InventoryService`.
- [ ] All tests pass.

**Acceptance criteria**: All four hardcoded account constants removed from domain services. All 27+ tests pass.

---

### Phase 8: Account Statement Report

**Goal**: Add an account-level ledger statement without materialized views — a direct query, always fresh.

**Pre-requisites**: Phase 4 (ApplicationService wired).
**Can start in parallel with Phases 6–7.**

**Tasks:**

- [ ] Create `internal/core/reporting_service.go`. Define:
  ```go
  type StatementLine struct {
      PostingDate   string
      DocumentDate  string
      Narration     string
      Reference     string
      Debit         decimal.Decimal
      Credit        decimal.Decimal
      RunningBalance decimal.Decimal
  }
  type ReportingService interface {
      GetAccountStatement(ctx, companyCode, accountCode, fromDate, toDate string) ([]StatementLine, error)
  }
  ```
- [ ] Implement `GetAccountStatement()` — query `journal_lines` joined to `journal_entries` and `accounts`, filtered by company, account code, and date range. Compute running balance in Go by iterating rows ordered by `posting_date ASC, je.id ASC`.
- [ ] Add `ReportingService` to `AppService` struct and inject into constructor.
- [ ] Add `GetAccountStatement(ctx, companyCode, accountCode, fromDate, toDate string)` to `ApplicationService` interface.
- [ ] REPL command: `/statement <account-code> [from-date] [to-date]` — prints date, narration, debit, credit, running balance.
- [ ] Integration test: post 3 journal entries → statement returns correct lines and running balances.

**Acceptance criteria**: `/statement 1200` shows all AR movements with running balance.

---

### Phase 9: Materialized Views + P&L Report

**Goal**: Add period-based Profit & Loss report backed by a materialized view.

**Pre-requisites**: Phase 8 (ReportingService scaffolding exists).

**Tasks:**

- [ ] Create `migrations/013_reporting_views.sql`:
  - `mv_account_period_balances` — net balance per account per calendar year/month:
    ```sql
    company_id, account_id, account_code, account_name, account_type,
    year INT, month INT, debit_total, credit_total, net_balance
    ```
  - `CREATE UNIQUE INDEX` on `(company_id, account_id, year, month)` to allow `REFRESH CONCURRENTLY`.
- [ ] Add to `ReportingService` interface and implement:
  ```go
  type PLReport struct {
      CompanyCode  string
      Year, Month  int
      Revenue      []AccountLine
      Expenses     []AccountLine
      NetIncome    decimal.Decimal
  }
  GetProfitAndLoss(ctx, companyCode string, year, month int) (*PLReport, error)
  RefreshViews(ctx context.Context) error
  ```
  `GetProfitAndLoss` queries `mv_account_period_balances` filtered to revenue/expense account types.
- [ ] Add to `ApplicationService` interface and implement in `appService`.
- [ ] REPL commands: `/pl [year] [month]` (defaults to current month), `/refresh`.
- [ ] Integration test: full order lifecycle → `/pl` shows revenue credit and COGS debit.

**Acceptance criteria**: `/pl` prints a formatted P&L. `/refresh` runs without error.

---

### Phase 10: Balance Sheet Report

**Goal**: Add a Balance Sheet report and verify the ledger is always in balance.

**Pre-requisites**: Phase 9 (materialized views exist).

**Tasks:**

- [ ] Create `mv_trial_balance` materialized view in a new migration: cumulative balance per account (all time), per company.
- [ ] Add to `ReportingService` interface and implement:
  ```go
  type BSReport struct {
      CompanyCode string
      AsOfDate    string
      Assets      []AccountLine
      Liabilities []AccountLine
      Equity      []AccountLine
      IsBalanced  bool
  }
  GetBalanceSheet(ctx, companyCode, asOfDate string) (*BSReport, error)
  ```
  `IsBalanced = totalAssets == totalLiabilities + totalEquity`.
- [ ] Add to `ApplicationService` interface and implement.
- [ ] REPL command: `/bs [date]` (defaults to today).
- [ ] Integration test: after any set of valid postings, `IsBalanced` is always `true`.

**Acceptance criteria**: `/bs` prints Assets, Liabilities, Equity sections. `IsBalanced` is always true on a valid ledger.

---

## Tier 3 — Business Domain Expansion

### Phase 11: Vendor Master

**Goal**: Add a vendor master — the counterparty for all purchase-side transactions.

**Pre-requisites**: Phase 7 (RuleEngine wired into InventoryService — AP account resolved dynamically).

**Tasks:**

- [ ] Create `migrations/014_vendors.sql`:
  ```sql
  CREATE TABLE IF NOT EXISTS vendors (
      id SERIAL PRIMARY KEY,
      company_id INT NOT NULL REFERENCES companies(id),
      code VARCHAR(20) NOT NULL,
      name VARCHAR(200) NOT NULL,
      email VARCHAR(200), phone VARCHAR(40), address TEXT,
      payment_terms_days INT DEFAULT 30,
      ap_account_code VARCHAR(20) DEFAULT '2000',
      default_expense_account_code VARCHAR(20),
      is_active BOOL DEFAULT true,
      created_at TIMESTAMPTZ DEFAULT NOW(),
      UNIQUE(company_id, code)
  );
  ```
- [ ] Create `migrations/015_seed_vendors.sql` — insert 2–3 vendors for Company 1000 with `ON CONFLICT DO NOTHING`.
- [ ] Create `internal/core/vendor_model.go` — `Vendor` struct.
- [ ] Create `internal/core/vendor_service.go` — `VendorService` interface with `CreateVendor()` and `GetVendors()`.
- [ ] Wire into `ApplicationService`: add `ListVendors(ctx, companyCode)` and `CreateVendor(ctx, req)` methods.
- [ ] REPL command: `/vendors [company-code]` — prints vendor list.
- [ ] Integration test: create vendor, retrieve vendor list, company isolation.

**Acceptance criteria**: `/vendors` shows seeded vendors. Vendor is scoped to company.

---

### Phase 12: Purchase Order DRAFT + APPROVED

**Goal**: Create a purchase order and assign it a gapless PO number on approval.

**Pre-requisites**: Phase 11 (Vendor master exists).

**Tasks:**

- [ ] Create `migrations/016_purchase_orders.sql`:
  - `purchase_orders` table: `id, company_id, vendor_id, po_number NULL, status DEFAULT 'DRAFT', po_date, expected_date, currency, exchange_rate, total_transaction, total_base, notes, approved_at, created_at`.
  - `purchase_order_lines` table: `id, order_id, line_number, product_id NULL, description, quantity, unit_cost, line_total_transaction, line_total_base, expense_account_code NULL`.
  - `PO` document type entry: `per_fy` numbering strategy.
- [ ] Create `internal/core/purchase_order_model.go` — `PurchaseOrder`, `PurchaseOrderLine`, `PurchaseOrderLineInput` structs.
- [ ] Create `internal/core/purchase_order_service.go` — `PurchaseOrderService` interface.
- [ ] Implement `CreatePO(ctx, companyCode, vendorCode, poDate, lines, notes)` — creates DRAFT, computes line totals.
- [ ] Implement `ApprovePO(ctx, poID, docService)` — row-locks PO, assigns gapless PO number via `DocumentService`, sets `status = 'APPROVED'`, sets `approved_at`.
- [ ] Implement `GetPO(ctx, poID)` and `GetPOs(ctx, companyCode, status)` queries.
- [ ] Wire into `ApplicationService` and REPL: `/purchase-orders [status]`, `/new-po <vendor-code>` (interactive wizard), `/approve-po <po-ref>`.
- [ ] Integration test: CreatePO → ApprovePO → assert PO number assigned and status correct. Company isolation.

**Acceptance criteria**: `/new-po V001` creates a DRAFT PO. `/approve-po PO-2026-00001` assigns a number.

---

### Phase 13: Goods Receipt Against Purchase Order

**Goal**: Receive goods against a PO, update inventory, and book the accounting entry.

**Pre-requisites**: Phase 12 (PO exists in APPROVED state). Phase 7 (RuleEngine resolves inventory account).

**Tasks:**

- [ ] Add `po_line_id INT NULL` to `inventory_movements` table (`migrations/017_po_link.sql`).
- [ ] Implement `ReceivePO(ctx, poID, warehouseCode, receivedLines []ReceivedLine, ledger, docService, inv)` in `PurchaseOrderService`:
  - Validate PO is APPROVED.
  - For each physical-goods line: call `InventoryService.ReceiveStock()` (weighted average cost update) and set `po_line_id` on the movement record.
  - For each service/expense line: post `DR expense_account_code / CR AP` directly via `Ledger.Commit()`.
  - Status → `RECEIVED`, set `received_at`.
- [ ] Wire into `ApplicationService` and REPL: `/receive-po <po-ref>` (interactive line input or single-line shorthand).
- [ ] Integration test: ApprovePO → ReceivePO → verify `qty_on_hand` increased, `inventory_movements.po_line_id` set, `DR Inventory / CR AP` journal entry posted.

**Acceptance criteria**: Receiving a PO updates stock and creates the correct journal entry, linked to the PO.

---

### Phase 14: Vendor Invoice + AP Payment

**Goal**: Complete the procurement cycle — record vendor bill and make payment.

**Pre-requisites**: Phase 13 (PO received).

**Tasks:**

- [ ] Implement `RecordVendorInvoice(ctx, poID, invoiceNumber, invoiceDate, invoiceAmount, ledger, docService)` in `PurchaseOrderService`:
  - Creates and posts a `PI` document (gapless number).
  - Posts journal entry: `DR Inventory / CR AP` (for goods) or `DR Expense / CR AP` (for services).
  - Warns (log, not error) if `invoiceAmount` deviates > 5% from PO total.
  - Status → `INVOICED`, set `invoiced_at`.
- [ ] Implement `PayVendor(ctx, poID, bankCode, paymentDate, ledger)`:
  - Posts: `DR AP / CR Bank`.
  - Status → `PAID`.
- [ ] Wire into `ApplicationService` and REPL: `/vendor-invoice <po-ref>`, `/pay-vendor <po-ref> [bank-account]`.
- [ ] Integration test: full lifecycle CreatePO → ApprovePO → ReceivePO → RecordVendorInvoice → PayVendor. Verify AP balance zeroed after payment.

**Acceptance criteria**: Full procurement cycle works end-to-end. AP balance clears on payment.

---

### Phase 15: Service Categories + Job Order DRAFT/CONFIRMED

**Goal**: Introduce the Job/Work Order domain for service businesses.

**Pre-requisites**: Phase 7 (RuleEngine). Note: Job Orders are a separate model from Sales Orders — the `SHIPPED` state and COGS automation are incompatible with service semantics.

**Tasks:**

- [ ] Create `migrations/018_job_orders.sql`:
  - `service_categories`: `id, company_id, code, name, default_revenue_account_code, is_active`.
  - `job_orders`: `id, company_id, customer_id, job_number NULL, service_category_id NULL, status DEFAULT 'DRAFT', description, asset_ref NULL (free text: vehicle plate, serial), scheduled_date, currency, exchange_rate, total_transaction DEFAULT 0, notes, confirmed_at, created_at`.
  - `JO` document type: `per_fy` numbering.
- [ ] Seed 2 service categories for Company 1000 (`migrations/019_seed_service_categories.sql`).
- [ ] Create `internal/core/job_model.go` — `JobOrder`, `ServiceCategory` structs.
- [ ] Create `internal/core/job_service.go` — `JobService` interface.
- [ ] Implement `CreateJob(ctx, companyCode, customerCode, categoryCode, description, assetRef, scheduledDate, currency, notes)`.
- [ ] Implement `ConfirmJob(ctx, jobID, docService)` — assigns gapless JO number, status → `CONFIRMED`.
- [ ] Implement `GetJob`, `GetJobs` queries.
- [ ] Wire into `ApplicationService` and REPL: `/jobs [status]`, `/new-job <customer-code>`, `/confirm-job <job-ref>`.
- [ ] Integration test: CreateJob → ConfirmJob → assert JO number assigned.

**Acceptance criteria**: `/new-job C001` creates a DRAFT job. `/confirm-job` assigns a JO number.

---

### Phase 16: Job Progress — Start + Add Lines

**Goal**: Allow adding labour and material lines to an in-progress job.

**Pre-requisites**: Phase 15 (Job exists in CONFIRMED state).

**Tasks:**

- [ ] Add `migrations/020_job_order_lines.sql`:
  - `job_order_lines`: `id, job_id, line_number, line_type VARCHAR(20), description, product_id NULL, quantity, unit_price, line_total_transaction, revenue_account_code NULL`.
  - `line_type` values: `LABOUR`, `MATERIAL`, `SUBCONTRACT`, `FIXED`.
- [ ] Implement `StartJob(ctx, jobID)` — status → `IN_PROGRESS`, set `started_at`.
- [ ] Implement `AddJobLine(ctx, jobID, lineType, description, productCode *string, qty, unitPrice decimal)`:
  - Validates job is `IN_PROGRESS`.
  - Resolves `product_id` if `productCode` is set.
  - Inserts line, recalculates `job_orders.total_transaction`.
- [ ] Wire into `ApplicationService` and REPL:
  - `/start-job <job-ref>`
  - `/add-labour <job-ref> <hours> <rate> [description]`
  - `/add-material <job-ref> <product-code> <qty> [unit-price]`
- [ ] Integration test: StartJob → add labour line → add material line → verify `total_transaction` updated.

**Acceptance criteria**: Lines added to a job accumulate into `total_transaction`. Labour and material lines stored with correct `line_type`.

---

### Phase 17: Job Completion + Invoice + Payment

**Goal**: Complete a job, generate the customer invoice, and record payment.

**Pre-requisites**: Phase 16 (Job has lines and is IN_PROGRESS).

**Tasks:**

- [ ] Implement `CompleteJob(ctx, jobID)` — status → `COMPLETED`, set `completed_at`. Blocks invoicing if called before this.
- [ ] Implement `InvoiceJob(ctx, jobID, ledger, docService)`:
  - Validates status is `COMPLETED`.
  - Posts `SI` document (gapless).
  - Builds proposal: `DR AR (full total)` / `CR revenue_account_code` per line (aggregated by account).
  - Calls `Ledger.Commit()`.
  - Status → `INVOICED`, set `invoiced_at`.
- [ ] Implement `RecordJobPayment(ctx, jobID, bankCode, paymentDate, ledger)`:
  - Posts: `DR Bank / CR AR`.
  - Status → `PAID`.
- [ ] Wire into `ApplicationService` and REPL: `/complete-job <job-ref>`, `/invoice-job <job-ref>`, `/pay-job <job-ref>`.
- [ ] Integration test: full lifecycle Start → AddLines → Complete → Invoice → Pay. Verify AR clears and revenue account is credited.

**Acceptance criteria**: Full service job lifecycle works. Revenue posted correctly per job line's revenue account.

---

### Phase 18: Inventory Consumption for Job Material Lines

**Goal**: Deduct physical inventory when a material line is added to a job, and book the COGS entry.

**Pre-requisites**: Phase 17 (Job invoicing works). Phase 7 (Inventory/COGS accounts via RuleEngine).

**Tasks:**

- [ ] Add `ConsumeForJobTx(ctx, tx, companyID, jobID, line JobOrderLine, ledger *Ledger, docService DocumentService) error` to `InventoryService` interface and implementation. Same row-locking pattern as `ShipStockTx`. Movement type: `JOB_CONSUMPTION`. Service products (no `inventory_item`) silently skipped.
- [ ] Update `JobService.AddJobLine()`: when `line_type = MATERIAL` and `product_id` is set, call `ConsumeForJobTx()` inside the same transaction as the line insert. Books `DR Job Expense / CR Inventory`.
- [ ] If inventory is insufficient for a material line, return a descriptive error before inserting the line.
- [ ] REPL: no new commands — `/add-material` now triggers stock deduction automatically.
- [ ] Integration test: AddMaterialLine → verify `qty_on_hand` decreased, COGS journal entry posted atomically with line insert. Insufficient stock returns error without inserting the line.

**Acceptance criteria**: Adding a material line to a job atomically deducts stock and books the cost entry.

---

### Phase 19: Rental Asset Master + Contract DRAFT/ACTIVE

**Goal**: Introduce rental assets and the contract lifecycle up to asset handover.

**Pre-requisites**: Phase 7 (RuleEngine for AP/revenue accounts).

**Tasks:**

- [ ] Create `migrations/021_rental.sql`:
  - `rental_assets`: `id, company_id, code, name, asset_type, purchase_cost, current_book_value, daily_rate NULL, weekly_rate NULL, monthly_rate NULL, asset_account_code, accumulated_depreciation_account_code, status DEFAULT 'AVAILABLE', created_at`.
  - `rental_contracts`: `id, company_id, customer_id, asset_id, contract_number NULL, status DEFAULT 'DRAFT', start_date, end_date, rate_type, agreed_rate, security_deposit DEFAULT 0, deposit_account_code, currency, exchange_rate DEFAULT 1, notes, created_at, activated_at`.
  - `RC` document type: `per_fy` numbering.
  - Add `account_rules` row: `SECURITY_DEPOSIT_LIABILITY → 2300` and `RENTAL_REVENUE → 4200` (also add these accounts to CoA seed).
- [ ] Create `internal/core/rental_model.go` and `rental_service.go`.
- [ ] Implement `CreateRentalContract()` — validates asset is AVAILABLE, creates DRAFT.
- [ ] Implement `ActivateContract(ctx, contractID, docService, ledger)`:
  - Checks asset is still AVAILABLE (row-lock on asset).
  - Assigns gapless RC number.
  - If `security_deposit > 0`: posts `DR Bank / CR Security Deposit Liability`.
  - Marks asset `RENTED`.
  - Status → `ACTIVE`.
- [ ] Implement `GetRentalAssets`, `GetRentalContracts` queries.
- [ ] Wire into `ApplicationService` and REPL: `/rental-assets`, `/rental-contracts`, `/new-rental <customer-code> <asset-code>`, `/activate-rental <contract-ref>`.
- [ ] Integration test: CreateContract → ActivateContract → verify asset status = RENTED, deposit entry posted. Activating a RENTED asset returns error.

**Acceptance criteria**: Double-booking prevented. Activation posts deposit entry and marks asset RENTED.

---

### Phase 20: Rental Billing + Asset Return

**Goal**: Generate rental invoices for a period and handle asset return.

**Pre-requisites**: Phase 19 (Contract is ACTIVE).

**Tasks:**

- [ ] Implement `BillRentalPeriod(ctx, contractID, periodStart, periodEnd, ledger, docService)`:
  - Validates contract is ACTIVE.
  - Computes `amount = agreed_rate × days` (or weeks/months based on `rate_type`).
  - Posts `SI` document. Journal: `DR AR / CR Rental Revenue`.
  - Contract status remains ACTIVE (multiple billing periods per contract).
- [ ] Implement `ReturnAsset(ctx, contractID, returnDate, ledger, docService)`:
  - Sets `actual_return_date`.
  - If `returnDate > end_date`: auto-calls `BillRentalPeriod()` for the overrun period.
  - Marks asset `AVAILABLE`.
  - Status → `RETURNED`.
- [ ] Implement `RecordRentalPayment(ctx, contractID, bankCode, paymentDate, ledger)` — `DR Bank / CR AR`. Status → `PAID`.
- [ ] Wire into `ApplicationService` and REPL: `/bill-rental <contract-ref> <from> <to>`, `/return-rental <contract-ref> [return-date]`, `/pay-rental <contract-ref>`.
- [ ] Integration test: Activate → BillPeriod → ReturnAsset (late) → verify overrun auto-billed, asset AVAILABLE.

**Acceptance criteria**: Billing, return, and payment work. Late return auto-bills the overrun.

---

### Phase 21: Security Deposit Refund + Asset Depreciation

**Goal**: Handle deposit refunds (full or partial) and run monthly straight-line depreciation.

**Pre-requisites**: Phase 20 (Asset returned).

**Tasks:**

- [ ] Implement `RefundDeposit(ctx, contractID, deductionAmount decimal, ledger)`:
  - Full refund: `DR Security Deposit Liability / CR Bank` (full deposit).
  - Partial (damage): `DR Security Deposit Liability / CR Bank` (net) + `DR Security Deposit Liability / CR Other Income or Damage Revenue` (deducted portion).
- [ ] Implement `RunDepreciation(ctx, companyCode, periodDate string, ledger)`:
  - Fetches all active rental assets for the company.
  - Computes monthly straight-line depreciation: `purchase_cost / (useful_life_months)`. Use a `useful_life_months INT DEFAULT 60` column on `rental_assets`.
  - Posts one journal entry per asset: `DR Depreciation Expense / CR Accumulated Depreciation`.
  - Updates `rental_assets.current_book_value`.
  - Idempotency key: `depreciation-{asset_id}-{period_date}`.
- [ ] Wire into `ApplicationService` and REPL: `/refund-deposit <contract-ref> [deduction-amount]`, `/depreciate [YYYY-MM]`.
- [ ] Integration test: partial refund posts two journal lines correctly. Depreciation is idempotent (running twice for the same period posts only once).

**Acceptance criteria**: Deposit refund and depreciation batch run correctly. Depreciation is idempotent.

---

## Tier 4 — Tax Framework

> **Before starting any Tier 4 phase**, read [`docs/plan_gaps.md`](plan_gaps.md) — Sections 1 and 3. The tax phases are under-specified. Regulatory requirements (RCM, GSTR formats, TDS threshold rules, multi-currency GST valuation) must be documented as concrete test scenarios before coding begins.

### Phase 22: Tax Rate Schema + TaxEngine Service

**Goal**: Create the generic tax data model and a computation engine. No changes to invoicing yet.

**Pre-requisites**: Phase 7 (RuleEngine for tax account resolution).

**Tasks:**

- [ ] Create `migrations/022_tax_rates.sql`:
  - `tax_rates`: `id, company_id, code, name, jurisdiction NULL, is_active`.
  - `tax_rate_components`: `id, tax_rate_id, component_name, rate NUMERIC(6,4), tax_account_code, is_input_tax BOOL DEFAULT false`.
  - Add to `products`: `hsn_code VARCHAR(8) NULL`, `tax_category VARCHAR(20) NULL`, `default_tax_rate_id INT NULL`.
  - Add to `customers`: `gstin VARCHAR(15) NULL`, `tax_jurisdiction VARCHAR(10) NULL`, `is_sez BOOL DEFAULT false`, `is_composition_dealer BOOL DEFAULT false`.
- [ ] Add tax accounts to CoA seed migration (`migrations/023_seed_tax_accounts.sql`): `2100 CGST Payable`, `2110 SGST Payable`, `2120 IGST Payable`, `1301 ITC-CGST`, `1311 ITC-SGST`, `1321 ITC-IGST`.
- [ ] Create `internal/core/tax_engine.go`. Define:
  ```go
  type TaxComponent struct {
      ComponentName  string
      Rate           decimal.Decimal
      TaxableAmount  decimal.Decimal
      TaxAmount      decimal.Decimal
      TaxAccountCode string
      IsInputTax     bool
  }
  type TaxEngine interface {
      ComputeOutputTax(ctx, companyID int, taxRateID int, taxableAmount decimal.Decimal) ([]TaxComponent, error)
  }
  ```
  Implementation: fetch `tax_rate_components` for the rate, compute `TaxAmount = taxableAmount × rate`, return components. No hardcoded component names.
- [ ] If `taxRateID = 0` or product has no `default_tax_rate_id`, return empty slice (zero tax — valid for exempt items).
- [ ] Unit test: correct components returned, multiple components, zero tax on nil rate.

**Acceptance criteria**: `TaxEngine.ComputeOutputTax()` computes correctly. No changes to invoicing behaviour yet. All existing tests pass.

---

### Phase 23: Tax-Aware Invoice Posting (SalesOrder)

**Goal**: Update `InvoiceOrder()` to post separate journal lines for each tax component.

**Pre-requisites**: Phase 22 (TaxEngine exists). This phase will break existing invoice integration tests — update them as part of this phase.

**Tasks:**

- [ ] Add `sales_order_tax_lines` table (`migrations/024_sales_order_tax_lines.sql`): `id, order_line_id, tax_rate_component_id, taxable_amount, tax_amount`.
- [ ] Inject `TaxEngine` into `OrderService`.
- [ ] Refactor `InvoiceOrder()`:
  - Per line: look up `product.default_tax_rate_id`. Call `TaxEngine.ComputeOutputTax()`.
  - Compute gross total = net total + all tax amounts.
  - Build proposal: `DR AR (gross)` / `CR Revenue account (net per line)` / `CR tax_account_code (per component)`.
  - Insert rows into `sales_order_tax_lines`.
- [ ] Update all integration tests that assert exact proposal line counts or AR amounts — they now include tax lines.
- [ ] Add integration test: invoice a product with a tax rate → verify tax account credited, AR debited at gross.
- [ ] Verify: invoicing a product with no `default_tax_rate_id` works exactly as before (zero tax).

**Acceptance criteria**: Tax-bearing products create split journal entries. Tax-exempt products unchanged. All tests pass.

---

### Phase 24: Input Tax on Purchases

**Goal**: Book Input Tax Credit (ITC) when receiving a purchase order.

**Pre-requisites**: Phase 23 (TaxEngine and tax accounts exist). Phase 13 (ReceivePO exists).

**Tasks:**

- [ ] Inject `TaxEngine` into `PurchaseOrderService`.
- [ ] Add `ComputeInputTax(ctx, companyID, taxRateID int, taxableAmount decimal) ([]TaxComponent, error)` to `TaxEngine` — same logic as output tax but `is_input_tax = true` components use ITC accounts.
- [ ] Add `default_tax_rate_id INT NULL` to `purchase_order_lines` (allow per-line tax rate override).
- [ ] Update `RecordVendorInvoice()`: per line, call `TaxEngine.ComputeInputTax()`. Post: `DR Inventory (net)` / `DR ITC account (per component)` / `CR AP (gross)`.
- [ ] Integration test: receive PO with GST18 product → ITC accounts debited, AP credited at gross.

**Acceptance criteria**: ITC correctly booked on purchase. Net inventory cost excludes recoverable tax.

---

### Phase 25: GST Rate Seeds + Jurisdiction Resolver

**Goal**: Configure Indian GST slabs and automatically choose CGST+SGST or IGST based on supply type.

**Pre-requisites**: Phase 23 and Phase 24 (TaxEngine in use).

**Tasks:**

- [ ] Create `migrations/025_gst_rates.sql`:
  - `indian_state_codes` reference table: `code CHAR(2)`, `state_name`.
  - Add `state_code CHAR(2) NULL` to `companies`.
  - Seed `tax_rates` for Company 1000: `GST0`, `GST5`, `GST12`, `GST18`, `GST28` — each with an intrastate variant (CGST+SGST split) and an interstate variant (IGST full rate).
  - Seed `tax_rate_components` with correct rates and account codes for each variant.
- [ ] Create `internal/core/gst_resolver.go`:
  ```go
  func ResolveGSTRateID(ctx, db, companyID int, customer Customer, gstSlabCode string) (taxRateID int, error)
  ```
  - Fetch `company.state_code`. If blank, return error.
  - Compare with `customer.tax_jurisdiction`. Same state → intrastate rate ID. Different → interstate rate ID.
  - SEZ (`customer.is_sez = true`) → GST0 rate ID.
- [ ] Update `InvoiceOrder()`: when company has a `state_code` set and customer has a `tax_jurisdiction`, call `ResolveGSTRateID()` to override product's `default_tax_rate_id` with the jurisdiction-correct one.
- [ ] Integration test: same-state invoice uses CGST+SGST. Different-state invoice uses IGST. SEZ uses zero rate.

**Acceptance criteria**: CGST+SGST vs IGST automatically resolved from customer and company state codes.

---

### Phase 26: GST Special Cases

**Goal**: Handle RCM (Reverse Charge Mechanism), composition dealers, and HSN validation.

**Pre-requisites**: Phase 25 (Basic GST working).

**Tasks:**

- [ ] Add `rcm_applicable BOOL DEFAULT false` to `vendors`.
- [ ] Update `RecordVendorInvoice()`: if `vendor.rcm_applicable = true`, post self-assessment entry: `DR RCM Input Tax / CR RCM Output Tax` (add these accounts to CoA seed). Net effect is zero but required for GSTR-3B.
- [ ] Update `InvoiceOrder()`: if `customer.is_composition_dealer = true`, skip `TaxEngine` call entirely — no output tax posted.
- [ ] Add HSN validation warning in `InvoiceOrder()`: if `product.hsn_code` is blank, log a warning to stdout (do not block the invoice).
- [ ] Integration test: RCM vendor invoice posts self-assessment lines. Composition customer invoice has no tax lines.

**Acceptance criteria**: RCM, composition dealer, and HSN warning all work correctly.

---

### Phase 27: TDS Schema + Deduction on Vendor Payments

**Goal**: Deduct TDS at source when paying vendors above the threshold.

**Pre-requisites**: Phase 14 (PayVendor exists). Phase 7 (TDS Payable account via RuleEngine).

**Tasks:**

- [ ] Create `migrations/026_tds.sql`:
  - `tds_sections`: `code VARCHAR(10)`, `description`, `rate NUMERIC(6,4)`, `threshold_limit NUMERIC(14,2)`.
  - Add to `vendors`: `tds_applicable BOOL DEFAULT false`, `default_tds_section_id INT NULL`.
  - `tds_vendor_ledger`: `company_id, vendor_id, section_id, financial_year INT, cumulative_paid NUMERIC(14,2) DEFAULT 0`. Unique on `(company_id, vendor_id, section_id, financial_year)`.
  - Add `TDS_PAYABLE` rule (`2200`) and `TCS_PAYABLE` rule (`2210`) to `account_rules` seed.
- [ ] Seed common TDS sections: 194C (Contractors 1%), 194J (Professional Services 10%).
- [ ] Update `PayVendor()` in `PurchaseOrderService`:
  - If `vendor.tds_applicable` and `default_tds_section_id` set:
    - Lock + read `tds_vendor_ledger` row for (vendor, section, current FY). Create row if not exists.
    - If `cumulative_paid < threshold_limit`: no TDS. Update cumulative only.
    - If threshold crossed: `tds_amount = payment_amount × section.rate`.
    - Post: `DR AP (full) / CR Bank (net) / CR TDS Payable (deducted amount)`.
    - Update `tds_vendor_ledger.cumulative_paid += payment_amount`.
- [ ] Integration test: first payment below threshold — no TDS. Second payment crosses threshold — TDS deducted. Verify split entry amounts.

**Acceptance criteria**: TDS deducted only after threshold crossed. Correct split into Bank + TDS Payable.

---

### Phase 28: TCS on Customer Receipts + TDS Settlement

**Goal**: Mirror TDS for customer collections (TCS) and enable TDS/TCS settlement payments to the government.

**Pre-requisites**: Phase 27 (TDS infrastructure in place).

**Tasks:**

- [ ] Add to `customers`: `tcs_applicable BOOL DEFAULT false`, `default_tcs_section_id INT NULL`.
- [ ] Add `tcs_customer_ledger` table (same schema as `tds_vendor_ledger` but for customers).
- [ ] Update `RecordPayment()` in `OrderService`: if `customer.tcs_applicable`: compute TCS, post `DR Bank (gross) / CR AR (net) / CR TCS Payable (collected)`.
- [ ] Implement `SettleTDS(ctx, companyCode, sectionCode, period, bankCode, ledger)` in a new `ComplianceService`:
  - Posts: `DR TDS Payable / CR Bank` for the net TDS balance for that section/period.
- [ ] Implement `SettleTCS(ctx, companyCode, sectionCode, period, bankCode, ledger)` — mirror of above.
- [ ] Wire into `ApplicationService` and REPL: `/pay-tds <section-code> <YYYY-MM>`, `/pay-tcs <section-code> <YYYY-MM>`.
- [ ] Integration test: TCS collected on receipt. Settlement clears TCS Payable balance.

**Acceptance criteria**: TCS collected on customer payments. Settlement commands clear the tax payable balance.

---

### Phase 29: Period Locking

**Goal**: Prevent any journal entry from being posted to a closed accounting period.

**Pre-requisites**: Phase 9 (Reporting understands periods). Can be implemented standalone.

**Tasks:**

- [ ] Create `migrations/027_accounting_periods.sql`:
  ```sql
  CREATE TABLE IF NOT EXISTS accounting_periods (
      id SERIAL PRIMARY KEY,
      company_id INT NOT NULL REFERENCES companies(id),
      year INT NOT NULL, month INT NOT NULL,
      status VARCHAR(10) DEFAULT 'OPEN',  -- OPEN | LOCKED
      locked_at TIMESTAMPTZ, locked_by VARCHAR(100),
      UNIQUE(company_id, year, month)
  );
  ```
- [ ] Update `Ledger.executeCore()`: before inserting the journal entry, check if a `LOCKED` row exists for the `posting_date`'s year/month. If so, return: `"posting to locked period YYYY-MM is not allowed"`.
- [ ] Implement `LockPeriod(ctx, companyCode, year, month int)` and `UnlockPeriod(ctx, companyCode, year, month int)` in `ReportingService`.
- [ ] Wire into `ApplicationService` and REPL: `/lock-period <YYYY-MM>`, `/unlock-period <YYYY-MM>`.
- [ ] Integration test: post entry → lock period → attempt another post to same period → expect error. Unlock → post succeeds.

**Acceptance criteria**: Posting to a locked period fails with a clear error. Unlocking re-enables posting.

---

### Phase 30: GSTR-1 + GSTR-3B Export

**Goal**: Export GST return data as JSON/CSV.

**Pre-requisites**: Phase 25 (GST tax lines populated in `sales_order_tax_lines`). Phase 29 (Period locking — return data must be from a locked period ideally).

**Tasks:**

- [ ] Add `ExportGSTR1(ctx, companyCode string, year, month int) (*GSTR1Report, error)` to `ReportingService`:
  - Query `sales_orders` + `sales_order_tax_lines` + `customers` for the period.
  - Structure into B2B (registered customers), B2C (unregistered), CDNR (credit notes), HSN Summary sections.
  - Return as a struct serialisable to JSON and CSV.
- [ ] Add `ExportGSTR3B(ctx, companyCode string, year, month int) (*GSTR3BReport, error)`:
  - Aggregate: output tax liability per component, ITC per component, net payable.
  - Source from `sales_order_tax_lines` and ITC entries in `journal_lines`.
- [ ] Wire into `ApplicationService` and REPL: `/gstr1 <YYYY-MM>`, `/gstr3b <YYYY-MM>`.
- [ ] Integration test: known dataset → assert GSTR1 output matches expected B2B and HSN summary values.

**Acceptance criteria**: `/gstr1 2026-02` outputs a correctly structured report. HSN summary totals match invoice line totals.

---

## Tier 5 — Scale & Governance

### Phase 31: AI Agent Architecture Upgrade

**Pre-requisites**: Phase 10 (Reporting). Phase 4 (Application Service).

> **Before implementing this phase**, read [`docs/ai_agent_upgrade.md`](ai_agent_upgrade.md) in full. The three bullet points below are the original plan. The upgrade document redefines Phase 31 as a foundational tool-calling architecture refactor, and proposes that individual AI capabilities be added alongside the domains they support (Phases 8, 12, 15, 23, 25, 27) rather than all at the end.

- [ ] **Tool-calling architecture**: agent selects from a registered set of `ApplicationService` tools, proposes the action with parameters, user confirms, system executes. Replaces the current single-output `Proposal` model for domain operations.
- [ ] **Receipt/Invoice image ingestion**: accept image file path or base64 → OpenAI Vision API → extract vendor, amount, date → propose entry → user confirms before commit.
- [ ] **Conversational reporting**: natural language query → AI calls `ReportingService` → returns plain-English answer. Example: *"What was my service revenue last quarter?"*
- [ ] **Anomaly flagging**: AI flags proposals with confidence < 0.5 for manual review with a warning message.
- [ ] **Proactive compliance guidance**: GST jurisdiction warnings, TDS threshold alerts, HSN code missing warnings — presented before the user confirms an action.
- [ ] **Plain-English explanations**: after any journal entry is committed, AI explains what happened in non-accounting language.

---

### ~~Phase 32: REST API / Web Interface Layer~~ — Superseded

> This phase has been removed from Tier 5 and replaced by **Tier 2.5 (Phases WF1–WF4)**. Web infrastructure is built immediately after Phase 7, before any Tier 3 domain expansion. See [`docs/web_ui_plan.md`](web_ui_plan.md) for the full plan: tech stack, authentication, frontend scaffold, domain UI phases, and REPL deprecation timeline.

---

### Phase 33: Workflow + Approvals

**Pre-requisites**: Phase WF2 (users table already exists; JWT auth already in place).

- [ ] `users` table: `company_id`, `username`, `role` (`ACCOUNTANT` | `FINANCE_MANAGER` | `ADMIN`), `password_hash`.
- [ ] Role-based checks: only `FINANCE_MANAGER` can approve a PO, lock a period, or cancel an invoiced order.
- [ ] `audit_log` table: `user_id`, `action`, `entity_type`, `entity_id`, `timestamp`, `notes`.
- [ ] Correction workflows: structured `RefundOrder()` via compensating journal entries with full audit trail.

---

### Phase 34: External Integrations

**Pre-requisites**: Phase WF1 (HTTP server). Phase 33 (auth for inbound webhooks).

- [ ] Inbound webhook receiver for Stripe / Razorpay — auto-propose payment settlement entries.
- [ ] Outbound webhook on order status change or journal entry committed.
- [ ] External `OrderCreated` ingestion endpoint for e-commerce platforms.

---

### Phase 35: Multi-Branch Support

**Pre-requisites**: Phase 4 (Application Service). Note: `documents.branch_id` column already exists — this activates it.

- [ ] `branches` table: `company_id, code, name, address`.
- [ ] Add nullable `branch_id` to `sales_orders`, `purchase_orders`, `job_orders`, `rental_contracts`.
- [ ] `ReportingService.GetProfitAndLoss()` accepts optional `branchCode` filter.
- [ ] `/company <code>` and `/branch <code>` REPL context commands.
- [ ] Inter-branch inventory transfer: `TRANSFER_OUT` / `TRANSFER_IN` movement types.

---

## Summary Table

| Phase | Title | Tier | Risk | Depends On | Status |
|---|---|---|---|---|---|
| 0 | Immediate bug fixes | 0 | 🟢 | — | ✅ Done |
| 1 | Result types + AppService contract | 1 | 🟢 | 0 | ✅ Done |
| 2 | AppService implementation | 1 | 🟢 | 1 | ✅ Done |
| 3 | REPL adapter extraction | 1 | 🟢 | 2 | ✅ Done (1 open item) |
| 4 | CLI adapter + slim main | 1 | 🟢 | 3 | ✅ Done |
| 5 | Rule engine schema + seed rules | 2 | 🟢 | 4 | ✅ Done |
| 6 | RuleEngine service + OrderService | 2 | 🟠 | 5 | ✅ Done |
| 7 | RuleEngine into InventoryService | 2 | 🟠 | 6 | 🔲 Pending |
| 8 | Account statement report | 2 | 🟢 | 4 | 🔲 Pending |
| 9 | Materialized views + P&L | 2 | 🟢 | 8 | 🔲 Pending |
| 10 | Balance Sheet report | 2 | 🟢 | 9 | 🔲 Pending |
| **WF1** | **REST API foundation** | **2.5** | 🟢 | **7** | 🔲 Pending |
| **WF2** | **Authentication (JWT + users table)** | **2.5** | 🟢 | **WF1** | 🔲 Pending |
| **WF3** | **Frontend scaffold (React shell + login)** | **2.5** | 🟢 | **WF2** | 🔲 Pending |
| **WF4** | **Core accounting screens (dashboard, reports)** | **2.5** | 🟢 | **WF3, 8–10** | 🔲 Pending |
| **WF5** | **AI chat panel (SSE, action cards, image upload)** | **2.5** | 🟠 | **WF3** | 🔲 Pending |
| 11 | Vendor master | 3 | 🟢 | 7 | 🔲 Pending |
| **WD0** | **Web UI: customers, products, sales orders** | **3** | 🟢 | **WF4, 11** | 🔲 Pending |
| 12 | Purchase order DRAFT + APPROVED | 3 | 🟠 | 11 | 🔲 Pending |
| 13 | Goods receipt against PO | 3 | 🟠 | 12 | 🔲 Pending |
| 14 | Vendor invoice + AP payment | 3 | 🟠 | 13 | 🔲 Pending |
| **WD1** | **Web UI: vendors, purchase order lifecycle** | **3** | 🟢 | **WF4, 14** | 🔲 Pending |
| 15 | Service categories + job order DRAFT/CONFIRMED | 3 | 🟠 | 7 | 🔲 Pending |
| 16 | Job progress: start + add lines | 3 | 🟢 | 15 | 🔲 Pending |
| 17 | Job completion + invoice + payment | 3 | 🟠 | 16 | 🔲 Pending |
| 18 | Inventory consumption for jobs | 3 | 🟠 | 17, 7 | 🔲 Pending |
| **WD2** | **Web UI: job orders lifecycle** | **3** | 🟢 | **WF4, 18** | 🔲 Pending |
| 19 | Rental asset master + contract DRAFT/ACTIVE | 3 | 🟠 | 7 | 🔲 Pending |
| 20 | Rental billing + asset return | 3 | 🟠 | 19 | 🔲 Pending |
| 21 | Security deposit + depreciation | 3 | 🟢 | 20 | 🔲 Pending |
| **WD3** | **Web UI: rentals + REPL deletion** | **3** | 🔴 | **WF4, 21** | 🔲 Pending |
| 22 | Tax rate schema + TaxEngine service | 4 | 🟠 | 7 | 🔲 Pending |
| 23 | Tax-aware invoice posting | 4 | 🔴 | 22 | 🔲 Pending |
| 24 | Input tax on purchases | 4 | 🟠 | 23, 13 | 🔲 Pending |
| 25 | GST rate seeds + jurisdiction resolver | 4 | 🟠 | 23 | 🔲 Pending |
| 26 | GST special cases (RCM, SEZ, composition) | 4 | 🟠 | 25 | 🔲 Pending |
| 27 | TDS schema + vendor deductions | 4 | 🟠 | 14, 7 | 🔲 Pending |
| 28 | TCS on receipts + TDS settlement | 4 | 🟢 | 27 | 🔲 Pending |
| 29 | Period locking | 4 | 🟢 | 9 | 🔲 Pending |
| 30 | GSTR-1 + GSTR-3B export | 4 | 🟢 | 25, 29 | 🔲 Pending |
| 31 | AI tool-calling architecture + full skills | 5 | 🔴 | 10, WF5 | 🔲 Pending |
| ~~32~~ | ~~REST API / Web layer~~ — *superseded by Tier 2.5* | — | — | — | 🚫 Removed |
| 33 | Workflow + approvals (role enforcement) | 5 | 🔴 | WF2 | 🔲 Pending |
| 34 | External integrations | 5 | 🔴 | WF1, 33 | 🔲 Pending |
| 35 | Multi-branch support | 5 | 🔴 | WF1 | 🔲 Pending |

**Risk:** 🟢 Additive / no existing changes &nbsp;|&nbsp; 🟠 Extends existing paths &nbsp;|&nbsp; 🔴 Modifies existing interfaces or test suites

**Status:** ✅ Done &nbsp;|&nbsp; 🔲 Not started / Pending &nbsp;|&nbsp; 🚫 Removed / superseded
