# Implementation Plan — Gaps & Open Issues

> **Purpose**: Records known gaps, under-specified areas, and missing coverage in the main implementation plan.
> **Last Updated**: 2026-02-26 (rev 3 — §9 marked resolved; tech stack corrected in Implementation_plan_upgrade.md)
> **Status**: Living document — expand each section before starting the affected phase.

---

## 1. Tier 4 Tax Framework — Regulatory Under-Specification

The Tier 4 phases (22–30) are structured correctly as a roadmap but are not detailed enough to implement from. The business logic is governed by Indian tax law, not by system design decisions. Each phase needs its regulatory requirements captured as concrete test scenarios before coding begins.

### 1.1 Phase 26 — RCM Missing Context

The plan says: *"post self-assessment entry: DR RCM Input Tax / CR RCM Output Tax."*

What is missing:
- **Why RCM exists**: unregistered vendors don't collect GST, so the buyer self-assesses liability and simultaneously claims ITC. The net cash effect is zero, but both lines must appear on GSTR-3B separately.
- **Which vendor types trigger RCM**: not all unregistered vendors — only specific notified categories (legal services, GTA, etc.).
- **How RCM ITC claim timing works**: ITC can only be claimed in the period it is paid, not when the invoice is recorded.

**Action before Phase 26**: Document the specific RCM categories in scope, the exact journal entries with account codes, and the expected GSTR-3B output for an RCM transaction.

---

### 1.2 Phase 30 — GSTR Export Is Under-Specified

The plan says: *"structure into B2B, B2C, CDNR, HSN Summary sections."*

What is missing:
- **B2B format**: aggregated per GSTIN per tax rate, not per invoice line. Requires customer GSTIN, place of supply, invoice value, taxable value, and tax amount broken by component.
- **HSN Summary**: requires HSN code, UOM, total quantity, and total value — not just amounts. Products must have `hsn_code` and `unit` populated.
- **CDNR (Credit/Debit Notes)**: linked to the original invoice number. The plan has no credit note feature; this is a dependency gap (see Section 3 below).
- **B2CS (B2C Small)**: unregistered customer invoices below ₹2.5 lakh go here, not in B2B.
- **GSTR-3B format**: separate from GSTR-1 — aggregated liability by tax type, ITC by source, net payable. Must match GSTR-1 totals.

**Action before Phase 30**: Obtain government GSTR-1 and GSTR-3B JSON schema specifications. Write expected output for at least one B2B invoice, one B2C invoice, and one credit note before writing any export code.

---

### 1.3 Phase 27 — TDS Threshold Edge Cases

The plan correctly introduces `cumulative_paid` per financial year per vendor per section. What is missing:

- **FY boundary rollover**: `cumulative_paid` must reset to zero on 1 April each year. The schema has `financial_year INT` but no migration or service logic handles the year-end transition.
- **Per-payment vs annual thresholds**: section 194C has an annual aggregate threshold (₹1,00,000) AND a per-payment threshold (₹30,000). Both must be checked.
- **TDS on advance payments**: TDS is deductible at the time of payment OR invoice, whichever is earlier. The plan only handles payment-time deduction.

**Action before Phase 27**: Specify exact threshold rules for each seeded TDS section (194C, 194J). Add FY rollover logic to the plan.

---

### 1.4 Multi-Currency × GST — Unaddressed

The plan covers multi-currency (SAP model) and GST independently but never addresses their interaction.

Gap: If a customer is invoiced in USD, what INR value is used for GST computation? Indian GST regulations require the RBI reference rate on the date of supply to convert the transaction currency to INR for tax purposes.

**Action**: Add a sub-task to Phase 25 for multi-currency invoice GST valuation. The `ExchangeRate` stored on the sales order may differ from the RBI reference rate — a separate `gst_exchange_rate` field may be needed.

---

## 2. Missing Business Operations

### 2.1 Credit Notes / Invoice Cancellation

The plan has no phase for credit notes or invoice reversals. This creates two problems:

