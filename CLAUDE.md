# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all migrations against the live database
go run ./cmd/verify-db

# Build the application
go build -o app.exe ./cmd/app

# Run all tests (integration tests require TEST_DATABASE_URL in .env)
go test ./internal/core -v

# Run a single test
go test ./internal/core -v -run TestLedger_Idempotency

# Run unit tests only (no DB required)
go test ./internal/core -v -run TestProposal

# Verify AI agent integration
go run ./cmd/verify-agent

# Run the interactive REPL
./app.exe

# CLI one-shot commands
./app.exe propose "event description"
./app.exe balances
Get-Content proposal.json | ./app.exe validate
Get-Content proposal.json | ./app.exe commit
```

## Architecture

### Layering (strictly enforced — no exceptions)

```
Layer 4 — Interface Adapters
          internal/adapters/repl/   REPL commands, display, interactive wizards
          internal/adapters/cli/    CLI one-shot commands (propose/validate/commit/bal)
          cmd/app/                  Wiring only — 48 lines, no business logic
                    ↓
Layer 3 — Application Service
          internal/app/             ApplicationService interface + implementation
                    ↓               No fmt.Println. No display logic.
Layer 2 — Domain Core
          internal/core/            Ledger, OrderService, InventoryService,
                                    DocumentService, RuleEngine
                    ↓
Layer 1 — Infrastructure
          internal/db/              pgx connection pool
          internal/ai/              OpenAI GPT-4o agent (advisory only, never writes DB)
