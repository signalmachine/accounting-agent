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

**Status**: Pending  
**Objective**: Address known technical debt deferred from Phase 0 before further feature work begins.

| Task | Status | Notes |
|------|--------|-------|
| **Idempotent Migrations** | ❌ Pending | `001_init.sql` and `002_sap_currency.sql` lack `IF NOT EXISTS` guards. Re-running `verify-db` on an existing DB will fail. The migration runner (`verify-db`) now uses a `schema_migrations` table, which mitigates this, but the raw SQL is not idempotent. |
| **Company-Scoped `GetBalances`** | ❌ Pending | `Ledger.GetBalances()` returns balances across **all** companies with no `company_id` filter — dangerous in a multi-company setup. |
| **`debit_base`/`credit_base` Column Type** | ❌ Pending | These columns are `NUMERIC` in the schema but stored as `TEXT` strings via Go code (`decimal.StringFixed(2)`), requiring a `::numeric` cast in `GetBalances`. Should be migrated to `NUMERIC(14,2)` with proper ORM handling. |
| **Additional Integration Tests** | ❌ Pending | Cross-company scoping test (Company 1 proposal rejected for Company 2 accounts) and `GetBalances` regression test. `TestLedger_CrossCompanyScoping` exists but needs extension. |

**Risk**: Very low. These are all additive or corrective changes. No API surfaces change.

---

### Phase 6: Reporting & Analytics Layer

**Status**: Not Started  
**Objective**: Expose computed financial statements securely without burdening the transactional database.

- [ ] **Read-Ready Projections**: Populate PostgreSQL materialized views optimized for reads (e.g., account balances by period, by company).
- [ ] **Financial Statements**: Add safe API/REPL commands returning computed P&L, Balance Sheet, and Trial Balance — derived from existing `journal_lines` data.

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

## 🔴 Difficult to Implement (Breaking or High-Risk)

These phases require new database schemas, new domain models, or significant refactoring of existing interfaces. Each carries a risk of breaking existing functionality if not carefully planned.

---

### Phase 2: Order Management Domain

**Status**: Not Started  
**Objective**: Introduce a multi-stage business lifecycle (Orders) upstream of accounting.

- [ ] **Domain Schema**: Add `customers`, `products`, `orders`, and `order_items` tables.
- [ ] **Order State Machine**: Implement deterministic lifecycle rules (e.g., `Draft → Confirmed → Shipped → Invoiced → Paid`).
- [ ] **Order-Driven Accounting**: When an Order reaches `Invoiced` status, automatically generate the downstream Accounts Receivable journal entry via direct `Ledger.Commit(...)` call (no event bus required since Phase 1 is cancelled).

**Why Difficult**:
- Requires multiple new tables and foreign key relationships.
- Introduces a new domain (`orders`) that must be carefully integrated with the existing `companies` and `accounts` scoping.
- The `Proposal` struct and `LedgerService` interface may need extension to accept `OrderID` as a reference source.
- Risk of polluting existing ledger tests if integration test setup is not carefully isolated.

---

### Phase 3: Inventory Engine

**Status**: Not Started  
**Objective**: Bring physical stock movements into the system and automate COGS booking.

- [ ] **Inventory Schema**: Add `warehouses`, `inventory_movements`, and real-time `stock_levels` computation.
- [ ] **Reservation Logic**: Tie inventory checks to Order states (soft-lock stock on Order Confirmed).
- [ ] **COGS Automation**: Automatically book Cost of Goods Sold and reduce the Inventory Asset ledger balance when a `StockShipped` movement is recorded.

**Why Difficult**:
- Depends on Phase 2 (Order Management) being fully implemented first.
- COGS automation requires the ledger to be triggered by inventory state changes — tight coupling between two new domains.
- Inventory stock levels require careful concurrency handling (race conditions on `stock_levels`).
- New schema adds significant complexity to the migration chain.

*Note: By this point, the system acts as a fully functional, localized mini-ERP.*

---

### Phase 4: Policy & Rule Engine

**Status**: Not Started  
**Objective**: Replace hard-coded account mappings with a configurable, versioned policy registry.

- [ ] **Deterministic Rule Registry**: Implement a locally-versioned policy registry that dictates how standard transaction types map to the Chart of Accounts.
- [ ] **Configurable Modifiers**: Allow conditional routing logic (e.g., tax account selection based on jurisdiction or order state).
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

## Summary Table

| Phase | Title | Category | Depends On |
|-------|-------|----------|------------|
| 0.5 | Technical Debt & Hardening | 🟢 Easy | Phase 0 ✅ |
| 6 | Reporting & Analytics Layer | 🟢 Easy | Phase 0 ✅ |
| 7 | AI Expansion (Smart Assistance) | 🟢 Easy | Phase 0 ✅ |
| 2 | Order Management Domain | 🔴 Difficult | Phase 0 ✅ |
| 3 | Inventory Engine | 🔴 Difficult | Phase 2 |
| 4 | Policy & Rule Engine | 🔴 Difficult | Phase 2 |
| 5 | Workflow, Approvals & Governance | 🔴 Difficult | Phases 2–4 |
| 8 | External Integrations | 🔴 Difficult | Phases 2–5 |

> **Recommended starting point**: Tackle **Phase 0.5** first (no risk, immediate correctness improvements), then **Phase 6** (reporting is high business value, zero breakage risk), before committing to any of the 🔴 Difficult phases.