1. **Operationally**: businesses regularly issue credit notes for returns, price adjustments, or cancellations. The immutable ledger handles this via compensating entries, but there is no `CreditNote` domain model, no document type `CN`, and no REPL command.
2. **GSTR dependency**: GSTR-1 has a CDNR (Credit/Debit Note for Registered) section. Phase 30 cannot produce a correct GSTR-1 without credit note data.

**Action**: Add a Phase between 23 and 30 for `CreditNote` — model, service, journal entry (reverse of SI), and CDNR inclusion in GSTR-1.

---

### 2.2 Stock Adjustments

The inventory engine handles receipts, shipments, reservations, and job consumption. There is no provision for:
- **Stock write-offs** (damaged, expired, or lost goods)
- **Stock adjustments** (physical count discrepancy)
- **Inter-warehouse transfers** (mentioned in Phase 35 as `TRANSFER_OUT / TRANSFER_IN` but with no accounting entry specified)

These are common operations for any inventory-holding business. They should be added to Tier 3 alongside the existing inventory operations.

---

### 2.3 Opening Balances

The plan seeds a company and chart of accounts but has no mechanism for importing opening balances for a business that is migrating from another system. Every real deployment needs this. A bulk journal entry import or an opening balance wizard is not currently planned.

---

## 3. Tier 4 Sequencing Dependency Gap

Phase 30 (GSTR export) depends on credit notes (not planned), HSN codes on all products (no validation phase), and customer GSTINs being populated (no onboarding flow). These must exist before Phase 30 can produce a compliant export.

---

## 4. Tier 5 Under-Specification

### 4.1 ~~Phase 32 — REST API~~ — Superseded

> Phase 32 has been moved to Tier 2.5 and fully specified in [`docs/web_ui_plan.md`](web_ui_plan.md). The gaps listed below were originally recorded here; they are now addressed in that document or remain as open questions.

Gaps that carry forward as open questions in `web_ui_plan.md §11`:
- **Pagination**: list endpoints (orders, journal lines, inventory movements) will need cursor or offset pagination — not yet specified per-endpoint.
- **Rate limiting**: no rate limiting policy defined for the API. Should be addressed before public/multi-tenant deployment.
- **Request timeout policy**: chi middleware should enforce a request timeout; default value not yet chosen.
- **Webhook auth model**: bearer tokens via `api_keys` table (Phase 34) — table schema not yet written.
- **API versioning**: open question — `/api/v1/` from day one vs plain `/api/` — decision needed before Phase WF1.

### 4.2 Phase 33 — Workflow and Approvals

The plan describes roles (`ACCOUNTANT`, `FINANCE_MANAGER`, `ADMIN`) but does not specify:
- **Session token issuance**: resolved by Phase WF2 — JWT in httpOnly cookie, 1-hour rolling expiry.
- **REPL authentication**: the REPL does not authenticate — it reads `DATABASE_URL` directly from the environment. REPL is being deprecated (see `docs/web_ui_plan.md §9`); this gap expires when the REPL is removed.
- **Correction workflow for wrong journal entry**: compensating entry only (immutable ledger principle). A structured `RefundOrder` flow is not planned; it would be a compensating `CreditNote` document (see `plan_gaps.md §2.1`). This gap remains open.

---

## 5. Test Infrastructure Gaps

### 5.1 Tax Test Setup Complexity

The current `setupTestDB` truncates and reseeds a minimal schema. Once GST rates, HSN codes, customer GSTINs, TDS sections, and state codes are introduced, test setup will become significantly more complex. A shared `setupTaxTestDB` helper will be needed that seeds the full tax-relevant master data without duplicating it across test files.

### 5.2 No Performance or Load Tests

The system uses PostgreSQL row-level locks for gapless numbering and inventory operations. Under concurrent load, lock contention is a real risk. The plan has a concurrency test for document sequencing (`TestDocumentService_ConcurrentPosting`) but none for inventory reservation or tax line insertion under concurrent order processing.

---

