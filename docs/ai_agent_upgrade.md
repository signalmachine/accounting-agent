# AI Agent Upgrade Plan

> **Purpose**: Defines the expanded role of the AI agent for non-expert users, identifies gaps in the current Phase 31 plan, and proposes how AI capabilities should be woven across tiers rather than deferred to the end.
> **Last Updated**: 2026-02-25 (rev 5 — added Section 15: complete agent stack — RAG as regulatory knowledge layer, context engineering as working memory curation, skills as structured reasoning modules, post-execution verification; corrected §14.6 search tools ≠ RAG)
> **Status**: Living document — expand with detailed specs before implementing each capability.
>
> **Execution Principle**: **Core system first. AI upgrades gradual and need-based.** The accounting, inventory, and order management core is always the first priority. AI capabilities are upgraded in parallel but strictly incrementally — only when the corresponding domain is stable and the addition is clearly needed. No AI feature is added at the expense of core system correctness or stability. See Section 16 for the full gradualism policy.

---

## 1. The Problem with the Current Approach

The current agent plays one role: **journal entry interpreter**.

```
User types natural language event
        ↓
AI proposes a Proposal (journal entry)
        ↓
User approves → ledger commit
```

Everything else — order creation, inventory receipts, vendor payments, job management — requires the user to know the correct slash command, its exact syntax, and which operation maps to which business concept. This works for developers. It does not work for the stated target users: **small business owners and operators who have limited accounting or taxation knowledge**.

Phase 31 (AI expansion) is currently placed in Tier 5, after the REST API and full governance infrastructure. For the target user base, this sequencing is wrong. AI should be a first-class capability at every tier, not a feature added after everything else is done.

The **"AI is advisory only — human must approve before any ledger write"** principle is correct and must be preserved throughout. What needs to change is the *scope* of what the AI handles before that approval moment.

---

## 2. What AI Reliance Should Actually Mean

### 2.1 AI as Primary Navigator

The AI should be the primary interface for users who do not know the slash commands. A user should be able to say:

> "I just received 50 units of Widget A from my supplier at ₹300 each"

and the AI should recognise this as a goods receipt, ask for the warehouse if ambiguous, confirm the credit account (AP by default), and call `ReceiveStock` through `ApplicationService` — without the user knowing that `/receive P001 50 300.00` exists.

Slash commands remain as power-user shortcuts and should not be removed. But they should not be the only path for domain operations.

### 2.2 Domain-Aware Action Routing

Today the AI produces only `Proposal` objects (journal entries). It needs to be able to route to any `ApplicationService` method based on what the user describes.

Examples of what this means in practice:

| User says | AI should do |
|---|---|
| "Raise an invoice for Acme Corp for last month's consulting" | Identify the open shipped order for Acme, call `InvoiceOrder` |
| "Mark the shipment for order SO-2026-00012 as sent" | Call `ShipOrder` |
| "I received goods from Ravi Traders today, 100 units at ₹450" | Call `ReceiveStock` after confirming product and warehouse |
| "What does Acme Corp owe us?" | Call `GetAccountStatement` for AR, filter by customer |
| "Start a repair job for Ravi's Honda — reg KA-01-AB-1234" | Call `CreateJob` after confirming customer and service category |

This requires the agent to understand the full set of available operations, not just journal entry composition.

### 2.3 Compliance Guidance — Proactive, Not Reactive

For non-accountant users, compliance errors should be prevented before they happen, not discovered after the fact. The AI should proactively surface:

- **GST jurisdiction**: "You're invoicing a customer registered in Tamil Nadu. Your company is in Karnataka. This is an interstate supply — IGST applies. Shall I proceed with IGST?"
- **TDS threshold alerts**: "This payment to Ravi Traders (₹85,000) will cross the 194C annual threshold of ₹1,00,000 when combined with earlier payments (₹22,000 paid so far this year). TDS of ₹850 will be deducted automatically."
- **HSN code missing**: "Product 'Widget A' has no HSN code. GST invoices require HSN codes for businesses above the turnover threshold. Would you like to set one now before invoicing?"
- **Period lock warning**: "The period January 2026 is locked. This journal entry will be posted to February 2026 instead. Is that correct?"

### 2.4 Plain-English Explanations

When a journal entry is proposed or committed, the AI should explain it in terms the user understands — not just show account codes and amounts:

> "This records that Acme Corp now owes you ₹59,000 (₹50,000 for goods + ₹9,000 GST at 18%). When they pay, that ₹59,000 will move from Accounts Receivable to your bank account."

### 2.5 Natural Language Reporting

Users should be able to ask business questions in plain English:

> "What was my total revenue last month?"
> "Which customers haven't paid yet?"
> "How much stock do I have of Widget A?"
> "What's my GST liability for this quarter?"

The AI calls the appropriate `ApplicationService` or `ReportingService` method and returns a plain-English answer with the relevant numbers. This is partially described in Phase 31 but needs to be available as soon as `ReportingService` exists (Phase 8 onwards).

### 2.6 Conversational Error Recovery

Currently, when an operation fails, the user sees a raw error message:

> `order 42 cannot be invoiced: status is CONFIRMED (must be SHIPPED)`

For a non-expert user this is unhelpful. The AI should intercept errors and respond conversationally:

> "That order hasn't been shipped yet. Would you like me to mark it as shipped first, then invoice it?"

This requires the AI to understand the order lifecycle and be able to chain operations with user confirmation between each step.

### 2.7 Image and Document Ingestion

Users can attach business documents to the AI chat window. The AI reads the document content as part of the request and uses it to complete the accounting task — extracting structured data, matching entities against the database, and proposing an action for human approval. This is a high-value capability for non-expert users and is prioritised in Phase WF5 (not deferred to Phase 31).

**Documents users will attach and what the AI does with them:**

| Document | Typical format | AI action |
|---|---|---|
| Vendor invoice | PDF (from supplier software) or scanned image | Extract vendor, invoice number, date, line items, tax → propose AP journal entry (DR Expense or Inventory, CR AP) |
| Expense receipt | Photo (JPG/PNG) | Extract payee, amount, date, category → propose expense journal entry (DR Expense, CR Bank/Cash) |
| Bank statement | PDF or Excel/CSV export | Parse each row → propose journal entries for each line (batch review before confirm) |
| Customer PO received | PDF | Extract customer, items, quantities, prices → propose sales order creation |
| Goods delivery note / GRN | PDF or image | Extract supplier, items, quantities → propose `ReceiveStock` |
| Bulk data import | XLSX, CSV | Parse rows → batch proposal shown as a review table; user confirms all or selectively |

**AI extraction fields (for invoice and receipt documents):**

The AI must extract and confirm these fields before proposing an action:

- **Vendor/payee name** — matched against the `vendors` table via `search_vendors` read tool (fuzzy match); if ambiguous, AI asks user to confirm before proceeding
- **Invoice / reference number** — used as document narration and for duplicate detection
- **Invoice date** — used as posting date; if month is locked, AI warns before proposing
- **Line items** — description, quantity, unit price, line total (used for Inventory vs Expense classification)
- **Tax amounts** — separated into recoverable input tax (GST/VAT) vs non-recoverable expense component
- **Total amount** — cross-checked against sum of line items + tax (AI flags discrepancy)
- **Currency** — if different from company base currency, AI asks for exchange rate

If any required field is missing or ambiguous, the AI requests clarification before proposing — it never silently assumes values for financial fields.

**Advisory-only rule is unchanged.** Document ingestion does not bypass the approval gate. The AI produces an action card (or a batch review table for multi-row documents). The user reviews, edits if needed, and explicitly confirms. No write occurs before confirmation.

**AI processing by file type:**

| Format | Processing | Passed to AI as |
|---|---|---|
| JPG / PNG / WEBP | Base64-encode | `image_url` content block — GPT-4o vision API (natively multimodal, no extra library) |
| PDF (text-based) | Text extracted via `github.com/ledongthuc/pdf` (pure Go) | Text context block prepended to user message |
| PDF (scanned / image) | Detected by near-zero text extraction; page 1 rendered to PNG via `github.com/gen2brain/go-fitz` (CGo, MuPDF) | `image_url` content block — vision API |
| XLSX / XLS | Parsed to markdown table via `github.com/xuri/excelize/v2` (pure Go) | Text context block |
| CSV | Parsed to markdown table via `encoding/csv` (stdlib) | Text context block |
| TXT | Read raw | Text context block |

**Entity matching via read tools (not blind AI guessing):**

After extracting the vendor name from a document, the agent calls `search_vendors(name)` to find the matching vendor record in the database. It presents the match to the user if confidence is low. This keeps entity resolution deterministic and auditable — the AI reasons about the match but the user confirms it. The same applies to product matching for goods receipt documents.

**Token budget management:**

A single document can easily exceed the context budget. The agent must manage this explicitly:

- Images: passed as-is (OpenAI handles token cost internally; max image size 20 MB, but upload is capped at 10 MB)
- Text documents: truncated to ~8 000 tokens; if truncation occurs, the agent notes it and asks if the user wants to process remaining pages separately
- Excel/CSV with many rows: AI processes the first 200 rows; prompts user if more rows exist
- Multi-page PDFs: Phase 31+ adds RAG-style page selection — most relevant pages passed to the agent rather than the full document

**Phase placement:**

| Phase | Capability |
|---|---|
| WF5 (initial) | Image upload (JPG/PNG/WEBP) — GPT-4o vision, no extra Go libraries |
| WF5 follow-on | PDF text extraction — add `github.com/ledongthuc/pdf` |
| WF5 follow-on | Scanned PDF → image — add `github.com/gen2brain/go-fitz` (CGo, MuPDF) |
| WF5 follow-on | Excel / CSV — add `github.com/xuri/excelize/v2` |
| Phase 31+ | Multi-page context management, batch document workflows, RAG page selection |

**Security constraints (enforced before any AI processing):**

- MIME type validated from file bytes via `net/http.DetectContentType` — extensions alone are not trusted
- 10 MB per-file limit; 5 files per message maximum
- Files stored in `UPLOAD_DIR` with UUID names — original filenames never used on disk
- Temp files cleaned up after 30 minutes of inactivity
- No executable MIME types accepted — the whitelist enforces this; no EXE, DLL, JAR, script types pass