```

**Forbidden imports:**
- Adapters must not import `internal/core` directly — they call `app.ApplicationService` only.
- Domain services must not import adapters or `internal/ai`.
- `internal/ai` must not import `internal/core` domain services (only uses core model types).
- No layer imports upward.

**Permitted cross-domain calls:**
- `OrderService` may call `LedgerService` and `DocumentService`.
- `InventoryService` may call `Ledger.CommitInTx` (concrete `*Ledger`, not the interface) and `DocumentService`.
- `ApplicationService` calls all domain services and `internal/ai`.

### Key Design Decisions

**No ORM.** All database access uses hand-written SQL with `pgx/v5`. The PostgreSQL schema is the source of truth. Never use struct tags or reflection to generate SQL.

**Immutable ledger.** `journal_entries` and `journal_lines` are append-only. Business corrections use compensating entries, never UPDATEs. Only `internal/core/ledger.go` may write to these tables.

**AI is advisory only.** `internal/ai/agent.go` calls GPT-4o and returns a `core.Proposal`. The proposal must pass `Proposal.Validate()` before `Ledger.Commit()` is called. The AI never writes to the database.

**One transaction currency per journal entry (SAP model).** A single `TransactionCurrency` and `ExchangeRate` apply to all lines of an entry. Mixed-currency entries within one posting are forbidden. Line amounts are stored in transaction currency; `debit_base`/`credit_base` store the computed base-currency equivalent.

**Company scoping everywhere.** Every query touching business data must filter by `company_id`. There are no global reads of business data.

**Monetary precision.** Use `github.com/shopspring/decimal` for all monetary values. Database columns are `NUMERIC(14,2)` or `NUMERIC(15,6)`. Never use `float64` for money.

### REPL Input Classification

The routing rule is simple and has no exceptions:

```
Input starts with /  →  Deterministic command dispatcher (instant, no AI)
Input has no /       →  AI agent (GPT-4o), regardless of length or content
```

`bal` without a `/` goes to the AI — it is **not** a shortcut for `/bal`. Users must always include the `/` prefix for commands.

**Slash commands (deterministic, no AI):**
- **Ledger**: `/bal`, `/balances`
- **Master data**: `/customers`, `/products`
- **Orders**: `/orders`, `/new-order`, `/confirm`, `/ship`, `/invoice`, `/payment`
- **Inventory**: `/warehouses`, `/stock`, `/receive`
- **Session**: `/help`, `/exit`, `/quit`

**AI clarification loop behaviour:**
- When the AI requests clarification, the REPL reads one more line from the user.
- If that line starts with `/`, the AI session is **cancelled immediately** and the slash command is dispatched normally. The user is never stuck in the AI loop.
- An empty line or the word `cancel` also cancels the session.
- After 3 clarification rounds with no resolution, the loop exits with a message directing the user to `/help`.

**AI prompt behaviour for non-accounting input:**
The AI prompt instructs GPT-4o: if the input is a non-financial/operational request (e.g. "list orders", "confirm shipment"), respond with `is_clarification_request: true` and redirect to the relevant slash command. This is the AI's mechanism for gracefully handling misrouted input — it does not always fire perfectly for ambiguous single-word inputs.

### OpenAI Integration

- Model: GPT-4o via Responses API (`openai-go` SDK)
- Strict JSON schema mode: all schema properties must appear in `required`. No `omitempty` on structs used for schema generation.
- The `$schema` key must be stripped before submission (OpenAI strict mode rejects it).
- Schema is dynamically generated from Go structs via `invopop/jsonschema`.
- Nullable fields use `anyOf: [{schema}, {type: "null"}]` manually — not Go pointers with omitempty.

### Document Flow

Business event → `DRAFT` Document → `POSTED` Document (gapless number assigned) → Journal Entry committed atomically.

Document types: `JE` (journal entry), `SI` (sales invoice, affects AR), `PI` (purchase invoice, affects AP), `SO` (sales order, gapless order numbering), `GR` (goods receipt, DR Inventory / CR AP), `GI` (goods issue / COGS, DR COGS / CR Inventory).

Gapless document numbers use PostgreSQL row-level locks on `document_sequences` (`FOR UPDATE`). Never compute the next sequence number in application memory.

### Inventory Design Rules

- `InventoryService` exposes two method categories: **standalone** (manage their own TX) and **TX-scoped** (accept a `pgx.Tx` from the caller).
- `ShipStockTx`, `ReserveStockTx`, `ReleaseReservationTx` are TX-scoped — called inside `OrderService` transactions to ensure atomicity.
- COGS booking uses `Ledger.CommitInTx(ctx, tx, proposal)` — committed inside the same TX as inventory deduction and order state update.
- Products without an `inventory_item` record are service products — silently skipped in all inventory operations (no stock check, no COGS).
- Inventory running totals (`qty_on_hand`, `qty_reserved`) are maintained under `SELECT ... FOR UPDATE` row-level locks. Never update inventory outside a locked row.

### Migrations

- Files live in `migrations/` and are named `NNN_description.sql` (lexicographic order).
- All migrations must be idempotent: use `IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, and `DO $$ ... EXCEPTION ... END $$` guards.
- Never edit a previously applied migration — always add a new numbered file.
- The migration runner tracks applied migrations via the `schema_migrations` table and acquires a PostgreSQL advisory lock before running.

### Testing

- Integration tests in `internal/core/*_integration_test.go` require `TEST_DATABASE_URL` and truncate that database. Never point `TEST_DATABASE_URL` at the live database.
- Tests auto-skip if `TEST_DATABASE_URL` is not set.
- After adding new migrations, apply them to the test DB before running integration tests:
  `DATABASE_URL=$TEST_DATABASE_URL go run ./cmd/verify-db`
- Ledger and proposal unit tests must not require OpenAI.
- Required test coverage: ledger commit success/rejection, cross-company isolation, concurrency for document numbering, balance calculation regression, inventory stock levels and COGS.
- Current test count: **70 tests** (including subtests) across ledger, document, order, inventory, rule engine, reporting, vendor, and purchase order suites.

## Code Quality Rules

**No global state.** No package-level mutable variables. All dependencies must be injected via constructors or function parameters.

**Services are HTTP-agnostic.** No HTTP types in service method signatures. Accept `context.Context` as the first parameter. Services must be testable without an HTTP server.