### 5.3 Phase 7 — Inventory Test Setup Missing Rule Seeds

After Phase 7 wires `RuleEngine` into `InventoryService`, the inventory integration tests will break unless the test setup seeds the three rule types that replace the deleted constants:

| rule_type | account_code | Replaces constant |
|---|---|---|
| `INVENTORY` | `1400` | `inventoryAccountCode` |
| `COGS` | `5000` | `cogsAccountCode` |
| `RECEIPT_CREDIT` | `2000` | `defaultReceiptCreditAccountCode` |

The current inventory test setup seeds only the `AR` rule (added during Phase 6). Without the three additional rules, `ReceiveStock()` and `ShipStockTx()` will return "no active rule found" errors, causing all inventory integration tests to fail.

**Action during Phase 7**: Before switching `InventoryService` from constants to `RuleEngine` calls, add INVENTORY, COGS, and RECEIPT_CREDIT rule inserts to `setupInventoryTestDB`. Verify the seeds work against the existing constant-based code first, then replace the constants. This avoids a situation where both the production change and the test setup change fail simultaneously.

---

## 6. AI Agent Gaps

See [`ai_agent_upgrade.md`](ai_agent_upgrade.md) for full details. In brief: the current AI agent is scoped only to journal entry proposal. For the target user base (non-accountants), the agent must be significantly expanded to cover domain navigation, compliance guidance, proactive alerts, and natural language reporting. Phase 31 as currently planned is insufficient and mis-sequenced.

---

---

## 7. Web UI Gaps

See [`docs/web_ui_plan.md`](web_ui_plan.md) for the full web UI plan. Gaps specific to the web tier that are not yet resolved:

### 7.1 Migration Numbering Conflict

Inserting `013_users.sql` and `014_seed_admin_user.sql` (Phase WF2) shifts all subsequent migration numbers by +2. Every migration file from 013 onwards must be renamed before Phase WF2 begins. This is a coordination risk if multiple phases are being implemented concurrently.

**Action**: Rename migrations 013–onwards as a single atomic rename commit immediately before Phase WF2 starts. Update all references in code and docs at the same time.

### 7.2 Go Template Partials and HTMX Swap Targets

The HTMX approach requires careful partitioning of HTML templates into re-renderable fragments (e.g., an order row that can be swapped in without reloading the full list). This is a design constraint that must be considered per screen, not an afterthought. Templates that are not designed for partial rendering from the start become difficult to refactor.

**Action**: Before implementing any screen, define which fragments will be HTMX swap targets and name the template files accordingly (e.g., `order_row.html` separate from `orders_page.html`).

### 7.3 SSE and HTMX for AI Chat Streaming

The AI chat panel is embedded on the dashboard as a first-class UI element (right column, always visible) and accessible as a slide-over panel on all other pages. HTMX's SSE extension (`hx-ext="sse"`) works well for appending streamed content, but managing multi-turn chat state (conversation history, action card rendering, confirm/cancel buttons) requires Alpine.js or custom JS beyond what HTMX alone provides. The boundary between HTMX-managed content and Alpine.js-managed state must be defined before Phase WF5.

**Action**: Prototype the chat panel before full implementation. Define what HTMX handles (SSE content appending) and what Alpine.js handles (button state, session history object, confirm/cancel callbacks).

### 7.4 Form Validation UX

Go template + HTMX forms can show server-side validation errors inline, but the UX for complex forms (new order wizard with multiple line items) needs careful design. The wizard pattern (multi-step form) does not map cleanly to a single HTMX request.

**Action**: Define the new order wizard as a sequence of HTMX-driven steps, each a separate server-rendered fragment, with state accumulated in hidden fields or a server-side session key. Specify this before Phase WD0.

### 7.5 Browser-Side Data Table Behaviour

HTMX renders server-side HTML; client-side sorting and filtering of large tables (e.g., trial balance with 50+ accounts, journal entries with thousands of rows) will require either server-side sort/filter parameters on every request, or a small JS library for in-place table interaction. Neither is specified yet.