See `docs/web_ui_plan.md` Section 7.5 for the full upload endpoint, processing pipeline, and UI specification.

---

## 3. Gaps in the Current Phase 31 Plan

Phase 31 as written is:

> *"Receipt/Invoice image ingestion. Conversational reporting. Anomaly flagging."*

This is three bullet points for what should be a multi-phase capability. Specific gaps:

### 3.1 No Agent Tool-Calling Architecture

The current agent (`internal/ai/agent.go`) produces a single `Proposal` struct via structured output. To support domain-aware action routing (Section 2.2), the agent needs a **tool-calling architecture** — the AI selects from a set of registered tools (each mapping to an `ApplicationService` method), calls one with parameters, and the system executes it with user confirmation.

OpenAI's function calling / tool use is already supported by the `openai-go` SDK. The structured output approach used currently is the right pattern for journal entry proposals; tool calling is the right pattern for domain operations.

### 3.2 No System State Context

The AI currently receives: chart of accounts, document types, company. It does not receive: open orders, stock levels, pending invoices, customer balances. For domain navigation to work, the agent needs relevant system state as part of its context — or the ability to query it via tools.

### 3.3 No Conversation Memory Within a Session

The clarification loop accumulates `accumulatedInput` as a string, which works for a single journal entry exchange. For multi-step domain operations ("raise invoice for Acme... actually first ship the order... now invoice it"), the agent needs structured conversation history within a session.

### 3.4 Phase 31 Is in the Wrong Place

AI capabilities should be introduced incrementally alongside the domains they support:

| Domain built | AI capability to add at the same time |
|---|---|
| Phase 8 (Account Statement) | Natural language balance and statement queries |
| Phase 12 (Purchase Orders) | "Create a PO for Ravi Traders for 100 units of Widget A" |
| Phase 15 (Job Orders) | "Start a repair job for customer..." |
| Phase 23 (Tax-aware invoicing) | Proactive GST compliance guidance |
| Phase 27 (TDS) | TDS threshold alerts on vendor payments |

Keeping Phase 31 as a single catch-all AI phase means users of Phases 8–30 get no AI assistance for those operations. That is the wrong tradeoff for this user base.

---

## 4. Architectural Changes Required

The existing `ApplicationService` boundary is correct — the AI should call it, not bypass it. The changes needed are in the agent layer.

### 4.1 Tool Registry

Define a `ToolRegistry` that maps tool names to `ApplicationService` calls. The AI selects a tool by name, provides structured parameters, and the REPL displays the proposed action for user confirmation before execution.

```
User input → Agent (selects tool + parameters) → REPL shows action → User confirms → ApplicationService executes
```

### 4.2 Context Builder

> **§14 Amendment**: The ContextBuilder pattern described below is superseded by **Section 14.5**. Context engineering is implemented via read tools that the agent calls autonomously during the tool loop — not by a pre-assembly component that tries to predict what the agent needs. The ContextBuilder does not need to be built.

A `ContextBuilder` that assembles the AI prompt context dynamically based on what operation the user is likely attempting. For order-related input: include recent orders. For inventory input: include current stock levels. Avoids sending the entire system state every time.

### 4.3 Compliance Rule Hooks

> **§14 Amendment**: The compliance hook architecture described below is superseded by **Section 14.7**. Compliance checks are read tools the agent calls during its reasoning loop — not system-injected pre-execution hooks. This is a better design because the agent reasons about compliance warnings in natural language and embeds them in its proposal, rather than the system emitting them separately.

A set of pre-execution compliance checks that the AI runs (or triggers) before confirming a proposed action. These are deterministic checks (not AI-generated) that the AI presents as warnings. Example: before calling `InvoiceOrder`, check if the customer has a GSTIN and whether the jurisdiction requires IGST or CGST+SGST.

### 4.4 Explanation Generator

A lightweight post-execution function that takes the result of an `ApplicationService` call and generates a plain-English explanation. This can use the AI or be a deterministic template — the choice depends on the operation.

---

## 5. What Does Not Change

- **AI is advisory only.** Every AI-proposed action requires explicit human confirmation before any ledger write. This is non-negotiable for compliance and auditability.
- **Slash commands remain.** They are power-user shortcuts and should not be removed or hidden.
- **The `ApplicationService` boundary is the gate.** The AI never calls domain services directly — only through `ApplicationService`. This is what makes the AI replaceable and the system testable.
- **Structured output for journal entries.** The existing Responses API + strict JSON schema approach for `Proposal` generation is correct and should be kept for direct journal entry proposals.

---

## 6. Recommended Sequencing Change to Main Plan

The main implementation plan should be amended to:

1. Add AI capability sub-tasks to each Tier 3 domain phase (Phases 11–21) as each new domain is built.
2. Add natural language reporting as part of Phase 8 (Account Statement), not Phase 31.
3. Add proactive GST compliance guidance as part of Phase 25, not Phase 31.
4. Add TDS threshold alerts as part of Phase 27, not Phase 31.
5. Redefine Phase 31 as the **tool-calling architecture refactor** — the foundational change that enables all the above to work in a unified, extensible way.

---

---

## 7. Current State Assessment (as of Phase 6)

Before planning the upgrade, it is important to document what the current agent does well and what must not be disturbed.

### 7.1 What Is Already Correct

- **`AgentService` interface** (`internal/ai/agent.go:19`) is the right abstraction boundary. The rest of the system depends on this interface, not the concrete `Agent` struct. Upgrading the agent means evolving this interface, not restructuring layers.
- **Advisory-only is already enforced.** The agent returns a `core.AgentResponse` struct. Nothing is written to the database until `ApplicationService.CommitProposal()` is called explicitly by the adapter after human confirmation.
- **`internal/ai` only imports core model types** (`core.Proposal`, `core.AgentResponse`, `core.Company`) — never domain services. This is permitted by the architecture rules and is the correct boundary. It must stay this way as the agent grows.
- **Responses API + strict JSON schema for journal entry proposals** is fragile to get right and is currently working. It must not be touched unless there is a specific, tested reason to change it.

### 7.2 What the Current Agent Cannot Do

- It only handles one operation: journal entry proposal. Every other domain operation (orders, inventory, payments, jobs) requires the user to know a slash command.
- It receives only: chart of accounts, document types, company name/currency. It has no access to open orders, stock levels, customer balances, or any other live system state.
- The clarification loop (`accumulatedInput`) accumulates context as a raw string. This is sufficient for a single-entry exchange but breaks down for multi-step operations.
- There is no session memory — each call to `InterpretEvent` is stateless.

### 7.3 The Core Constraint: Agent Never Calls Domain Services

As new capabilities are added (tool-calling, context queries, compliance checks), there will be pressure to have the agent call domain services directly for convenience. This must not happen.

The rule is absolute: **the agent only receives context that is passed to it, and only proposes actions that the adapter then executes via `ApplicationService`.** The agent never calls `core.OrderService`, `core.InventoryService`, or any other domain service directly. This is what keeps the agent replaceable and the system testable.

---

## 8. Skills Architecture

> **§14 Amendment**: The Skills architecture in this section is superseded by **Section 14.2 and 14.4**. "Skills" and "tools" are the same concept — a skill is a tool definition. The key architectural distinction is not "skill vs slash command" but **read tool vs write tool**: read tools execute autonomously (no confirmation), write tools always require human confirmation. The `SkillRegistry` described here becomes the `ToolRegistry` in §14. Read §14 before implementing this section.

### 8.1 What a Skill Is

A **skill** is a registered capability the agent can select and invoke. Concretely, a skill is:

- A **name** and **description** the AI uses to decide when to invoke it
- A **parameter schema** (JSON schema) the AI fills in based on user input
- A **mapping** to one `ApplicationService` method that the adapter calls after human confirmation

Skills are domain-scoped. When the order domain is built, it registers its skills. When the inventory domain is built, it registers its own. The agent sees all registered skills as available tools in its context.

### 8.2 How the Skill Call Flow Works

The agent never executes a skill itself. It only *selects* a skill and proposes parameters. Execution always requires human confirmation, driven by the adapter.

```
User input
    ↓
Agent (reviews registered skills, selects skill + fills parameters)
    ↓
Adapter receives proposed action: {skill: "receive_stock", params: {...}}
    ↓
Adapter displays action to user in plain English
    ↓
User confirms (or cancels)
    ↓
Adapter calls ApplicationService.ReceiveStock(params)
    ↓
Domain executes, ledger written
```

This is identical in spirit to the current journal entry flow — the agent proposes, the human confirms, the adapter commits. The difference is that the scope of what can be proposed expands to cover all domain operations.

### 8.3 Skills vs. Slash Commands

Skills and slash commands serve the same domain operations but different user types:

| | Slash command | Skill (AI) |
|---|---|---|
| User type | Power user who knows the command | Non-expert who describes in plain English |
| Input | `/receive P001 50 300.00` | "I received 50 Widget A from Ravi at ₹300 each" |
| Execution | Immediate, deterministic | AI interprets → confirmation → execution |
| Both paths end at | `ApplicationService.ReceiveStock()` | `ApplicationService.ReceiveStock()` |

Slash commands must never be removed. Skills do not replace them — they add a second, more accessible path to the same operations.

### 8.4 Multi-Agent Future (Do Not Implement Yet)

The skills architecture naturally supports a multi-agent system in the future: a **router agent** that reads user intent and delegates to a **specialist agent** (journal entry agent, order agent, compliance agent, reporting agent). Each specialist has a focused skill set and context. This is a future design concern — the current upgrade should not design for it explicitly, but the skills registry and interface boundaries should not make it harder to introduce later.

---

## 9. Context Engineering and RAG

### 9.1 Current Context: Flat Text Dump

> **§14 Amendment**: Sections 9.1–9.3 (flat text dump problem, ContextBuilder pattern, RAG) are all superseded by **Sections 14.5 and 14.6**. The correct solution is not a ContextBuilder or a separate RAG pipeline — it is read tools. The agent calls `search_accounts(query)`, `get_stock_levels()`, or `get_open_orders()` to fetch exactly the context it needs. The system prompt becomes minimal (company name, currency, date). No embeddings or intent classification required for Phase 31; PostgreSQL full-text search backs the search tools initially.

