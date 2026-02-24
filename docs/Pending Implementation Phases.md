# Pending Implementation Phases

This document tracks all remaining implementation phases for the `accounting-agent` project. **Phase 1 (The Business Event Layer) has been cancelled.** The architecture will continue to build on top of the existing direct-ledger-commit model.

Phases are categorized by implementation risk:
- 🟢 **Easy** — Non-breaking; can be implemented incrementally without disrupting existing functionality.
- 🔴 **Difficult** — Breaking or high-risk; requires careful planning, migration, and/or backward-compatibility work to avoid disrupting existing functionality.

---

## 🟢 Easy to Implement (Non-Breaking)

These phases introduce new capabilities alongside existing code with minimal risk of regression.

---

### Phase 0.5: Technical Debt & Hardening

**Status**: ✅ Complete
**Objective**: Address known technical debt deferred from Phase 0 before further feature work begins.

| Task | Status | Notes |
|------|--------|-------|
| **Idempotent Migrations** | ✅ Done | `001_init.sql` and `002_sap_currency.sql` updated with `CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, and `DO $$ ... EXCEPTION ... END $$` guards for all constraints. |
| **Company-Scoped `GetBalances`** | ✅ Done | `Ledger.GetBalances()` now accepts `companyCode string`, resolves `company_id`, and filters accounts and balances strictly per company. |
| **`debit_base`/`credit_base` Column Type** | ✅ Done | Go code now stores/reads these as `decimal.Decimal` (not `string`). The `::numeric` cast in the SQL query was removed — values are proper NUMERIC types end-to-end. |
| **Additional Integration Tests** | ✅ Done | Added `TestLedger_GetBalances_MultiCompany` verifying strict cross-company isolation. `TestLedger_GetBalances` updated to call the new signature. |
| **REPL Command Aliases & Trial Balance** | ✅ Done | `bal` / `balances` for trial balance (default company). `bal<CODE>` (e.g. `bal2000`) for any company. `h` = help, `e`/`q` = exit. Trial Balance output now shows report title, company code & name, and base currency. CLI aliases: `prop`/`p`, `val`/`v`, `com`/`c`, `bal`. |

**Risk**: Very low. These are all additive or corrective changes. No API surfaces change.

---

### Phase 6: Reporting & Analytics Layer

**Status**: 🗓️ Planned — Final Phase
**Objective**: Expose computed financial statements securely without burdening the transactional database.

- [ ] **Read-Ready Projections**: Populate PostgreSQL materialized views optimized for reads (e.g., account balances by period, by company).
- [ ] **Financial Statements**: Add REPL commands `pl` (Profit & Loss), `bs` (Balance Sheet), and `refresh` (refresh materialized views) — all derived from existing `journal_lines` data.

**Risk**: Low. This is a purely additive read layer. No writes to existing tables. No existing interfaces change. Can be built incrementally.

---

### Phase 7: AI Expansion (Smart Assistance)

**Status**: Not Started
**Objective**: Expand the AI Agent's capabilities beyond text input to handle richer inputs and provide insights.

- [ ] **Multi-modal Input**: Support receipt image ingestion and invoice parsing (e.g., via OpenAI Vision API) to auto-propose journal entries.
- [ ] **Conversational Insights**: Allow the AI to query reporting projections and answer queries like "Why is COGS higher this month?"
- [ ] **Predictive Actions**: Suggest re-order or cash-flow events based on ledger velocity, surfaced as proposals for human approval.

**Risk**: Low to Medium. The AI layer (`internal/ai`) is already decoupled from the ledger. New capabilities extend the agent without touching the core ledger or database schema.

---

### Phase 11: TDS / TCS (Withholding Tax)

**Status**: Not Started
**Objective**: Implement Tax Deducted at Source (TDS) and Tax Collected at Source (TCS) as a withholding layer on payment vouchers.

This phase is architecturally simpler than the Tax Engine (Phase 9) because it adds a **deduction step** to existing payment flows rather than changing the invoice model. However, it depends on Phase 4 (Rule Engine) to drive which TDS section applies to which vendor/transaction type.

- [ ] **TDS Section Master** (`tds_sections` table): Section number (194C, 194J, etc.), description, rate, threshold limit, surcharge rules.
- [ ] **Vendor TDS Flag**: `vendors.tds_applicable bool`, `vendors.default_tds_section_id` — opt-in per vendor.
- [ ] **Deduction-Aware Payment Posting**: When paying a vendor with TDS applicable, split the payment into:
  - DR `Accounts Payable` (full invoice amount)
  - CR `Bank/Cash` (net amount after deduction)
  - CR `TDS Payable` liability account (deducted amount)
- [ ] **Cumulative Threshold Tracking**: Track `tds_vendor_ledger` (vendor_id, section_id, financial_year, cumulative_paid) to apply TDS only after the threshold is crossed.
- [ ] **TCS (Tax Collected at Source)**: Mirror of TDS applied on collections from customers. If the withholding model above exists, TCS is a configuration change — same architecture, opposite direction.
- [ ] **TDS Payable Settlement**: `/pay-tds <section> <period>` REPL command to record payment to government: DR `TDS Payable` / CR `Bank`.

**Why Relatively Easier**:
- Does not change the core invoice or journal entry schema.
- Operates as an overlay on the existing payment flow — `RecordPayment()` gains an optional deduction step.
- TCS is structurally identical to TDS (same tables, different direction).

**Depends On**: Phase 4 (Rule Engine) for section-to-vendor mapping rules. Phase 9 (Tax Engine) schema alignment recommended but not strictly required.

---

### Phase 12: Statutory Compliance & Reporting

**Status**: Not Started
**Objective**: Generate GST return data, integrate with government portals, and enforce period locking for statutory compliance.

This phase is purely additive — it reads from existing `journal_lines`, `sales_order_tax_lines` (Phase 9), and `inventory_movements` data. No changes to the core posting engine.

- [ ] **Period Locking**: Add `accounting_periods` table with `status` (`OPEN` / `LOCKED`). Enforce in `Ledger.executeCore()`: reject postings to a locked period. Unlock only via explicit `/unlock-period` command with override reason logged. This is **non-negotiable for statutory compliance** — GST returns, TDS filings, and audit trails all depend on immutability of closed periods.
- [ ] **GSTR-1 Data Export**: Query invoices by period → produce line-item JSON/CSV conforming to GSTR-1 format (B2B, B2C, CDNR, HSN summary). This is query logic on top of clean tax data — no schema changes.
- [ ] **GSTR-3B Summary**: Aggregate output tax liability, input tax credit, and net payable from `journal_lines` filtered to tax accounts. Produce GSTR-3B summary report.
- [ ] **E-Invoice (IRN) Integration**: On `InvoiceOrder()`, call the IRP API to generate an Invoice Reference Number (IRN) and QR code. Store `irn`, `irn_ack_no`, `irn_ack_date` in `sales_orders`. This is an API integration layer — does not change ledger architecture.
- [ ] **E-Way Bill**: Generate e-way bill at shipment (`ShipOrder()`) for consignments above ₹50,000. Store e-way bill number in `sales_orders`. Operational compliance layer — no accounting impact.
- [ ] **REPL Commands**: `/gstr1 <period>`, `/gstr3b <period>`, `/lock-period <YYYY-MM>`, `/unlock-period <YYYY-MM>`.

**Why Easier**:
- Period locking is additive — one new table and one guard in `executeCore()`.
- All reports are read-only queries on existing data.
- E-invoice and e-way bill are API integration tasks with no schema impact on the ledger.

**Depends On**: Phase 9 (Tax Engine) for clean `sales_order_tax_lines` data. Phase 10 (GST Implementation) for GSTIN and HSN fields.

---

## 🔴 Difficult to Implement (Breaking or High-Risk)

These phases require new database schemas, new domain models, or significant refactoring of existing interfaces. Each carries a risk of breaking existing functionality if not carefully planned.

---

### Phase 2: Order Management Domain

**Status**: ✅ Complete
**Objective**: Introduce a multi-stage business lifecycle (Orders) upstream of accounting.

| Task | Status | Notes |
|------|--------|-------|
| **Domain Schema** | ✅ Done | `customers`, `products`, `sales_orders`, `sales_order_lines` tables added via `migrations/007_sales_orders.sql`. `SO` document type added for gapless order numbering. Seed data in `migrations/008_seed_orders.sql`. |
| **Order State Machine** | ✅ Done | `DRAFT → CONFIRMED → SHIPPED → INVOICED → PAID \| CANCELLED`. State transitions enforced with row-level locks. SO number assigned via `DocumentService.PostDocumentTx()` at CONFIRMED. |
| **Order-Driven Accounting** | ✅ Done | `InvoiceOrder()` builds a `Proposal` (DR 1200 AR / CR revenue accounts per product) and calls `Ledger.Commit()`. `RecordPayment()` builds a second `Proposal` (DR bank / CR 1200 AR). Both use idempotency keys tied to order ID. |
| **Integration Tests** | ✅ Done | 7 tests: full lifecycle, multi-product revenue split, state guard violations, cancel, cross-company isolation, list/filter, invoice idempotency. All 27 tests pass (including pre-existing ledger + inventory tests). |
| **REPL Commands** | ✅ Done | `customers`, `products`, `orders` (single-word); `confirm`, `ship`, `invoice`, `payment`, `neworder` (2-word structured, intercepted before AI routing). |

**Hardcoded AR account `1200`** — marked `TODO(phase4)` for replacement by the Rule Engine.

---

### Phase 3: Inventory Engine

**Status**: ✅ Complete
**Objective**: Bring physical stock movements into the system and automate COGS booking.

| Task | Status | Notes |
|------|--------|-------|
| **Inventory Schema** | ✅ Done | `warehouses`, `inventory_items`, `inventory_movements` tables via `migrations/009_inventory.sql`. `GR` (Goods Receipt) and `GI` (Goods Issue) document types added. Seed data in `migrations/010_seed_inventory.sql`. |
| **Reservation Logic** | ✅ Done | `ReserveStockTx()` called atomically within `ConfirmOrder()` TX. `ReleaseReservationTx()` called within `CancelOrder()` TX. Hard-blocks on insufficient available stock. Service products (no `inventory_item`) silently skipped. |
| **COGS Automation** | ✅ Done | `ShipStockTx()` deducts `qty_on_hand`, converts reservations, and calls `Ledger.CommitInTx()` — all within a single TX with the order state transition. DR 5000 COGS / CR 1400 Inventory. Weighted average cost used for valuation. |
| **Ledger.CommitInTx** | ✅ Done | Refactored `execute()` to extract `executeCore()`. Added public `CommitInTx(ctx, tx, proposal)` enabling atomic ledger commits within caller-provided transactions. |
| **Integration Tests** | ✅ Done | 7 new inventory tests: ReceiveStock, WeightedAverageCost, ReserveStock, InsufficientStock, ShipStock (with COGS verification), CancelOrder reservation release, FullLifecycle. All 27 tests pass. |
| **REPL Commands** | ✅ Done | `/warehouses`, `/stock`, `/receive <product> <qty> <cost>`. `/confirm` and `/ship` now pass `inventoryService` for reservation + COGS automation. |

**Hardcoded account codes 1400 (Inventory), 5000 (COGS)** — marked `TODO(phase4)` for replacement by the Rule Engine.

*Note: By this point, the system acts as a fully functional, localized mini-ERP.*

---

### Phase 4: Policy & Rule Engine

**Status**: Not Started
**Objective**: Replace hard-coded account mappings with a configurable, versioned policy registry.

- [ ] **Deterministic Rule Registry**: Implement a locally-versioned policy registry that dictates how standard transaction types map to the Chart of Accounts.
- [ ] **Configurable Modifiers**: Allow conditional routing logic (e.g., tax account selection based on jurisdiction or order state, TDS section selection based on vendor type).
- [ ] **Tax Framework Alignment**: The rule engine must be the single source of truth for account resolution — including tax accounts (CGST Payable, Input Tax Credit, TDS Payable). Phase 9 (Generic Tax Engine) plugs into this registry rather than hardcoding account codes.
- [ ] **Validation Guardrails**: Rules must be fully unit-tested. *AI is strictly forbidden from writing or modifying rule configurations dynamically.*

**Why Difficult**:
- Requires a new abstraction layer (a rule engine) that sits between business events/orders and `Ledger.Commit(...)`.
- The current `Proposal` model and the AI agent directly determine account codes. Moving to a rule engine means the AI or user provides a *business intent* (e.g., "Pay supplier invoice"), and the rule engine resolves the correct debit/credit accounts — a significant architectural change to how proposals are generated and processed.
- Incorrect rules can silently book transactions to wrong accounts.

---

### Phase 5: Workflow, Approvals & Governance

**Status**: Not Started
**Objective**: Add enterprise-grade oversight so no financial entry is posted without authorized consent.

- [ ] **Role-Based Approvals**: Introduce user/system roles and permission constraints over state progression (e.g., only a `Finance Manager` role can approve a posting).
- [ ] **Correction Workflows**: Build explicit exception events (`CancelOrder`, `RefundPayment`) rather than allowing direct backend data mutations.
- [ ] **Audit Trail Expansion**: Bind human approval decisions to specific transactions, logging User ID and timestamp of approval.
- [ ] **Period Locking (statutory prerequisite)**: Formal controls on who can lock/unlock accounting periods, with approval workflow integration. (Basic period locking infrastructure is introduced in Phase 12; this phase adds the governance layer on top.)

**Why Difficult**:
- Requires a full authentication/authorization system that does not currently exist.
- The current REPL is a single-user, unauthenticated loop. This phase requires adding a user identity model, session management, and permission checks to the entire stack.
- `Correction Workflows` may require changes to the append-only ledger model to handle compensating actions in a structured, auditable way.

---

### Phase 8: External Integrations

**Status**: Not Started
**Objective**: Connect the deterministic accounting engine to external real-world systems.

- [ ] **Banking/Payment Feeds**: Consume external webhooks (e.g., Stripe, Plaid) to automatically propose payment settlement journal entries.
- [ ] **Third-Party APIs**: Expose HTTP endpoints allowing external e-commerce sites or ERPs to reliably inject `OrderCreated`-style data into the system.

**Why Difficult**:
- Requires an HTTP server with proper authentication, rate limiting, and idempotency guarantees for inbound webhooks.
- External webhook reliability demands durable message queuing or at-least-once delivery guarantees — infrastructure that does not currently exist.
- Security surface area expands significantly; malformed or malicious payloads must be strictly validated before touching the ledger.
- Depends on Phases 2–5 being stable and battle-tested before external data can be safely ingested.

---

### Phase 9: Generic Tax Component Model

**Status**: Not Started
**Objective**: Build a foundational, jurisdiction-agnostic tax architecture that future-proofs the system for GST, VAT, and any indirect tax regime. This is the most foundational tax phase — get this wrong and every downstream tax feature will hurt.

**Design Principle**: Do NOT build "GST-specific" logic here. Build a *generic tax abstraction* that GST (and future VAT/sales tax) will configure. The goal is: `invoice line → tax engine → multiple tax component postings` with zero hardcoded tax names in the core schema.

- [ ] **Tax Rate Master** (`tax_rates` table):
  - `id`, `company_id`, `code` (e.g., `GST18`, `GST5`), `name`, `jurisdiction` (e.g., `IN-KA`)
  - One-to-many `tax_rate_components`: `component_name` (e.g., `CGST`, `SGST`, `IGST`), `rate NUMERIC(6,4)`, `tax_account_code`, `is_input_tax bool`
  - This allows CGST+SGST split for intrastate, IGST for interstate — as configuration, not code.
- [ ] **Order Line Tax** (`sales_order_tax_lines` table): `order_line_id`, `tax_rate_component_id`, `taxable_amount`, `tax_amount`. Populated when order is confirmed or invoiced.
- [ ] **Customer Tax Fields**: Add `gstin VARCHAR(15)`, `tax_jurisdiction VARCHAR(10)`, `is_sez bool` to `customers`.
- [ ] **Product Tax Fields**: Add `hsn_code VARCHAR(8)`, `tax_category VARCHAR(20)`, `default_tax_rate_id` to `products`.
- [ ] **Tax Accounts in Chart of Accounts**:
  - Output tax liabilities: `2100 CGST Payable`, `2110 SGST Payable`, `2120 IGST Payable`
  - Input tax assets: `1300 Input Tax Credit - CGST`, `1310 Input Tax Credit - SGST`, `1320 Input Tax Credit - IGST`
  - Payables: `2200 TDS Payable`, `2210 TCS Payable`
- [ ] **Tax-Aware Invoice Posting**: `InvoiceOrder()` must post tax lines separately for each tax component: DR 1200 AR (gross) / CR 4000 Revenue (net) / CR 2100 CGST Payable / CR 2110 SGST Payable.
- [ ] **Input Tax on Purchases**: `ReceiveStock()` and purchase entries must support: DR 1400 Inventory (net) / DR 1300 ITC-CGST / DR 1310 ITC-SGST / CR 2000 AP (gross).

**Why Difficult**:
- Touches `sales_orders`, `sales_order_lines`, `customers`, `products`, `accounts` — four existing tables require schema changes.
- `InvoiceOrder()` in `order_service.go` must be refactored to compute and post tax components per line instead of a single revenue credit.
- Existing integration tests (`TestOrder_FullLifecycle`, etc.) will need updating to account for tax lines in posted journals.
- Must be done before Phase 10 (GST) and Phase 11 (TDS) can be implemented properly.

---

### Phase 10: Indian GST Implementation

**Status**: Not Started
**Objective**: Implement full Indian GST computation logic — CGST/SGST for intrastate, IGST for interstate, RCM for reverse charge — on top of the generic tax framework from Phase 9.

- [ ] **Interstate vs Intrastate Detection**: Compare `company.state_code` with `customer.tax_jurisdiction`. If same state → CGST + SGST split; if different → IGST full rate.
- [ ] **Multi-Rate GST Slabs**: Seed `tax_rates` with standard Indian slabs (0%, 5%, 12%, 18%, 28%) and their CGST/SGST/IGST component splits.
- [ ] **HSN/SAC Code Validation**: Validate `products.hsn_code` format (4/6/8 digit) and warn on missing codes at invoice time.
- [ ] **Reverse Charge Mechanism (RCM)**: For RCM vendors, flip tax posting: DR `RCM Input Tax` (asset) / CR `RCM Output Tax` (liability) instead of normal input credit. Net effect is zero but required for GSTR-3B.
- [ ] **Composition Scheme Support**: Flag `customers.is_composition_dealer` — composition dealers cannot claim ITC; suppress tax line posting for such sales.
- [ ] **SEZ / Export Handling**: Sales to SEZ or export marked as zero-rated (`is_sez = true` or `export_type`). No output tax posted; LUT bond reference stored.
- [ ] **GSTR-2A Reconciliation Hook**: Stub for future ITC claim validation against GSTN-reported purchase data.

**Why Difficult**:
- Complex conditional logic (intrastate/interstate/RCM/SEZ/export) must integrate into `InvoiceOrder()` without polluting the generic tax engine.
- Multi-rate support requires that each order line can have a different HSN/tax rate — the tax engine must compute per-line, not per-order.
- State code management requires new reference data (Indian state codes) in the schema.
- Full regression test suite needed for every GST scenario combination.

**Depends On**: Phase 9 (Generic Tax Engine schema).

---

## Summary Table

| Phase | Title | Category | Depends On | Status |
|-------|-------|----------|------------|--------|
| 0.5 | Technical Debt & Hardening | 🟢 Easy | Phase 0 ✅ | ✅ Complete |
| 2 | Order Management Domain | 🔴 Difficult | Phase 0 ✅ | ✅ Complete |
| 3 | Inventory Engine | 🔴 Difficult | Phase 2 ✅ | ✅ Complete |
| 4 | Policy & Rule Engine | 🔴 Difficult | Phases 2–3 ✅ | ⏸️ Deferred |
| 6 | Reporting & Analytics Layer | 🟢 Easy | Phase 0 ✅ | ⏸️ Deferred |
| 7 | AI Expansion (Smart Assistance) | 🟢 Easy | Phase 0 ✅ | ⏸️ Deferred |
| 5 | Workflow, Approvals & Governance | 🔴 Difficult | Phases 2–4 | ⏸️ Deferred |
| 8 | External Integrations | 🔴 Difficult | Phases 2–5 | ⏸️ Deferred |
| 9 | Generic Tax Component Model | 🔴 Difficult | Phase 4 | ⏸️ Deferred |
| 10 | Indian GST Implementation | 🔴 Difficult | Phase 9 | ⏸️ Deferred |
| 11 | TDS / TCS (Withholding Tax) | 🟢 Easy | Phase 4, 9 | ⏸️ Deferred |
| 12 | Statutory Compliance & Reporting | 🟢 Easy | Phases 9–10 | ⏸️ Deferred |

> **Current roadmap**: Phase 0.5 ✅ complete. Phase 2 ✅ complete. Phase 3 ✅ complete. **Phase 4 (Policy & Rule Engine)** or **Phase 6 (Reporting)** is next.
>
> **Tax implementation order**: Phase 9 (Generic Tax Engine) must come before Phase 10 (GST) and Phase 11 (TDS). Phase 12 (Statutory Reporting) depends on clean tax data from Phases 9–10. Period Locking is introduced in Phase 12 and governed in Phase 5.