**Action**: Decide per screen whether sort/filter is server-side (preferred for correctness, simpler for large datasets) or client-side (better UX for small tables). Document the decision before Phase WF4.

---

---

## 8. Reporting Service Gaps (Phases 8–10)

### 8.1 Opening Balance Incompatibility with Account Statement (Phase 8)

`GetAccountStatement()` derives running balances by summing `journal_lines` in posting-date order. For a greenfield business starting fresh in this system, this is correct. For a business migrating mid-year from another system, there are no journal lines for pre-migration activity — the running balance will start from zero regardless of the actual account position at migration.

This is a direct consequence of the opening balances gap (Section 2.3). The account statement report cannot show a correct running balance for migrated accounts until opening balances are addressed.

**Action before Phase 8**: Explicitly scope Phase 8 as greenfield-only — running balance starts from the first entry recorded in this system. Add a visible disclaimer to the report output. Document that a proper opening balance import (Section 2.3) is a prerequisite for this report to be useful for migrating businesses.

---

### 8.2 Materialized View Refresh Strategy (Phase 9)

The current plan specifies a manual `/refresh` command as the only way to update `mv_account_period_balances`. P&L and Balance Sheet reports read from this view and will be stale between refreshes. Three strategies are possible but none has been decided:

- **Manual refresh only** (current plan): simplest. Reports may be minutes or hours stale. Users must remember to run `/refresh` before reading reports.
- **Auto-refresh after every ledger commit**: calls `REFRESH MATERIALIZED VIEW CONCURRENTLY` inside or immediately after `Ledger.executeCore()`. Adds latency to every write. Acceptable for low-volume SMBs; under concurrent load, concurrent refreshes can still contend even with `CONCURRENTLY`.
- **Background goroutine**: refreshes every N seconds. Requires a background goroutine in the server startup and a graceful shutdown hook. Introduces eventual consistency but decouples write latency from report freshness.

**Action before Phase 9**: Choose the refresh strategy and specify it in the phase tasks. For the SMB target with low concurrent write volume, auto-refresh after commit is likely the correct default — the latency overhead is negligible and reports are always fresh.

---

### 8.3 Balance Sheet Year-End Retained Earnings (Phase 10)

A correct Balance Sheet requires `Total Assets = Total Liabilities + Total Equity`. Total Equity includes retained earnings — the accumulated net income from prior financial years that has been closed into an equity account at year-end.

The current plan has no year-end closing operation. Without it, income and expense account balances carry forward across financial years, and the Balance Sheet equity total will be incorrect after the first year-end.

The `IsBalanced` check in Phase 10 can be implemented in two ways:

1. **From account-type sums** (the plan's implied approach): `Assets = Liabilities + Equity`. Requires a year-end close to accumulate retained earnings correctly across periods.
2. **From ledger identity** (always correct by construction): since every journal entry is balanced, `Sum(all debits) = Sum(all credits)` always holds. `IsBalanced` is trivially true and is a ledger integrity check, not a Balance Sheet math check.

**Action before Phase 10**: Either (a) define a `CloseYear(ctx, companyCode, year int)` operation in `ReportingService` that posts a closing entry (DR revenue accounts, CR expense accounts, net to Retained Earnings equity account), or (b) explicitly scope `IsBalanced` as a ledger-identity check — always true by construction, not a financial statement validation. Document the decision clearly; the two interpretations are very different in practice.

---

## 9. ~~Primary Roadmap Document Inconsistency — Web UI Tech Stack~~ — RESOLVED

> **Resolved (2026-02-26)**: `Implementation_plan_upgrade.md` companion table and Phase WF3 task list have been updated to reflect the correct stack: **Go + a-h/templ + HTMX 2.x + Alpine.js 3.x + Tailwind CSS v4 + Chart.js 4.x**. The outdated React/TypeScript/shadcn references have been removed. No further action required.

---

*Expand each section above before starting the affected phase. Add new gaps as they are discovered during implementation.*