The current agent receives the entire chart of accounts and document types as flat strings in the prompt on every call. For a small company this is manageable. As the chart of accounts grows and more context types are added (open orders, stock levels, customer list), this approach becomes:

- **Token-wasteful**: sending 50+ account codes when only 3–4 are relevant
- **Noisy**: the model must reason over irrelevant accounts, increasing the chance of incorrect account selection
- **Expensive**: prompt token cost grows linearly with every new context type added

### 9.2 Context Builder Pattern

The `ContextBuilder` (described in Section 4.2) is the correct mitigation. The principle: inject only the context that is relevant to the likely intent of this specific input.

Rules for context selection:

| Detected intent | Context to inject |
|---|---|
| Goods receipt / inventory | Inventory accounts, AP accounts, current stock levels for mentioned products |
| Sales invoice / AR | Revenue accounts, AR accounts, open shipped orders for mentioned customer |
| Vendor payment | AP accounts, bank accounts, open purchase invoices for mentioned vendor |
| Balance query | No accounts needed — route to reporting |
| Ambiguous / unknown | Fall back to full chart of accounts |

Intent detection does not need to be perfect. A lightweight keyword scan or a fast preliminary AI call (cheap model) can classify the input before the main context is assembled.

### 9.3 RAG for Chart of Accounts (Future)

For large charts of accounts (100+ codes), semantic retrieval is better than keyword matching. The user describes a business event; the system retrieves the top-N most semantically relevant accounts and injects only those. This requires embedding the chart of accounts at setup time and querying by cosine similarity at runtime.

This is a future capability. The `ContextBuilder` pattern above is the pragmatic first step that does not require an embedding store.

### 9.4 Schema Fragility Warning

The JSON schema for `Proposal` in `internal/ai/agent.go` (`generateSchema()`) is **hand-written** and must be kept in sync with `core.Proposal` manually. Any change to `core.Proposal` fields (add a field, rename a field, change a type) must also update `generateSchema()`. This is a silent failure mode — the schema may silently accept or reject fields after a mismatch.

Before the tool-calling architecture is built, consider:
- Adding a test that cross-validates `generateSchema()` against the actual `core.Proposal` struct fields
- Or migrating to schema generation from the struct (e.g. `invopop/jsonschema`) with a test that pins the output

---

## 10. Upgrade Risks and Rules of Engagement

The agent is working. Once it breaks, it is hard to restore. These rules govern how the upgrade should be approached.

### 10.1 Be Additive, Not Disruptive

The correct upgrade pattern is:

1. Add a **new method** to `AgentService` (`InterpretDomainAction` or similar) for tool-calling behaviour
2. Leave `InterpretEvent` (journal entry path) completely untouched
3. Prove the new method works in isolation before wiring it into the REPL
4. Only retire or consolidate the old method after the new path has been stable across multiple phases

Never modify `InterpretEvent` as a side effect of adding tool-calling support.

### 10.2 Tool Calling and Structured Output Are Different API Modes

> **§14 Amendment**: This section correctly identifies the two-mode problem as a short-term constraint. **Section 14.4** describes the long-term resolution: journal entries become a `propose_journal_entry` write tool, eliminating the split entirely. The migration path in §14.4 is additive and preserves the rule in §10.1 (do not touch `InterpretEvent` during the initial build). The constraint below remains valid until §14.4 step 4 is reached.

OpenAI's Responses API supports structured output (JSON schema) and tool calling, but they behave differently and cannot be trivially mixed in one request. The upgrade must maintain two distinct call patterns:

- **Structured output** (`InterpretEvent`): for journal entry proposals — current, working, do not change
- **Tool calling** (`InterpretDomainAction`): for domain operations — new, to be built

Attempting to unify them prematurely will break the journal entry path.

### 10.3 Session Memory Is a REPL Concern, Not an Agent Concern

Multi-step tool operations (ship → invoice → confirm payment in sequence) require the REPL to maintain structured conversation history — not just a concatenated string. This is the REPL adapter's responsibility. The agent should receive structured `[]Message` history as input, not manage it internally. Keep this separation: the REPL owns the session, the agent processes each turn.

### 10.4 The Agent Must Remain Replaceable

The `AgentService` interface is what makes the agent replaceable. When adding capabilities:

- New operations must be added as new interface methods, or by extending existing method signatures in a backward-compatible way
- No caller outside `internal/ai` should depend on `Agent` (the concrete struct) — only on `AgentService`
- The system must compile and run (with reduced capability) even if the `Agent` implementation is replaced with a stub

### 10.5 Never Query Domain Services from the Agent

As context needs grow, there will be pressure to have `Agent` call `OrderService` or `InventoryService` directly to fetch live data. This must not happen — ever. Live data for context must be:

1. Assembled by `ApplicationService` before the agent call
2. Passed into the agent as part of the context parameter
3. Or fetched by the agent via a dedicated read-only **context tool** (a tool that only queries, never writes, and is gated through `ApplicationService`)

---

## 11. Recommended Implementation Approach

When the time comes to build the tool-calling architecture, the steps are:

1. **Define `InterpretDomainAction`** on `AgentService` — takes user input + skill list + context, returns a proposed tool call `{skill_name, params, reasoning}` or a clarification request. Leave `InterpretEvent` untouched.

2. **Build a `SkillRegistry`** — each skill has a name, description, parameter schema, and a reference to an `ApplicationService` method. Skills are registered by domain at startup. The registry serialises to the JSON tool definitions that OpenAI tool-calling expects.

3. **Add a web chat panel dispatch path for tool results** — the web chat panel (Phase WF5) receives a proposed tool call and renders it as a structured action card ("I will receive 50 units of Widget A at ₹300 from Ravi Traders — confirm?") with Confirm / Cancel / Edit buttons. On confirmation, the adapter calls the mapped `ApplicationService` method. This replaces the REPL's text-based confirm prompt. See `docs/web_ui_plan.md` §7.

4. **Add structured conversation history to the web session** — `[]Message{role, content}` is maintained in React state and sent as `session_history` on each `/api/ai/chat` request. The REPL's `accumulatedInput` string approach is not migrated — the web session model replaces it entirely.

5. **Build the `ContextBuilder`** — starts simple (keyword-based intent detection, injects relevant account subset). Evolves toward semantic retrieval over time.

6. **Add compliance hook calls** before execution — deterministic checks (GST jurisdiction, TDS threshold, period lock) are run by `ApplicationService` before the domain method is called. Results are surfaced as warnings in the REPL confirmation step, not as errors after the fact.

7. **Integrate incrementally** — add skill support per domain phase, not all at once. Phase 8 (reporting) gets the first read-only skills. Phase 12 (purchase orders) gets the first write skills. Each phase proves the pattern before the next extends it.

---

---

## 12. AI in the Web Context

> **Context**: The REPL is being deprecated in favour of a web UI (see `docs/web_ui_plan.md`). This section describes how each AI capability from Sections 2–11 maps to the web environment and what changes compared to the REPL approach.

### 12.1 Chat Panel as the Primary AI Interface

The web equivalent of the REPL AI loop is a **persistent chat panel**. Unlike the REPL, the chat panel has two modes:

- **Embedded on the dashboard (home page)**: The AI chat occupies the right column of the dashboard alongside KPI cards and pending actions. This is the primary entry point — the first thing a user sees after login is both their business summary *and* the AI ready to help. No need to invoke a separate mode or know a command.
- **Slide-over on all other pages**: A fixed "Ask AI" button in the header opens the same chat as a side panel, so users can ask questions or trigger domain actions without navigating away from their current screen.

Unlike the REPL, the chat panel:

- Maintains full visual conversation history within the session (rendered in the browser, not printed to a terminal)
- Renders proposed actions as structured **action cards** (not plain text confirm prompts)
- Shows compliance warnings as **amber banners** inline in the conversation — not as post-error messages
- Accepts **file uploads** (vendor invoices, receipts) via a button — no file path typing required
- Streams AI responses token-by-token via SSE, giving real-time feedback

The advisory-only constraint is unchanged: no `ApplicationService` write method is called until the user clicks Confirm on an action card.

### 12.2 Action Card UI (Replaces REPL Confirm Prompt)

When the agent proposes a skill invocation, the web chat panel renders it as a card:

```
┌─────────────────────────────────────────────┐
│  📦 Receive Stock                           │
│  Product: Widget A (P002)                   │
│  Quantity: 50 units                         │
│  Unit Cost: ₹300.00                         │
│  Warehouse: MAIN                            │
│  Credit Account: 2000 (AP — Ravi Traders)  │
│                                             │
│  [Confirm]  [Cancel]  [Edit parameters]     │
└─────────────────────────────────────────────┘
```

The [Edit parameters] button opens an inline form pre-filled with the agent's proposed values — the user can correct any misinterpreted field before confirming.

### 12.3 Compliance Warnings as UI Components

In the REPL, compliance warnings were planned as text output before the confirmation prompt. In the web UI, they are **amber warning banners** that appear between the action card and the Confirm button:

```
⚠️  Interstate supply detected
    Your company is in Karnataka (KA). Acme Corp is registered in
    Tamil Nadu (TN). IGST at 18% applies (not CGST+SGST).
    The proposed journal entry has been updated accordingly.
```

The user sees the updated entry, reads the explanation, and then confirms — or cancels and adjusts. This is a significantly better UX than text-only output.

### 12.4 Image Upload for Document Ingestion

In the web UI, document ingestion (Phase 31 / Section 2.7) becomes a native file upload:

1. User clicks the paperclip button in the chat panel
2. Browser file picker opens — user selects a photo or PDF of a vendor invoice
3. File is sent to `POST /api/ai/upload-document` (multipart form)
4. Server passes image bytes to OpenAI Vision API
5. Agent extracts vendor name, invoice number, date, line items, amounts
6. Proposed journal entry is returned as an action card in the chat thread
7. User reviews, edits if needed, and confirms

This eliminates the file path problem inherent in the REPL approach and works naturally on mobile (camera upload).

### 12.5 Natural Language Reporting in the Web Context