**AI must be replaceable.** Define AI behind a Go interface (`AgentService`). The system must compile and run correctly without the AI module.

**No circular dependencies.** No god structs that own unrelated concerns. No shared mutable state between packages.

**Refactoring discipline.** When modifying existing behavior: don't change behavior silently, add tests before changing logic, preserve backward compatibility unless explicitly breaking.

## Pending Roadmap

### AI Agent Upgrade Principle

**Core system first. AI upgrades gradual and need-based.**

The accounting, inventory, and order management core is always the first priority. The AI agent is upgraded in parallel with the core build, but strictly incrementally and only when a new domain is stable and the AI addition is clearly needed. Never add AI capabilities at the expense of core correctness or system stability.

Concretely:
- **Do not start AI work for a domain until that domain's integration tests pass.**
- **Add only the tools and skills the current domain requires** — do not pre-build tools for domains not yet implemented.
- **The existing `InterpretEvent` path must remain untouched** until `InterpretDomainAction` has been stable in production across at least two domain phases.
- **Phase 7.5 introduces the AI tool architecture** (ToolRegistry, agentic loop, `InterpretDomainAction`, first read tools) immediately after Phase 7. AI tooling is added incrementally with each domain phase thereafter — there is no separate deferred AI phase.
- **Phase AI-RAG** (regulatory knowledge layer) begins after Phase 14, once 4+ domain phases have proven tool-call stability. **Phase AI-Skills** (skills framework + verification) begins after Phase 17, once Phase AI-RAG is stable.
- **If there is any tension between core correctness and an AI feature**, core correctness wins without exception.

**Every AI agent upgrade requires careful evaluation before implementation:**
- Invoke the `openai-integration` skill (`/openai-integration`) before writing or modifying any code that touches the OpenAI Go SDK (`openai-go`). This skill contains strict, project-specific rules for Responses API usage, structured output schema construction, tool call patterns, and error handling that must be followed exactly.
- All SDK usage must conform to the patterns in the `openai-integration` skill — no deviations without an explicit documented reason.
- Breaking changes to `AgentService`, `InterpretEvent`, or schema generation require a written justification in the commit message.

### Planning Documents

| Document | Role | Read when |
|---|---|---|
| [`docs/One_final_implementation_plan.md`](docs/One_final_implementation_plan.md) | **MVP roadmap — all phases complete.** WF2–WF5, Phases 11–14, WD0–WD1 all done. | For historical reference or re-implementation |
| [`docs/Tax_Regulatory_Future_Plan.md`](docs/Tax_Regulatory_Future_Plan.md) | **Deferred.** GST, TDS, TCS, period locking, GSTR export (Phases 22–30). Do not start until MVP is stable in production. | Before implementing any tax compliance work |

Archived (superseded): `docs/archive/Implementation_plan_upgrade.md`, `docs/archive/web_ui_plan.md`, `docs/archive/ai_agent_upgrade.md`, `docs/archive/plan_gaps.md`

