# Phased Implementation Plan: Business Event Processing System

This document translates the high-level roadmap into an actionable, phased implementation plan for the `accounting-agent` repository. The overarching goal is to evolve the project from an "AI-assisted ledger" into a robust, deterministic **Event-Driven Business Operating System** where accounting is simply one downstream projection.

We will execute this roadmap iteratively, prioritizing structural hardening and architectural boundaries before introducing complex domains.

### Execution Rules
1. **Testing is Mandatory:** After the completion of each phase, the system must be thoroughly tested to ensure it is working as intended and that no existing functionality is broken.
2. **Phase Sign-off:** Once a phase is successfully completed and confirmed as working, the phase must be marked as `[Completed]` in this document.
3. **Sequential Execution:** We will only move to the next phase after the current phase has been fully tested and signed off, updating the document to reflect this completion.

---

## Phase 0: Core Ledger Hardening [Completed]
**Objective**: Guarantee that the current local accounting core is highly deterministic, prevents race conditions, and operates on undeniable invariants. The ledger must become a "boringly reliable" black box that cannot corrupt itself.

**Key Objectives & Tasks:**
- [x] **Strict Immutability & Reversals**: `journal_entries` is append-only. `Ledger.Reverse()` generates atomic offsetting entries — no UPDATEs or DELETEs.
- [x] **Data Types & Precision**: All monetary values use `shopspring/decimal` in Go and `TEXT`/`NUMERIC` in PostgreSQL. No `float64` anywhere in the money pipeline.
- [x] **Idempotency Mechanisms**: `Ledger.Commit()` accepts a UUID `idempotency_key`. Duplicate submissions are detected via `ON CONFLICT (idempotency_key) DO NOTHING` and returned as an explicit error.
- [x] **Atomic Validations**: Account lookups and journal insertions occur within the same `pgx` database transaction. No phantom reads possible.
- [x] **AI-Ledger Boundary Enclosure**: AI output (`Proposal`) is strictly normalized via `Proposal.Normalize()` and validated via `Proposal.Validate()` before any ledger operation. The `LedgerService` interface has no AI dependency.
- [x] **SAP Multi-Company, Multi-Currency Architecture**: Complete schema upgrade to support:
  - `companies` table with `base_currency` (local currency per company).
  - `accounts` scoped to `company_id`.
  - `journal_entries` scoped to `company_id`.
  - `journal_lines` storing `transaction_currency`, `exchange_rate`, `amount_transaction`, `debit_base`, `credit_base`.
  - **Single currency per entry rule**: All lines in a journal entry share the same `TransactionCurrency` and `ExchangeRate` — no mixed-currency entries (SAP model).
  - `TransactionCurrency` and `ExchangeRate` are header-level fields on `Proposal`, not per-line.
  - Base-currency balancing: `Sum(Amount × ExchangeRate)` debits = credits enforced in Go.
- [x] **Test Environment Isolation**: Integration tests use `TEST_DATABASE_URL` to prevent wiping the live database. Tests are skipped if `TEST_DATABASE_URL` is not set.
- [x] **Idempotent Seed Data**: `migrations/003_seed_data.sql` seeds the default company and 15 chart of accounts entries using `ON CONFLICT DO NOTHING`.

*Go/No-Go Gate*: ✅ All integration tests (`TestLedger_Idempotency`, `TestLedger_Reversal`) and unit tests (`TestProposal_*`) pass. The system correctly enforces single-currency-per-entry, base-currency balancing, company scoping, and atomic commits.

---

## Phase 0.5: Technical Debt & Hardening
**Objective**: Address known technical debt deferred from Phase 0 before Phase 1 begins. These are non-breaking improvements that improve correctness, safety, and maintainability.

**Key Objectives & Tasks:**
- [ ] **Idempotent Migrations**: Add `IF NOT EXISTS` guards to `001_init.sql` and `002_sap_currency.sql` DDL statements so `go run ./cmd/verify-db` is safe to run on an existing database without errors. Alternatively implement a `schema_migrations` versioning table.
- [ ] **Company-Scoped `GetBalances`**: The `Ledger.GetBalances()` method currently returns balances for all accounts across all companies. Add a `companyID` parameter (or filter by default company) to prevent mixing account balances across companies in a multi-company setup.
- [ ] **`debit_base`/`credit_base` Column Type**: These columns are currently `TEXT` (stored as `decimal.StringFixed(2)`), requiring a `::numeric` cast in `GetBalances`. Migrate to `NUMERIC(14,2)` for cleaner, standard SQL.
- [ ] **Additional Integration Tests**:
  - Cross-company account scoping test: verify that account codes from one company cannot be used in a proposal for a different company. *(Note: already partially implemented with `TestLedger_CrossCompanyScoping`. Extend to test that an account code existing in Company 2 but not Company 1 is correctly rejected when used in a Company 1 proposal.)*
  - `GetBalances` regression test: verify that balances are accurately computed and returned after a commit. *(Note: `TestLedger_GetBalances` covers this.)*

*Go/No-Go Gate*: Move to Phase 1 after all tasks are checked and integration tests pass with `TEST_DATABASE_URL` set.

---