When a user asks a reporting question in the chat panel ("What was my GST liability last month?"), the agent calls read-only reporting skills. The response is rendered inline in the chat thread as a mini table or summary card — not as plain text numbers. The user can then click "Open full report" to navigate to the reporting page with the correct filters pre-applied.

### 12.6 Revised Section 11 Step 3 (Web Context)

Section 11, Step 3 previously described adding a REPL dispatch path. That step is superseded:

- **Before Phase WD3 (REPL still active)**: implement both REPL and web dispatch paths in parallel so the REPL continues to work during the transition.
- **After Phase WD3 (REPL removed)**: only the web dispatch path remains. The `accumulatedInput` string from the REPL clarification loop is deleted.

### 12.7 What Does Not Change

- The `AgentService` interface and `InterpretDomainAction` method signature are UI-agnostic.
- The `SkillRegistry`, `ContextBuilder`, and compliance hook architecture are identical whether called from REPL or web. The adapter layer (REPL vs web handler) differs; the agent and application layers do not.
- Advisory-only: no write path is called without explicit human confirmation, regardless of whether the confirmation UI is a terminal `[y/n]` or a browser Confirm button.

---

---

## 13. Immediate Agent Code Fixes (Pre-Architecture Work)

Before implementing the tool-calling architecture (Phase 31), four concrete deficiencies in `internal/ai/agent.go` must be addressed. These were identified by auditing `agent.go` against the `openai-integration` skill requirements. They are independent of the architecture upgrade and should be applied as a dedicated commit — ideally before Phase 31 begins, or as the very first step within it.

All changes are confined to `internal/ai/agent.go` (and optionally `internal/ai/schema.go`). No other files require modification.

### 13.1 Phase 1 — Correctness and Reliability

**Fix 1.3 — Add `option.WithMaxRetries(3)` to client construction** *(do first — trivial, de-risks all API calls)*

The SDK defaults to 2 retries. The project standard is 3:

```go
// Before
client := openai.NewClient(option.WithAPIKey(apiKey))

// After
client := openai.NewClient(
    option.WithAPIKey(apiKey),
    option.WithMaxRetries(3),
)
```

---

**Fix 1.1 — Replace `generateSchema()` with canonical `GenerateSchema[T]()` helper**

The current `generateSchema()` function returns `interface{}` and forces a redundant `json.Marshal` → `json.Unmarshal` round-trip in the caller (lines 63–71 of `agent.go`), with inline `$schema` deletion. This is fragile and inconsistent with the project's OpenAI integration standard.

Replace with the canonical generic helper:

```go
func GenerateSchema[T any]() map[string]any {
    reflector := jsonschema.Reflector{
        AllowAdditionalProperties: false,
        DoNotReference:            true,
    }
    var v T
    schema := reflector.Reflect(v)
    data, _ := json.Marshal(schema)
    var result map[string]any
    json.Unmarshal(data, &result)
    delete(result, "$schema") // OpenAI strict mode rejects the $schema meta-field
    return result
}
```

The call site in `InterpretEvent` becomes a one-liner:

```go
schemaMap := GenerateSchema[core.Proposal]()
```

The manual marshal/unmarshal block (lines 63–74 of `InterpretEvent`) is removed entirely. Optionally extract this helper to `internal/ai/schema.go` if `agent.go` grows large — and add a test there that pins the schema output against the `core.Proposal` struct fields (see Section 9.4 schema fragility note).

---

**Fix 1.2 — Add `*openai.Error` typed inspection on API errors**

All API errors are currently wrapped uniformly with `fmt.Errorf`, losing the HTTP status code. A 429 rate-limit error and a 400 schema rejection are indistinguishable in logs:

```go
resp, err := a.client.Responses.New(ctx, params)
if err != nil {
    var apierr *openai.Error
    if errors.As(err, &apierr) {
        log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
    }
    return nil, fmt.Errorf("openai responses error: %w", err)
}
```

Add `"errors"` and `"log"` to the import block if not already present.

---

**Phase 1 acceptance criteria:**

- [ ] `GenerateSchema[T]()` helper exists in `agent.go` or `internal/ai/schema.go`
- [ ] `InterpretEvent` calls `GenerateSchema[core.Proposal]()` directly — no manual marshal/unmarshal block
- [ ] Error block on `Responses.New` uses `errors.As(err, &apierr)` before wrapping
- [ ] `NewAgent` passes `option.WithMaxRetries(3)`
- [ ] `go build ./...` compiles; all existing tests pass

---

### 13.2 Phase 2 — Observability and Cost Tracking

**Fix 2.1 — Log `resp.Usage` after every API call**

Token usage is currently discarded. Log it after every successful `Responses.New` call for future cost analysis and model routing decisions:

```go
if usage := resp.Usage; usage != nil {
    log.Printf("OpenAI usage — prompt: %d, completion: %d, total: %d tokens",
        usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
}
```

This is a non-breaking additive change. In tests, redirect `log.SetOutput(io.Discard)` in setup if the output is unwanted.

**Phase 2 acceptance criteria:**

- [ ] Token usage (prompt / completion / total) logged on every successful `Responses.New` call
- [ ] Log output visible in REPL and CLI `propose` command during manual testing
- [ ] No new dependencies introduced

---

### 13.3 Implementation Order

```
Phase 1: Fix 1.3 (retry config)
       → Fix 1.1 (schema helper refactor)
       → Fix 1.2 (typed error inspection)
Phase 2: Fix 2.1 (usage logging)
```

> **Source**: Originally documented in `docs/openai_remediation_plan.md` (derived from the OpenAI integration skill audit). That file now redirects here; the archived original is at `docs/archive/openai_remediation_plan.md`.

---

## 14. Tool Use as Primary Architectural Paradigm

> **Supersedes**: §4.2 (ContextBuilder), §4.3 (Compliance Hooks), §8 (Skills Architecture), §9.1–9.3 (Context Engineering and RAG), §10.2 (two-mode constraint — in the long term).
>
> **Core insight**: Tool calling is not one capability among several. It is the foundational mechanism through which every other agent capability — context engineering, search/RAG, compliance checks, domain navigation, clarification — is implemented. Everything flows through tools.

---

### 14.1 The Central Thesis

The current document describes tool calling, a ContextBuilder, a Skills registry, and RAG as four separate architectural concerns to be built separately and integrated. They are not separate:

| Capability | Earlier sections describe it as | Correct implementation via tools |
|---|---|---|
| Context engineering | A `ContextBuilder` that pre-assembles relevant data | Read tools the agent calls to fetch exactly what it needs |
| RAG / account search | Embedding store + similarity search pipeline | `search_accounts(query)`, `search_products(query)` read tools backed by PostgreSQL full-text search |
| Skills / domain ops | A registry mapping skill names to ApplicationService methods | Write tool definitions registered at startup |
| Compliance checks | Pre-execution hooks the system runs before the agent proposes | `check_tax_jurisdiction`, `check_tds_threshold` read tools the agent calls during its reasoning loop |
| Clarification | `is_clarification_request: true` field in structured output | `request_clarification(question, context)` write tool that terminates the loop |
| Natural language reporting | Calling ReportingService methods via a separate path | Read tools that wrap ReportingService methods |

When tool calling is the primary paradigm, the agent has one interface: a set of tools it can call. It uses read tools to understand the situation, then proposes a write tool. Every capability is an instance of this pattern. The architecture becomes simple, unified, and extensible.

---

### 14.2 Two Categories of Tools

All tools fall into exactly two categories with fundamentally different execution rules:

#### Read Tools — Agent Executes Autonomously

Read tools are pure queries — no database writes, no side effects. The agent calls them freely during the tool loop without human confirmation. Every call result is returned to the agent as a tool result and informs its next step.

**Context query tools** (available as domains are built):

| Tool | Parameters | Returns |
|---|---|---|
| `get_open_orders` | `customer_code?, status?` | List of matching orders |
| `get_order_detail` | `order_ref` | Full order with lines, status, amounts |
| `get_stock_levels` | `product_code?, warehouse_code?` | On-hand and reserved qty per product/warehouse |
| `get_customer_info` | `customer_code` | GSTIN, state code, TCS flags, credit limit |
| `get_vendor_info` | `vendor_code` | AP account, TDS section, RCM flag, payment terms |
| `get_account_balance` | `account_code, company_code` | Current balance |
| `get_account_statement` | `account_code, from_date, to_date` | Ledger lines with running balance |
| `get_tds_cumulative` | `vendor_code, section_code, financial_year` | Cumulative paid and threshold remaining |
| `get_period_status` | `posting_date` | OPEN or LOCKED |
| `get_pl_report` | `year, month` | P&L summary |
| `get_balance_sheet` | `as_of_date` | Assets, liabilities, equity totals |

**Search tools** (these replace both ContextBuilder and RAG):

| Tool | Parameters | Returns |
|---|---|---|
| `search_accounts` | `query` | Top-N matching accounts (code, name, type, balance) |
| `search_products` | `query` | Matching products (code, name, HSN, price, stock) |
| `search_customers` | `query` | Matching customers (code, name, GSTIN, open balance) |
| `search_vendors` | `query` | Matching vendors (code, name, AP balance, TDS section) |

For Phase 31, implement search tools with PostgreSQL `ILIKE` or `pg_trgm`. The tool interface is stable — the backend can be upgraded to vector search without changing the agent or callers.

**Compliance check tools** (these replace the compliance hook system):

| Tool | Parameters | Returns |
|---|---|---|
| `check_tax_jurisdiction` | `company_state_code, customer_state_code, is_sez` | `{rate_type: "IGST"\|"CGST+SGST", rate: 18, note: "..."}` |
| `check_tds_threshold` | `vendor_code, section_code, payment_amount, fy` | `{tds_applicable: bool, tds_amount, cumulative_after, note}` |
| `check_stock_availability` | `product_code, warehouse_code, qty` | `{available: bool, qty_on_hand, qty_reserved, shortfall}` |
| `check_period_lock` | `posting_date` | `{locked: bool, period, locked_at}` |
| `check_hsn_coverage` | `order_ref` | Lines missing HSN codes, if any |

#### Write Tools — Human Confirmation Required