**Completed:**
- **Tier 0**: Bug fixes — hardcoded `INR` currency in GR/COGS proposals, non-deterministic company load, AI loop depth limit.
- **Phase 1**: `internal/app/` — `ApplicationService` interface, result types, request types.
- **Phase 2**: `ApplicationService` implementation (`app_service.go`).
- **Phase 3**: REPL adapter extraction — `internal/adapters/repl/` (repl, display, wizards).
- **Phase 4**: CLI adapter — `internal/adapters/cli/cli.go` + `main.go` slimmed to 48 lines. `LoadDefaultCompany` and `ValidateProposal` added to `ApplicationService`.
- **Phase 5**: `account_rules` table + seed (migrations 011–012). 6 rules seeded for Company 1000.
- **Phase 6**: `RuleEngine` service (`internal/core/rule_engine.go`) wired into `OrderService`. `arAccountCode` constant removed. 5 new `TestRuleEngine_ResolveAccount` subtests added.
- **Phase 7**: `RuleEngine` wired into `InventoryService`. `inventoryAccountCode`, `cogsAccountCode`, and `defaultReceiptCreditAccountCode` constants removed. `NewInventoryService` now takes `ruleEngine` parameter. `setupInventoryTestDB` seeds INVENTORY/COGS/RECEIPT_CREDIT rules. All 32 tests pass. REPL deprecation clock starts — no new REPL slash commands from this point.
- **Phase 7.5**: AI Tool Architecture — `internal/ai/tools.go` (`ToolRegistry`, `ToolDefinition`, `ToolHandler`). `InterpretDomainAction` added to `AgentService` + `ApplicationService` alongside existing `InterpretEvent` (untouched). 5 Phase 7.5 read tools registered: `search_accounts`, `search_customers`, `search_products`, `get_stock_levels`, `get_warehouses`. Agentic tool loop: max 5 iterations, `PreviousResponseID` for multi-turn, read tools execute autonomously, `request_clarification` and `route_to_journal_entry` meta-tools terminate the loop. REPL AI path updated to route through `InterpretDomainAction`; journal entry events route back to `InterpretEvent`. Migration 013: `pg_trgm` extension + GIN indexes on `accounts.name`, `customers.name`, `products.name`. All 32 tests pass.
- **Phase 8**: Account statement report — `internal/core/reporting_service.go` (`ReportingService`, `StatementLine`, `GetAccountStatement`). `AccountStatementResult` added to `app/result_types.go`. `GetAccountStatement` added to `ApplicationService`. `NewAppService` updated in both `cmd/app` and `cmd/server`. REPL command `/statement <account-code> [from-date] [to-date]` added. Read tools `get_account_balance` and `get_account_statement` registered in `buildToolRegistry`. Integration test `TestReporting_GetAccountStatement` (3 sub-tests: full, date-range, empty). 35 tests pass.
- **Phase WF1**: Server + chat UI shell — `cmd/server/main.go`, `internal/adapters/web/` (handlers, middleware, errors, chat). `POST /api/chat/message` SSE endpoint (calls `InterpretDomainAction`, routes journal entries to `InterpretEvent`, emits typed SSE events). `POST /api/chat/confirm` (token-based pending-action store with 10-min TTL, commits journal entries via `CommitProposal`). `web/web.go` + `web/static/index.html` embedded static chat frontend (vanilla JS, no external deps, Fetch API streaming, action cards with confirm/cancel). All packages build clean.
- **Phase 9+10**: Materialized views + P&L + Balance Sheet reports. Migrations `014_reporting_views.sql` (`mv_account_period_balances`) and `015_trial_balance_view.sql` (`mv_trial_balance`). `ReportingService` extended with `GetProfitAndLoss`, `GetBalanceSheet`, `RefreshViews` (all via direct journal_lines queries, not MV-dependent). `ApplicationService` interface + `appService` impl updated with all 3 methods. 3 new AI read tools: `get_pl_report`, `get_balance_sheet`, `refresh_views`. REPL commands: `/pl [year] [month]`, `/bs [as-of-date]`, `/refresh`. Display functions `printPL`, `printBS` added. Integration tests `TestReporting_GetProfitAndLoss` (2 sub-tests) + `TestReporting_GetBalanceSheet` (2 sub-tests). 39 tests total.
- **Phase WF2**: Authentication — migrations 016 (users), 017 (admin seed: admin/Admin@1234), 018 (audit columns). `UserService` + `user_model.go`. `AuthenticateUser`/`GetUser` on `ApplicationService`. `internal/adapters/web/auth.go` (JWT HS256 httpOnly cookie, `RequireAuth` middleware, login/logout/me handlers). All API routes protected except health + auth endpoints.
- **Phase WF3**: Frontend scaffold — `github.com/a-h/templ v0.3.977`. HTMX 2.x, Alpine.js 3.x, Chart.js 4.x vendored. Tailwind CSS v4.2.1 (`tailwindcss.exe`). Layouts: `login_layout`, `app_layout`, `chat_layout`, `modal_shell`. Pages: `login`, `dashboard`. `RequireAuthBrowser` middleware. `pages.go` (loginPage, loginFormSubmit, logoutPage, dashboardPage, buildAppLayoutData). `Makefile` (generate, css, dev, build, test).
- **Phase WF4**: Core accounting screens — `internal/adapters/web/accounting.go`. Five page templates: `trial_balance.templ`, `pl_report.templ`, `balance_sheet.templ`, `account_statement.templ`, `journal_entry.templ`. API: GET trial-balance, GET statement (with CSV export), GET pl, GET balance-sheet, POST refresh, POST journal-entries, POST journal-entries/validate. Browser pages: `/reports/trial-balance`, `/reports/pl`, `/reports/balance-sheet`, `/reports/statement`, `/accounting/journal-entry`. Dashboard Journal Entry shortcut added. REPL reporting commands superseded.
- **Phase 11**: Vendor Master — migrations 019 (vendors table), 020 (3 seed vendors: V001 Acme Supplies, V002 Global Tech Components, V003 Swift Logistics), 021 (GIN trigram index on vendors.name). `internal/core/vendor_model.go` + `vendor_service.go` (CreateVendor, GetVendors, GetVendorByCode). `ListVendors`/`CreateVendor` on `ApplicationService`. `VendorsResult`, `VendorResult`, `CreateVendorRequest` types. 4 AI tools: `get_vendors`, `search_vendors` (pg_trgm similarity), `get_vendor_info` (read), `create_vendor` (write). 45 tests (6 new vendor subtests).
- **Phase 12**: Purchase Orders — migration 022 (purchase_orders + purchase_order_lines + PO document type `per_fy`). `internal/core/purchase_order_model.go` + `purchase_order_service.go` (CreatePO, ApprovePO with gapless `PO-YYYY-NNNNN` numbering via DocService, GetPO, GetPOs). `ListPurchaseOrders`/`CreatePurchaseOrder`/`ApprovePurchaseOrder` on `ApplicationService`. `PurchaseOrdersResult`, `PurchaseOrderResult`, `CreatePurchaseOrderRequest`, `POLineInput` types. `purchaseOrderService` wired into `NewAppService` in both `cmd/app` and `cmd/server`. 4 AI tools: `get_purchase_orders` (read), `get_open_pos` (read, DRAFT+APPROVED), `create_purchase_order` (write/nil), `approve_po` (write/nil). 7 integration tests in `purchase_order_integration_test.go` (`setupPurchaseOrderTestDB`). Note: DATE columns use `::text` cast in SELECT queries.
- **Phase 13**: Goods Receipt Against PO — migration 023 (`po_line_id` on `inventory_movements` + `received_at` on `purchase_orders`). `ReceivePO` on `PurchaseOrderService` (validates APPROVED status, calls `InventoryService.ReceiveStock` with `poLineID *int` for goods lines, posts `DR expense_account / CR AP` for service lines via `Ledger.Commit`, status → `RECEIVED`, sets `received_at`). `ReceivedLine`/`ReceivedLineInput` structs. `ReceivePurchaseOrder` on `ApplicationService`. `POReceiptResult` result type. `ReceivePORequest` request type. `InventoryService.ReceiveStock` signature extended with `poLineID *int` (nil for standalone receipts). 2 AI tools: `check_stock_availability` (enhanced read with optional PO context), `receive_po` (write/nil). `setupReceivePOTestDB` helper. 4 new subtests in `TestPurchaseOrder_ReceivePO`. 65 total tests (all pass).
- **Phase 14**: Vendor Invoice + AP Payment — migration 024 (adds `invoice_number`, `invoice_date`, `invoice_amount`, `pi_document_number`, `invoiced_at`, `paid_at` to `purchase_orders`). `RecordVendorInvoice` on `PurchaseOrderService` (validates RECEIVED, creates+posts PI document with gapless `PI-YYYY-NNNNN` number, returns warning if invoice amount deviates >5% from PO total, status → INVOICED). `PayVendor` on `PurchaseOrderService` (validates INVOICED, posts `DR AP / CR Bank` via `CommitInTx` atomically with status update, status → PAID). `RecordVendorInvoice`/`PayVendor` + `VendorInvoiceRequest`/`PayVendorRequest`/`VendorInvoiceResult`/`PaymentResult` on `ApplicationService`. 4 AI tools: `get_ap_balance` (read), `get_vendor_payment_history` (read), `record_vendor_invoice` (write/nil), `pay_vendor` (write/nil). `TestPurchaseOrder_FullLifecycle` (5 subtests). **70 total tests** (all pass).
- **Phase WF5**: AI Chat Home + Document Upload — `GET /` serves `chat_home.templ` (full-screen chat, `ChatLayout`). `POST /chat` SSE streaming, `POST /chat/upload` (JPG/PNG/WEBP, UUID filename, 30-min cleanup), `POST /chat/confirm` executes write tools via `ExecuteWriteTool`, `POST /chat/clear`. `Attachment` struct added to `internal/app` and `internal/ai`; `InterpretDomainAction` variadic (`...Attachment`). `pendingStore` TTL 15 min, background purge every 5 min. `sessionStorage['chat_history']` shared between chat home and app-layout slide-over. New files: `internal/adapters/web/ai.go`, `web/templates/pages/chat_home.templ`.
- **Phase WD0**: Sales Order Web UI — `GetOrder` added to `ApplicationService`. `internal/adapters/web/orders.go` (page + API handlers). Templates: `customers_list.templ`, `products_list.templ`, `stock_levels.templ`, `orders_list.templ` (status filter pills), `order_detail.templ` (Alpine.js lifecycle buttons), `order_wizard.templ` (Alpine.js dynamic lines, auto-fill unit price), `order_shared.templ`. All browser routes for sales/inventory wired. Full SO lifecycle (DRAFT → PAID) completable from web UI.
- **Phase WD1**: Vendor & PO Web UI — **FINAL MVP PHASE**. `GetPurchaseOrder` added to `ApplicationService`. `internal/adapters/web/vendors.go` (page + API handlers). Templates: `vendors_list.templ`, `vendor_form.templ`, `po_list.templ`, `po_detail.templ` (inline expandable lifecycle forms for approve/receive/invoice/pay), `po_wizard.templ` (dual line types: goods vs service/expense). 7 browser routes + 10 API routes wired. Full procurement lifecycle (DRAFT → PAID) completable from web UI. 70 tests passing, `go build ./...` clean.

**MVP complete.** All phases (WF2–WF5, 11–14, WD0–WD1) are done. 70 tests passing. Next work items are deferred phases from `docs/Tax_Regulatory_Future_Plan.md` (GST, TDS, GSTR) or user testing guides.

**Pending — User Testing Guides:**
- `docs/user_testing/` contains a `README.md` defining the structure and scope of workflow testing guides for the web UI.
- Individual guides (one per workflow, e.g. `login.md`, `sales-order.md`, `trial-balance.md`) must be written as each web UI domain phase (WD0–WD3) is delivered.
- Each guide must include: prerequisites, numbered steps with exact UI labels and input values, expected results, pass criteria, and fail indicators.
- No guides exist yet — this work begins when Phase WF3 (login UI) and WD0 (dashboard + trial balance) are complete.

When implementing future phases: New domains call `Ledger.Commit()` or `Ledger.CommitInTx()` — they never construct `journal_lines` directly. Follow the TX-scoped service method pattern from `InventoryService` for any operations that must be atomic with order state transitions.

**Multi-company usage:** When the database contains more than one company, set `COMPANY_CODE=<code>` in `.env`. Without it, the system selects the single company automatically and errors if multiple companies are found.