## Phase 1: The Business Event Layer (The Paradigm Shift)
**Objective**: Stop treating "journal entries" as direct inputs. Pivot to a model where *Business Events* are the primary source of truth, and journal entries are generated asynchronously or synchronously by deterministic processors observing those events.

**Key Objectives & Tasks:**
- [ ] **Central Event Store**: Create an append-only `events` table:
    - `id` (UUID)
    - `event_type` (e.g., `CustomerPaymentReceived`, `SupplierInvoiceIssued`)
    - `payload` (JSONB for strict, schema-validated event data)
    - `created_at`
    - `processing_status` (Pending, Processed, Failed)
- [ ] **Event Dispatcher & Processor Interface**: Build a robust, testable Go interface (`EventProcessor`) that consumes newly inserted events and dictates actions.
- [ ] **The Accounting Processor**: Create an accounting processor that explicitly listens for financial events and translates them into calls to `Ledger.Commit(...)`.
- [ ] **Deprecate Direct Ledger Write Prompts**: Migrate the AI Agent to propose *Business Events* instead of raw double-entry accounting lines, shifting complexity to local deterministic processors.

---

## Phase 2: Order Management Domain
**Objective**: Introduce the concept of a multi-stage business lifecycle before accounting is involved.

**Key Objectives & Tasks:**
- [ ] **Domain Schema**: Add `customers`, `products`, `orders`, and `order_items` tables.
- [ ] **Order State Machine**: Implement deterministic lifecycle rules (e.g., `Draft -> Confirmed -> Shipped -> Invoiced -> Paid`).
- [ ] **Order-Driven Events**: Emit domain events on state transitions (`OrderCreated`, `OrderConfirmed`, `PaymentCaptured`).
- [ ] **Integration to Phase 1**: Have the Phase 1 *Accounting Processor* recognize order-related events and generate the downstream journal entries (e.g., generating an Accounts Receivable entry when an `Order` transitions to `Invoiced`).

---

## Phase 3: Inventory Engine
**Objective**: Bring physical stock movements into the operational system.

**Key Objectives & Tasks:**
- [ ] **Inventory Schema**: Add `warehouses`, `inventory_movements`, and real-time computation of `stock_levels`.
- [ ] **Reservation Logic**: Tie Inventory checks to Order states (e.g., physical stock is soft-locked when an order is Confirmed).
- [ ] **Inventory Events**: Publish `StockReserved`, `StockShipped`, `StockAdjusted` events.
- [ ] **COGS Automation**: Update the Accounting Processor to automatically book Cost of Goods Sold (COGS) and reduce the Inventory Asset ledger balance strictly when a `StockShipped` event is processed.

*Note: By this point, the system acts as a fully functional, localized mini-ERP.*

---

## Phase 4: Policy & Rule Engine
**Objective**: Extract hard-coded mapping logic (e.g., "Event X hits Account Y") into configurable policies.

**Key Objectives & Tasks:**
- [ ] **Deterministic Rule Registry**: Implement a locally versioned policy registry dictating how standard events map to the Chart of Accounts.
- [ ] **Configurable Modifiers**: Allow logic routing (e.g., if Order is in state 'CA', map Tax to Account 2100; if 'NY', Account 2110).
- [ ] **Validation Guardrails**: The rules themselves must be tested locally. *AI is strictly forbidden from writing or altering rule configurations dynamically.* 

---

## Phase 5: Workflow, Approvals & Governance
**Objective**: Add enterprise-grade oversight so asynchronous states don't move without authorized consent.

**Key Objectives & Tasks:**
- [ ] **Role-Based Approvals**: Introduce user/system roles and permission constraints over Event state progression.
- [ ] **Correction Workflows**: Build specific exception handling events (`CancelOrder`, `RefundPayment`) rather than allowing manual backend data edits.
- [ ] **Audit Trail Expansion**: Bind human approval logic to specific event types, logging the User ID timestamp of approval.

---

## Phase 6: Reporting & Analytics Layer
**Objective**: Extract real-time decision-support metrics securely without hitting transactional databases.

**Key Objectives & Tasks:**
- [ ] **Read-Ready Projections**: Leverage the event layer to populate materialized views optimized for reads.
- [ ] **Financial Statements**: Standardize safe API endpoints returning computed P&L, Balance Sheet, and Trial Balances.

---

## Phase 7: AI Expansion (Smart Assistance)
**Objective**: Make the AI Agent a powerful utility on top of the robust core, ingesting documents and making confident event proposals.

**Key Objectives & Tasks:**
- [ ] **Multi-modal Input**: Support text, receipt image ingestion, and invoice parsing.
- [ ] **Conversational Insights**: Allow the AI to read reporting projections and answer questions like, "Why is COGS higher this month?"
- [ ] **Predictive Actions**: Suggest Re-order Events based on velocity, translating directly into system proposals for human approval.

---

## Phase 8: External Integrations
**Objective**: Connect the deterministic engine to the real world.

**Key Objectives & Tasks:**
- [ ] **Banking/Payment Feeds**: Listen for external webhooks (e.g., Stripe, Plaid) to propose payment settlement Events.
- [ ] **Third-Party APIs**: Provide endpoints allowing external E-commerce sites to reliably inject `OrderCreated` events into the central queue. 