Write tools are proposed by the agent but never executed until the user explicitly confirms. The adapter displays the proposed tool call as an action card and waits for confirmation before calling the mapped `ApplicationService` method.

**Domain operation write tools** (one per ApplicationService write method, registered as domains are built):

`create_order`, `confirm_order`, `ship_order`, `invoice_order`, `record_payment`, `receive_stock`, `create_purchase_order`, `approve_po`, `receive_po`, `record_vendor_invoice`, `pay_vendor`, `create_job`, `confirm_job`, `start_job`, `add_labour_line`, `add_material_line`, `complete_job`, `invoice_job`, `record_job_payment`, `activate_rental_contract`, `bill_rental_period`, `return_asset`, `refund_deposit`, `lock_period`, `unlock_period`, `settle_tds`, `settle_tcs`

**Journal entry write tool** (replaces `InterpretEvent` in the long term — see §14.4):

```
propose_journal_entry(
  document_type: "JE" | "SI" | "PI",
  lines: [{account_code, is_debit, amount, narration}],
  posting_date: "YYYY-MM-DD",
  summary: string,
  reasoning: string,
  confidence: number
)
```

**Special write tool:**

```
request_clarification(
  question: string,
  context: string   // what the agent has established so far
)
```

Terminates the tool loop and renders the question to the user in the chat panel. The user's answer is the next user message in the conversation history. This replaces the `is_clarification_request: true` field in the current structured output.

**The write/read distinction IS the advisory-only constraint**, expressed at the tool level rather than as a separate architectural principle.

---

### 14.3 The Agentic Tool Loop

Tool calling enables a multi-step reasoning loop within a single user interaction. The agent does not need a ContextBuilder to pre-assemble context — it calls read tools to discover exactly what it needs, then proposes a write tool.

```
User: "Invoice Acme Corp for last month's consulting"
    ↓
Agent enters tool loop
    │
    ├─ calls: get_open_orders(customer_code="ACME", status="SHIPPED")
    │         → [{ref: "SO-2026-00012", date: "2026-01-15", amount: 85000, currency: "INR"}]
    │
    ├─ calls: get_customer_info(customer_code="ACME")
    │         → {gstin: "29AAPCA1234F1Z5", state_code: "KA", is_sez: false}
    │
    ├─ calls: check_tax_jurisdiction(company_state="KA", customer_state="KA", is_sez=false)
    │         → {rate_type: "CGST+SGST", rate: 18, note: "Intrastate supply"}
    │
    ├─ calls: check_hsn_coverage(order_ref="SO-2026-00012")
    │         → {missing: []}   (all products have HSN codes — no warning needed)
    │
    └─ proposes write tool: invoice_order(order_ref="SO-2026-00012")
              reasoning: "SO-2026-00012 is shipped and unpaid. CGST+SGST at 18% applies
                          (intrastate, Karnataka to Karnataka). No HSN gaps."
    ↓
Adapter renders action card:
┌────────────────────────────────────────────────────────────┐
│  Invoice Sales Order                                       │
│  Order:    SO-2026-00012  (Acme Corp)                      │
│  Net:      ₹85,000                                         │
│  CGST 9%:  ₹7,650  →  account 2100                        │
│  SGST 9%:  ₹7,650  →  account 2110                        │
│  AR total: ₹1,00,300  →  account 1200                      │
│                                                            │
│  Intrastate supply — CGST+SGST applies                     │
│                                                            │
│  [Confirm]  [Cancel]  [Edit]                               │
└────────────────────────────────────────────────────────────┘
    ↓
User clicks Confirm
    ↓
Adapter calls: ApplicationService.InvoiceOrder("SO-2026-00012", companyCode)
    ↓
Agent generates plain-English explanation:
    "Done. Acme Corp now owes you ₹1,00,300 (₹85,000 for consulting
     + ₹15,300 GST at 18%). This appears in your Accounts Receivable.
     When they pay, run /payment or tell me and I'll record it."
```

**Loop invariants:**
- The agent calls **0 or more** read tools (autonomously, no confirmation)
- The agent calls **exactly 1** write tool or `request_clarification` to terminate the loop
- Read tool results are returned to the agent and appear in the conversation history
- Write tool proposals are displayed to the user — never auto-executed
- If the agent cannot determine the right action after calling read tools, it uses `request_clarification`

---

### 14.4 Resolving the Two-Mode Problem (supersedes §10.2 long-term)

Section 10.2 correctly identifies that structured output and tool calling cannot be trivially mixed. This is a valid short-term constraint. The long-term resolution is to eliminate the split by making journal entries a write tool.

**Unified model (end state):**
```
ALL user inputs → InterpretDomainAction (tool loop)
  → 0-N read tool calls (autonomous)
  → 1 write tool call, including propose_journal_entry for freeform events
  → human confirmation
  → ApplicationService executes
```

**Migration path (strictly additive — preserves §10.1):**

| Step | Action | Prerequisite |
|---|---|---|
| 1 | Build `InterpretDomainAction` with domain write tools (orders, inventory, jobs) | — |
| 2 | Add `propose_journal_entry` as a write tool in `InterpretDomainAction` | Step 1 stable in production |
| 3 | Route all natural-language input through `InterpretDomainAction` first | Step 2 |
| 4 | Build a regression corpus: run 50 representative journal entry descriptions through both paths and compare outputs | Step 2–3 |
| 5 | When corpus agreement ≥ 95%: retire `InterpretEvent` and the structured output code path | Step 4 |

**`InterpretEvent` is not touched during steps 1–3.** The two paths co-exist during the transition. Only after step 4 validates correctness does step 5 remove the old path.

The `propose_journal_entry` write tool takes the same parameters as the current `core.Proposal` struct. The validation logic in `Proposal.Validate()` and `Proposal.Normalize()` is reused unchanged — the tool implementation calls them before returning the proposal to the adapter.

---

### 14.5 Context Engineering via Read Tools (supersedes §4.2 ContextBuilder)

The ContextBuilder in §4.2 requires the system to predict what context the agent needs before the agent runs. This prediction requires an intent classifier, which is wrong in edge cases and must be maintained as new domains are added.

With read tools, the agent assembles its own context. There is no intent classifier, no ContextBuilder component, and no maintenance burden. The agent calls what it needs:

- Doesn't know which order to invoice? Calls `get_open_orders`.
- Doesn't know the account code for "insurance expense"? Calls `search_accounts("insurance")`.
- Doesn't know the GST rate? Calls `check_tax_jurisdiction`.
- Doesn't know if the payment will trigger TDS? Calls `check_tds_threshold`.

**System prompt (minimal):** Company name, base currency, today's date, and a one-line instruction to use tools to gather information before proposing actions. Nothing else. No chart of accounts. No document types list. All live data comes from read tool calls.

**Token economics:**
- Current: every call sends the full chart of accounts (50+ accounts × ~30 tokens = 1,500+ tokens per call)
- With read tools: `search_accounts` call costs ~50 tokens for the tool definition + ~100 tokens for a targeted result. For calls that do not need account lookup, the cost is zero.
- Net effect: token cost per call decreases for focused operations; agent gets richer, more accurate context for complex ones.

**Scaling:** As the system grows (more accounts, more customers, more products), the token cost stays constant because the agent only fetches what it uses on each turn.

---

### 14.6 Search Tools as Database Lookup (not RAG)

> **Clarification**: This section describes *database search tools* — finding records that already exist in PostgreSQL. This is **not the same as RAG**. RAG (Retrieval Augmented Generation) is a separate concern covering regulatory and policy knowledge that lives outside the database (tax law, GST circulars, accounting standards). See **Section 15.2** for the full RAG design.

The vector embedding store described in §9.3 is a future optimisation, not a Phase 31 requirement. For Phase 31, implement search tools using PostgreSQL:

```sql
-- search_accounts implementation
SELECT code, name, account_type, current_balance
FROM accounts
WHERE company_id = $1
  AND (name ILIKE '%' || $2 || '%' OR code ILIKE '%' || $2 || '%')
ORDER BY similarity(name, $2) DESC   -- pg_trgm extension
LIMIT 5;
```

The search tool interface is stable and identical regardless of whether the backend uses `ILIKE`, `pg_trgm`, `tsvector`, or a vector store. The agent calls `search_accounts("insurance expense")` — it does not care how the results are ranked. The backend can be upgraded to semantic search later without changing the agent, the ToolRegistry, or any caller.

**What to build at Phase 31:**
1. `pg_trgm` extension enabled in a migration (if not already present)
2. GIN index on `accounts.name`, `products.name`, `customers.name`, `vendors.name`
3. Four search read tools backed by `pg_trgm` similarity queries

**What to defer:** Vector embeddings, cosine similarity, embedding refresh jobs. These are Phase 35+ optimisations for large deployments.

---

### 14.7 Compliance Checks as Read Tools (supersedes §4.3)

In §4.3, compliance checks are described as pre-execution hooks — deterministic checks the system runs before the agent proposes an action. This creates coupling between the compliance check system and the execution pipeline.

With read tools, the agent itself calls compliance checks during the tool loop. The results are part of the agent's reasoning, not a system gate:

```
Agent reasoning:
  check_tax_jurisdiction → {rate_type: "IGST", rate: 18}
  check_tds_threshold → {tds_applicable: true, tds_amount: 5000, note: "threshold crossed"}
  → propose write tool: pay_vendor(vendor="RAVI", amount=50000, tds_amount=5000)
  → reasoning includes: "TDS of ₹5,000 applies (194C, threshold crossed). Net payment ₹45,000."
```

The action card rendered by the adapter includes the compliance information because it came from the agent's reasoning — it is not a separate amber banner bolted on by the system. The agent explains the compliance issue in plain English as part of the action description.

**Benefits over the hook approach:**
- No separate compliance check registry to maintain
- The agent can explain *why* a compliance warning applies, not just that it does
- Compliance behaviour evolves by updating tool descriptions and agent prompts, not by modifying a pre-execution hook system
- Adding a new compliance check = adding a new read tool; no changes to the execution pipeline

---

### 14.8 MCP Compatibility as a Design Goal

[Model Context Protocol (MCP)](https://modelcontextprotocol.io/) is a standardised protocol for how AI applications expose tools to language models. Building MCP-compatible tool definitions from the start has zero extra cost and significant upside:

**What MCP compatibility enables:**
- The accounting agent's tools can be exposed as an MCP server — any MCP-compatible AI client (Claude Desktop, other agents) can use this system's tools without custom integration
- External MCP servers can be mounted into the agent's tool set: a GST portal MCP server, a bank statement MCP server, a Razorpay payment MCP server. The agent can call these alongside its own tools without any architecture changes
- Each tool is independently discoverable and testable via the MCP protocol

**What MCP compatibility requires:**
Each tool definition must be a JSON object: `{name: string, description: string, inputSchema: JSON Schema}`. This is identical to what OpenAI tool calling expects. There is no additional work.

**Action when building the `ToolRegistry` (§11 Step 2):**

```go
type ToolRegistry struct {
    readTools  []ToolDefinition
    writeTools []ToolDefinition
}

type ToolDefinition struct {
    Name        string
    Description string
    InputSchema map[string]any  // JSON Schema
    Handler     func(ctx, params) (any, error)  // for read tools
    ServiceRef  string  // ApplicationService method name for write tools
}

func (r *ToolRegistry) ToOpenAITools() []openai.Tool { ... }
func (r *ToolRegistry) ToMCPTools() []mcp.Tool { ... }
// Both serializations share identical underlying definitions
```

The two serialization methods are ~10 lines each. The investment is minimal; the optionality is significant.

---

### 14.9 Revised Implementation Sequencing

Amendments to the §11 implementation steps:

| Step | §11 Original | Revision |
|---|---|---|
| 1 | Define `InterpretDomainAction` on AgentService | **Keep.** New method alongside `InterpretEvent`. Leave `InterpretEvent` untouched. |
| 2 | Build `SkillRegistry` | **Rename to `ToolRegistry`.** Register read tools and write tools separately. Read tools have a `Handler func`; write tools have a `ServiceRef` string. MCP-compatible definitions from the start. |
| 3 | Web chat dispatch for tool results | **Keep.** Action cards for write tool proposals. Read tool calls are invisible to the user (they appear in conversation history but are not shown as action cards). |
| 4 | Structured conversation history | **Expand.** History includes tool call/result pairs: `{role: "assistant", tool_calls: [...]}` + `{role: "tool", content: "..."}`. Full transparency into what the agent queried and why. |
| 5 | Build `ContextBuilder` | **Remove.** Not built. Context is assembled by read tool calls during the loop. |
| 6 | Add compliance hook calls | **Reframe as read tools.** Register `check_tax_jurisdiction`, `check_tds_threshold`, etc. as read tools. The agent calls them during reasoning — no pre-execution hook system needed. |
| 7 | Integrate incrementally per domain phase | **Keep.** Phase 8 gets first read tools. Phase 12 gets first write tools. Table in §14.10. |
| **8 (new)** | Migrate `InterpretEvent` → `propose_journal_entry` write tool | After steps 1–7 stable across ≥2 domain phases. See §14.4 migration path. |
| **9 (new)** | Retire `InterpretEvent` and structured output code path | After step 8 regression corpus validates ≥95% agreement. |

---

### 14.10 Tool Catalog — Registration Per Phase

Tools are registered incrementally as each domain phase is built. The agent sees only the tools that have been registered; it cannot propose tools that do not exist.

| Phase | Read tools registered | Write tools registered |
|---|---|---|
| **Phase 8** (Account statement) | `get_account_balance`, `get_account_statement` | — |
| **Phase 9** (P&L) | `get_pl_report`, `get_balance_sheet` | `refresh_views` |
| **Phase 31** (initial — tool architecture) | `search_accounts`, `check_period_lock` | `propose_journal_entry` (migrates `InterpretEvent`) |
| **Phase 11** (Vendor master) | `get_vendors`, `search_vendors`, `get_vendor_info` | `create_vendor` |
| **Phase 12** (Purchase orders) | `get_purchase_orders`, `get_open_pos` | `create_purchase_order`, `approve_po` |
| **Phase 13** (Goods receipt) | `get_stock_levels`, `check_stock_availability` | `receive_po` |
| **Phase 14** (AP payment) | `get_tds_cumulative`, `check_tds_threshold` | `record_vendor_invoice`, `pay_vendor` |
| **Phase 15** (Job orders) | `get_jobs`, `search_customers`, `get_service_categories` | `create_job`, `confirm_job` |
| **Phase 16** (Job lines) | `get_job_detail` | `start_job`, `add_labour_line`, `add_material_line` |
| **Phase 17** (Job completion) | — | `complete_job`, `invoice_job`, `record_job_payment` |
| **Phase 19** (Rentals) | `get_rental_assets`, `get_rental_contracts` | `create_rental_contract`, `activate_rental_contract` |
| **Phase 20** (Rental billing) | — | `bill_rental_period`, `return_asset`, `record_rental_payment` |
| **Phase 21** (Deposit) | — | `refund_deposit` |
| **Phase 22** (TaxEngine) | `get_tax_rates` | — |
| **Phase 23** (Tax invoicing) | `check_tax_jurisdiction` | *(invoice_order gains tax awareness automatically)* |
| **Phase 25** (GST rates) | `check_gst_rate`, `search_products` (enhanced with HSN), `check_hsn_coverage` | — |
| **Phase 27** (TDS) | `get_tds_threshold_status` | *(pay_vendor gains TDS deduction automatically)* |
| **Phase 28** (TCS) | `get_tcs_status` | `settle_tds`, `settle_tcs` |
| **Phase 29** (Period lock) | *(check_period_lock already registered in Phase 31)* | `lock_period`, `unlock_period` |
| **Phase 30** (GSTR export) | `get_gstr1_preview`, `get_gstr3b_preview` | `export_gstr1`, `export_gstr3b` |

By Phase 30, the agent has ~40 read tools and ~35 write tools — covering every domain operation in the system. A user can describe any business event in plain language and the agent will navigate to the correct operation.

---

### 14.11 Conversation History in the Tool Loop

Multi-turn sessions need structured history that includes tool call/result pairs — not just user/assistant text. The format follows the OpenAI Responses API conversation structure:

```json
[
  {"role": "user",      "content": "Invoice Acme Corp for last month's consulting"},
  {"role": "assistant", "tool_calls": [{"id": "tc_1", "name": "get_open_orders",
                                         "args": {"customer_code": "ACME", "status": "SHIPPED"}}]},
  {"role": "tool",      "tool_call_id": "tc_1",
                         "content": "[{\"ref\":\"SO-2026-00012\",\"amount\":85000}]"},
  {"role": "assistant", "tool_calls": [{"id": "tc_2", "name": "check_tax_jurisdiction",
                                         "args": {"company_state": "KA", "customer_state": "KA"}}]},
  {"role": "tool",      "tool_call_id": "tc_2",
                         "content": "{\"rate_type\":\"CGST+SGST\",\"rate\":18}"},
  {"role": "assistant", "tool_calls": [{"id": "tc_3", "name": "invoice_order",
                                         "args": {"order_ref": "SO-2026-00012"}}]},
  {"role": "user",      "content": "[Confirmed]"},
  {"role": "assistant", "content": "Done. Acme Corp now owes you ₹1,00,300..."}
]
```

**Storage per environment:**
- **Web UI**: Alpine.js `x-data` array serialised to a hidden form input on each POST to `/api/ai/chat`. Session ends on page close or explicit "New conversation" button.
- **REPL (transition period)**: `[]Message` slice maintained in `repl.go` session struct. Passed to `InterpretDomainAction` on each call. The current `accumulatedInput` string is replaced by this.
- **CLI**: Stateless — no history. Each `./app propose` call is a single-turn interaction.

The tool call/result pairs in the history give the agent full context for follow-up questions: "What did you find when you checked the orders?" — the agent can answer from its own prior tool calls in the session history.

---

---

## 15. The Complete Agent Stack — What Tool Use Alone Cannot Do

> **Source**: Refined from `docs/clarification_on_tool_use.txt`. Section 14 establishes tool calling as the primary paradigm but treats it as nearly self-sufficient. This section corrects that: tool calling is the *execution* layer. The intelligence that decides *which* tool, *when*, and *whether it was correct* requires three additional layers that Section 14 underspecifies: RAG, context engineering, and skills.

**Tool use alone is function calling. A real agent is tool use + RAG + context engineering + skill orchestration — tightly integrated, not bolted together.**

---

### 15.1 The Four Layers

| Layer | Role | Analogy | What breaks without it |
|---|---|---|---|
| **Tool use** | Execution — calls services, posts entries, fetches records | Hands | Nothing executes |
| **RAG** | Knowledge — retrieves regulatory/policy content from a document store | External memory | Hallucinated compliance; wrong tax treatment |
| **Context engineering** | Control — curates working memory per turn | Working memory | Duplicate entries, forgotten state, entity confusion |
| **Skills** | Reasoning — constrained modules for domain-specific calculations | Reasoning modules | Unpredictable output at scale; wrong calculations |

All four must work together. The proper flow for every agent turn:

```
User request
    ↓
[1] Context assembly     — curate working memory (system state + session history)
    ↓
[2] RAG retrieval        — fetch relevant regulatory/policy knowledge (if needed)
    ↓
[3] Skill selection      — apply constrained reasoning module for this domain operation
    ↓
[4] Tool execution       — agent calls read tools, then proposes write tool → human confirms
    ↓
[5] Verification         — deterministic invariant checks after write tool executes
    ↓
Response (plain-English explanation + verification result)
```

Each layer is described in detail below.

---

### 15.2 RAG — Regulatory and Policy Knowledge Layer

RAG in this system is **not** about searching the chart of accounts or product catalog — those are database search tools (§14.6). RAG is specifically the mechanism for retrieving **regulatory and legal knowledge** that:

- Lives outside the PostgreSQL database (government documents, statutory text, tax circulars)
- Changes when the government issues new notifications (GST circulars, CBDT notifications, MCA updates)
- Cannot be modelled as structured data without losing nuance
- Must be retrieved accurately before the agent proposes any compliance-relevant action

**What belongs in the RAG knowledge store for this system:**

| Document type | Why RAG, not a database table |
|---|---|
| Indian GST Act sections (RCM categories, exempt supply list) | Statutory text with nuance; changes via notifications |
| CBDT TDS/TCS circulars and threshold updates | Threshold changes by notification, not code changes |
| ICAI accounting standards (AS, Ind AS) | Reference text; too large and nuanced for structured tables |
| GST valuation rules (multi-currency, related-party transactions) | Interpretive; rule context matters |
| Company-specific accounting policy / SOP documents | Internal policy that governs how the company handles specific situations |

**The RAG flow in action:**

```
User: "Record a purchase from a legal firm — reverse charge applies"
    ↓
Agent calls RAG tool: search_regulations("RCM legal services")
    → Returns: "Legal services (Section 9(3), Entry 2 of Notification 13/2017-CT(R)):
                 RCM applies when supplier is individual/firm, recipient is any registered person.
                 Self-invoice required. ITC available in same period."
    ↓
Agent now knows: RCM is correctly applicable, requires self-invoice, ITC claimable.
    ↓
Agent proposes write tool: record_vendor_invoice(vendor=..., rcm_applicable=true, ...)
    — with confidence grounded in retrieved statutory text, not hallucination
```

Without RAG, the agent either hallucinates the rule or applies a hardcoded check that goes stale when regulations change. With RAG, the agent retrieves the current rule text and reasons from it.

**RAG tool definition:**

```
search_regulations(query: string, category?: "gst" | "tds" | "icai" | "company_policy")
→ Returns: [{text: string, source: string, effective_from: string, relevance_score: number}]
```

This is a read tool. The agent calls it during the tool loop, before proposing a compliance-relevant write tool.

**What to build at Phase 31 (minimal viable RAG):**

A curated markdown document store covering the most common compliance scenarios — RCM categories, TDS section summaries (194C/194J with thresholds and rates), GST rate slabs, key exempt categories. Chunk the documents and store them in a `regulations` table with full-text search. This is sufficient for Phase 31 and can be upgraded to vector embedding retrieval later.

**What to defer:** Vector embeddings, semantic similarity, automatic ingestion of new government notifications. These are correctness improvements, not Phase 31 blockers.

---

### 15.3 Context Engineering — Working Memory Curation

Section 14.5 says "system prompt should be minimal; agent fetches context via read tools." That is correct but incomplete. Context engineering is not just about *what to include* — it is equally about *what to exclude*. **Context explosion** (too much irrelevant data in the context window) is one of the most common real-world failure modes in production agents:

- The agent forgets the current transaction state because it is buried under unrelated history
- It posts duplicate entries because earlier confirmation messages are out of context
- It misinterprets entity boundaries (confuses company A's balance with company B's)
- It ignores partial payments because the prior payment entry has scrolled out of the window

**Accounting-specific context requirements (always include, per turn):**

| Context element | Why it must always be present |
|---|---|
| `company_id`, `company_code` | Every query is scoped by company — forgetting this causes cross-company data leaks |
| `financial_year` | Tax calculations, TDS thresholds, period locks all depend on FY |
| `tax_regime` (GST registered? Composition?) | Determines which tools and rules apply |
| `user_role` (ACCOUNTANT / FINANCE_MANAGER / ADMIN) | Agent must not propose actions the user cannot confirm |
| `posting_date` (today) | Determines period lock status, FY boundary |
| `base_currency` | Exchange rate calculations, GST INR valuation |
| Last 3–5 session messages (with tool calls) | Short-term memory for multi-step operations |

**What to actively exclude (context hygiene):**

| What to exclude | Why |
|---|---|
| Full chart of accounts (50+ accounts) | Agent calls `search_accounts` when needed; inject full CoA only if explicitly required |
| All historical journal entries | Inject only entries relevant to the current transaction (via `get_account_statement`) |
| Other companies' data | Never cross company_id boundaries in any context element |
| Completed/confirmed earlier operations | Once an operation is confirmed and explained, remove the intermediate tool calls from active context to prevent duplication |
| RAG results that are not relevant | If `search_regulations` returns 5 chunks, inject only the top 2 most relevant |

**Context assembly responsibility:**

Context assembly happens in `ApplicationService` before calling the agent, not inside the agent. The `ApplicationService` method `InterpretDomainAction` receives a `DomainActionContext` struct:

```go
type DomainActionContext struct {
    Company        *core.Company
    UserRole       string
    FinancialYear  int
    PostingDate    time.Time
    SessionHistory []Message          // curated, not all history
    PinnedContext  map[string]string  // key facts that must always be visible
}
```

The adapter (REPL or web handler) is responsible for maintaining session history and deciding what to prune before the next turn. The agent never manages its own context — it only receives what is given to it.

---

### 15.4 Skills — Structured Reasoning Modules

Section 14 frames "skills" as write tool wrappers (one skill per ApplicationService method). This is incomplete. Skills are more accurately **constrained reasoning routines** — mini prompt templates with structured input and structured output that may call multiple tools internally.

The purpose of skills: **reduce the agent's reasoning randomness for domain-specific calculations.** Instead of the agent "thinking freestyle" about whether TDS applies to a payment, it invokes the TDS Calculation Skill — a constrained module that follows a deterministic reasoning path and produces a structured output.

**Skills relevant to this accounting system:**

| Skill | Input | Internal tool calls | Output |
|---|---|---|---|
| `gst_applicability` | customer state, company state, supply type, product category, is_sez | `check_tax_jurisdiction`, `search_regulations("GST exemption {category}")` | `{applicable: bool, rate_type, rate, special_case, regulation_ref}` |
| `tds_calculation` | vendor_code, section_code, payment_amount, financial_year | `get_tds_cumulative`, `check_tds_threshold`, `search_regulations("section {code} threshold")` | `{tds_applicable, tds_amount, tds_rate, cumulative_after, threshold_remaining}` |
| `invoice_validation` | order_ref, customer_code | `get_order_detail`, `get_customer_info`, `check_hsn_coverage`, `check_period_lock` | `{valid: bool, warnings: [], blocking_issues: []}` |
| `rcm_evaluation` | vendor_code, supply_description | `get_vendor_info`, `search_regulations("RCM {supply_description}")` | `{rcm_applicable, basis, self_invoice_required, itc_available}` |
| `period_close_readiness` | company_code, year, month | `get_pl_report`, `get_account_balance("1200")` (AR), `get_account_balance("2000")` (AP), `check_period_lock` | `{ready: bool, open_items: [], unreconciled: [], recommendation}` |
| `forex_valuation` | transaction_currency, amount, invoice_date | `search_regulations("RBI reference rate GST")`, external RBI rate tool | `{inr_value, rate_used, rate_source, gst_basis}` |

**How skills are invoked:**

Skills are not separate AI calls — they are invoked by the primary agent during the tool loop. The agent recognises a situation requires a specific skill and calls it as a read tool that returns structured output. The skill itself may call other read tools internally and use a focused sub-prompt to reason about the calculation.

```
Agent recognises: "This is a vendor payment — need TDS calculation"
Agent calls skill as read tool: tds_calculation(vendor="RAVI", section="194C", amount=50000, fy=2026)
    → Skill calls: get_tds_cumulative(vendor="RAVI", section="194C", fy=2026)
                      → {cumulative_paid: 75000}
    → Skill calls: check_tds_threshold(...)
                      → {threshold: 100000, crossed: true, tds_amount: 500}
    → Skill returns: {tds_applicable: true, tds_amount: 500, rate: 0.01,
                      basis: "Cumulative ₹75,000 + this payment ₹50,000 = ₹1,25,000 > ₹1,00,000 threshold"}
Agent uses this structured output to propose: pay_vendor(amount=50000, tds_amount=500, net=49500)
```

**Why skills over freestyle reasoning:**

A TDS calculation must be correct every time. If the agent reasons freestyle, it may calculate the TDS on the wrong base, apply the wrong rate, or miss the per-payment vs annual threshold distinction (§1.3 in `plan_gaps.md`). A TDS Calculation Skill constrains the reasoning to the correct path — it cannot skip the threshold check, it cannot use the wrong rate.

**Skills vs slash commands vs write tools:**

| | Slash command | Write tool | Skill |
|---|---|---|---|
| User type | Power user | Any user (via AI) | Internal to agent |
| Input | Typed syntax | Natural language → structured | Structured (called by agent) |
| Output | Immediate execution | Action card → human confirm | Structured result used by agent |
| Visible to user | Yes (command) | Yes (action card) | No (internal reasoning step) |

---

### 15.5 Verification — Post-Execution Invariant Checks

The current plan ends at "ApplicationService executes → agent generates plain-English explanation." This is missing a critical step for an accounting system: **verify that the system is still internally consistent after the write.**

This is not an AI step — it is deterministic SQL. After every write tool executes, run a set of invariant checks:

| Invariant | Check | What failure means |
|---|---|---|
| Ledger balance | `SUM(debit_base) = SUM(credit_base)` for the posted journal entry | A bug in the journal entry construction — must alert, not silently pass |
| Stock non-negative | `qty_on_hand >= 0` for affected inventory items | Stock went negative — signals a missing reservation or a double-shipment |
| AR/AP cleared | After `record_payment`: `open_AR_balance` decreased by the payment amount | Payment posted to wrong account — mismatch between AR debit and bank credit |
| Period not retroactively modified | `posting_date` of the new entry is in an open period | Period lock bypass — should never happen but is worth asserting |
| Idempotency key unique | The `idempotency_key` on the journal entry is unique | Duplicate posting — system must surface this immediately |

**Implementation:**

```go
type VerificationResult struct {
    Passed   bool
    Checks   []VerificationCheck
    Failures []VerificationCheck
}

type VerificationCheck struct {
    Name    string
    Passed  bool
    Detail  string  // SQL result or computed value
}

func (s *appService) VerifyPostExecution(ctx context.Context, jeID int, writeToolName string) (*VerificationResult, error)
```

Called by the adapter immediately after `ApplicationService` executes a confirmed write tool. The result is included in the agent's plain-English explanation:

> "Journal entry posted. Ledger balance verified ✓. Stock level verified ✓. Entry is idempotent ✓."

If a check fails, the adapter surfaces it as a prominent warning — not buried in the explanation:

> "⚠ Payment posted but AR balance did not decrease as expected. Please review account 1200 for order SO-2026-00012."

**Verification is not the same as `Proposal.Validate()`**: `Validate()` is a pre-execution check (does the proposed entry balance? are account codes valid?). Verification is a post-execution check (did the system remain consistent after the write?). Both are needed.

---

### 15.6 The Complete Flow — All Four Layers Together

The full agent turn, with all four layers explicit:

```
User: "Pay Ravi Traders ₹50,000 for PO-2026-00015"
    ↓
────────────────────────────────────────────────────────
[Layer 1: Context Assembly] — ApplicationService builds DomainActionContext:
  - company_id: 1000, company_code: "CORP1", base_currency: "INR"
  - financial_year: 2026, posting_date: 2026-02-25
  - user_role: "FINANCE_MANAGER"
  - session_history: last 3 messages (PO was approved in prior turn)
  - pinned: {open_po: "PO-2026-00015", vendor: "RAVI", outstanding_ap: 50000}
────────────────────────────────────────────────────────
    ↓
[Layer 2: RAG Retrieval] — Agent decides: payment involves TDS, retrieves regulation:
  Agent calls: search_regulations("TDS 194C contractor threshold 2026")
    → "Section 194C: deduct TDS if single payment > ₹30,000 OR aggregate in FY > ₹1,00,000.
       Rate: 1% (individual/HUF), 2% (others). Deduct at time of payment."
────────────────────────────────────────────────────────
    ↓
[Layer 3: Skill Execution] — Agent recognises TDS scenario, invokes skill:
  Agent calls: tds_calculation(vendor="RAVI", section="194C", amount=50000, fy=2026)
    → Skill calls: get_tds_cumulative("RAVI", "194C", 2026) → {cumulative: 75000}
    → Skill calls: check_tds_threshold(...)     → {crossed: true, tds_amount: 500}
    → Skill returns: {tds_applicable: true, tds_amount: 500, net_payment: 49500,
                      basis: "Aggregate ₹75,000 + ₹50,000 = ₹1,25,000 > ₹1,00,000 threshold"}
────────────────────────────────────────────────────────
    ↓
[Layer 4: Tool Execution] — Agent proposes write tool:
  pay_vendor(po_ref="PO-2026-00015", gross_amount=50000, tds_amount=500, net_bank=49500,
             bank_account="1100", tds_section="194C")

  Adapter renders action card:
  ┌──────────────────────────────────────────────────────┐
  │  Pay Vendor — Ravi Traders                           │
  │  PO: PO-2026-00015                                   │
  │  Gross payment:  ₹50,000  DR Accounts Payable        │
  │  TDS deducted:   ₹500     CR TDS Payable (194C)      │
  │  Net to bank:    ₹49,500  CR Bank Account (1100)     │
  │                                                      │
  │  TDS basis: FY aggregate ₹1,25,000 > ₹1,00,000       │
  │  Rate: 1% (Section 194C, contractor)                 │
  │                                                      │
  │  [Confirm]  [Cancel]  [Edit]                         │
  └──────────────────────────────────────────────────────┘

  User clicks Confirm
  ApplicationService.PayVendor(...) executes
────────────────────────────────────────────────────────
    ↓
[Layer 5: Verification] — Adapter calls VerifyPostExecution:
  ✓ Journal entry DR/CR balanced
  ✓ AP balance decreased by ₹50,000
  ✓ TDS Payable increased by ₹500
  ✓ Idempotency key unique
────────────────────────────────────────────────────────
    ↓
Agent explanation:
  "Payment recorded. Ravi Traders received ₹49,500 — ₹500 TDS was deducted
   under Section 194C (their aggregate payments this year crossed ₹1,00,000).
   Remember to pay the ₹500 TDS to the government by the 7th of next month.
   All ledger checks passed ✓."
```

---

### 15.7 What This Means for Phase 31

Phase 31 must now deliver all five layers, not just tool calling:

| Layer | Phase 31 deliverable |
|---|---|
| **Tool use** | `InterpretDomainAction`, `ToolRegistry`, read/write tool catalog for existing domains (Phases 8–10 tools) |
| **RAG** | `search_regulations` read tool backed by a curated markdown document store (RCM categories, TDS sections, key GST rates). Vector upgrade deferred. |
| **Context engineering** | `DomainActionContext` struct. Session history pruning in web handler (last N messages). Context hygiene rules (what to include/exclude). System prompt template with mandatory accounting fields. |
| **Skills** | `gst_applicability`, `tds_calculation`, `invoice_validation` skills as internal read tools. One skill per major compliance calculation. |
| **Verification** | `VerifyPostExecution` called after every confirmed write tool. Ledger balance check, stock non-negative, AP/AR move check, idempotency assertion. |

Phase 31 is a larger phase than previously specified. It may need to be split:

- **Phase 31a**: Tool architecture (ToolRegistry, InterpretDomainAction, read/write tools for existing domains, conversation history)
- **Phase 31b**: RAG knowledge store, skill modules, post-execution verification

This split keeps each deliverable independently testable.

---

---

## 16. Gradualism Policy — How AI Capabilities Are Added

> **This section is the governing constraint for all AI work.** Sections 1–15 describe *what* to build. This section describes *when* and *at what pace*.

### 16.1 The Core Principle

The accounting, inventory, and order management core is always the first priority. The AI agent evolves in parallel with the core system, but only incrementally and only when clearly needed. No AI capability is added speculatively, in advance of its domain, or at the expense of core stability.

**The test is simple**: if removing the AI layer entirely leaves a correctly functioning accounting system, the core is healthy. The AI should always be a useful addition on top of a correct foundation — never a crutch for a fragile one.

### 16.2 Gate Conditions — When AI Work May Begin for a Domain

AI tooling for a domain phase may begin **only after all of the following are true**:

1. **Domain integration tests pass** — the domain service (e.g., `PurchaseOrderService`) has complete integration test coverage and all tests are green.
2. **ApplicationService wiring is complete** — the domain methods are reachable through `ApplicationService` and behave correctly.
3. **The domain has been running in at least one real scenario** — not just tests. A real end-to-end operation (even manually triggered via REPL or CLI) has succeeded.
4. **The AI addition has a clear, immediate user need** — "a user would struggle without this" is the bar. Speculative capabilities that might be useful later are deferred.

If any condition is not met, AI work for that domain is deferred regardless of how architecturally complete the tool design is.

### 16.3 Size of Each AI Addition

Each AI addition to a domain phase must be the **minimum needed** to address the immediate user need:

| What is acceptable per domain | What is not acceptable |
|---|---|
| 1–3 read tools for the new domain's data | Building tools for domains not yet implemented |
| 1–2 write tools mapping to the new ApplicationService methods | Pre-building the full tool catalog in advance |
| Extending one existing skill (e.g., adding a new TDS section to the TDS Calculation Skill) | Writing new skill modules for tax scenarios that don't exist in the codebase yet |
| Adding one new regulation chunk to the RAG store (e.g., the relevant TDS section text) | Loading the entire GST Act into RAG before tax phases begin |

Add what the current domain needs. Add nothing more.

### 16.4 The `InterpretEvent` Protection Rule

The existing `InterpretEvent` path (journal entry proposals via structured output) must remain **completely untouched** until:

- `InterpretDomainAction` (tool calling) has been implemented and is running in production
- At least two domain phases have used `InterpretDomainAction` write tools successfully
- A regression corpus of ≥50 journal entry descriptions has been run through both paths and agreement is ≥95%

Only after all three conditions are met does the `propose_journal_entry` write tool migration (§14.4) begin. `InterpretEvent` is retired only after step 5 of the migration path is validated. This is not optional — breaking the journal entry path would disable the system's primary current capability.

### 16.5 Phase 31 Split and Gate

Phase 31 is split into two independently gated parts:

**Phase 31a — Tool Architecture** (may begin after Phase 10 + WF5):
- `InterpretDomainAction` on `AgentService`
- `ToolRegistry` with read/write separation, MCP-compatible definitions
- Conversation history as `[]Message` including tool call/result pairs
- Read tools for Phases 8–10 domains (account balance, account statement, P&L, balance sheet)
- First write tool: `propose_journal_entry` (migrating `InterpretEvent`) — only after the tool loop is proven for read operations first
- Web chat dispatch for tool results (action cards)

**Phase 31b — Intelligence Layers** (may begin only after 31a is proven stable for ≥2 months):
- RAG knowledge store (`search_regulations` tool, curated markdown document store)
- Skill modules (`gst_applicability`, `tds_calculation`, `invoice_validation`)
- `DomainActionContext` struct and context hygiene rules
- `VerifyPostExecution` invariant check after write tools

31b must not begin until 31a has been running without incident. The layers in 31b require the tool loop to be stable before they can be meaningfully tested.

### 16.6 Resolving Tension Between Core and AI Work

When there is competition for development time:

| Priority | Work |
|---|---|
| 1 (highest) | Core domain correctness — fixing bugs, completing integration tests, resolving plan gaps |
| 2 | Core domain new phases (per the implementation plan sequence) |
| 3 | Web UI phases (WF1–WF5, WD0–WD3) — primary interface |
| 4 | AI capability additions — read/write tools for the current domain |
| 5 (lowest) | AI intelligence layers (RAG, skills, verification) |

If the core has any failing tests or unresolved gaps, AI work at priority 4–5 stops until priority 1 is resolved.

### 16.7 What Gradual Does Not Mean

Gradual does not mean AI work is low-quality or treated as an afterthought. The architecture in Sections 14–15 is the correct long-term design. Gradual means:

- **The right architecture is built incrementally** — each piece is designed correctly for the full vision but implemented only when needed.
- **Each increment is production-quality** — no "good enough for now" tool implementations that will need rewrites.
- **The order of implementation follows need** — tools for the domains users are actually using, not the domains we plan to build next.

The goal is a fully capable AI agent over the complete implementation roadmap — arrived at without destabilising the core system at any point along the way.

---

*This document will be expanded with detailed specs, API designs, and test scenarios before implementation of each AI capability begins.*
