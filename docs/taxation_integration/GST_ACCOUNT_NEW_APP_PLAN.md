# GST Accounting Application — Implementation Document

> **Purpose**: Design and implementation plan for a standalone GST-first accounting web application for small Indian traders.
> Built from scratch, learning from this project's AI agent architecture.

---

## What This Application Is

A lightweight accounting web application where the **AI agent is the primary interface**. Designed for small traders (medical shops, retail, distributors) who process their own accounting without a dedicated accountant. The application automates GST-compliant journal entries via natural language input.

**One-line summary**: A web wrapper around PostgreSQL, with a GPT-4o agent that speaks double-entry accounting and Indian GST.

---

## REPL-First Recommendation

**Strongly recommended: build a REPL before building the web UI.**

This was the exact journey of this project (cmd/app REPL → cmd/server web UI), and it is the right order for one critical reason:

**The AI agent is the hardest part.** Getting the agent to reliably:
- Parse a "purchased 50 strips of Metformin 500mg from ABC Pharma at ₹8.50 each with 12% GST" and produce correct journal lines
- Distinguish inter-state (IGST) vs intra-state (CGST+SGST) based on supplier state
- Ask the right clarification questions (missing GSTIN, unclear state, unknown product)
- Handle bulk daily sales ("counter sales of ₹12,400 including GST at 12%")

...requires rapid iteration on prompts, tool definitions, and clarification flows — **without** the overhead of an HTTP server, SSE streaming, frontend state management, or templ compilation.

### The REPL Advantage
```
Iteration cycle with REPL:   edit → go run → test → 30 seconds
Iteration cycle with web UI: edit → make generate → make css → go run → open browser → test → 3 minutes
```

### REPL → Web Migration Cost
The REPL is throwaway code (< 300 lines). The AI agent, tools, service layer, and database are **reused entirely** when building the web UI. The web UI is just a new adapter on top.

### When to Switch to Web UI
- Agent reliably handles 5+ real transaction types correctly
- Clarification flow works (agent asks, user answers, agent proposes)
- Approval flow works (propose → approve/reject/comment cycle)
- GST computation is correct for both intra/inter-state
- All tool calls are stable with no hallucinated parameters

---

## Application Architecture

```
┌─────────────────────────────────────────────────────┐
│  Phase 1: REPL (throwaway, for agent iteration)     │
│  Phase 2: Web UI (chat home + dashboard)            │
└─────────────────────────────────────────────────────┘

Layer 4 — Adapters
    cmd/repl/          REPL (Phase 1 only)
    internal/adapters/web/   Web handlers (Phase 2)
              ↓
Layer 3 — Application Service
    internal/app/      ApplicationService interface + impl
              ↓
Layer 2 — Domain Core
    internal/core/     Ledger, GSTService, InventoryService,
                       SalesService, PurchaseService
              ↓
Layer 1 — Infrastructure
    internal/db/       pgx connection pool
    internal/ai/       OpenAI GPT-4o agent (advisory, never writes DB)
```

**Forbidden imports** (same as parent project):
- Adapters must not import domain services directly — only via ApplicationService
- `internal/ai` must not import `internal/core` service types (only model types)
- No layer imports upward

---

## Non-Negotiable Rules (Inherited)

1. All writes inside explicit transactions
2. Debit must equal credit before COMMIT — enforced in service layer, not DB triggers
3. No ORM — raw SQL only (pgx/v5)
4. No DB triggers for accounting logic
5. AI is advisory only — agent returns a `Proposal`, which must pass `Validate()` before `Ledger.Commit()`
6. `shopspring/decimal` for all monetary values — never `float64`
7. All amounts stored as `BIGINT` paise (1 INR = 100 paise) in DB, converted for display

---

## Database Schema

### Core Accounting

```sql
-- accounts
CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('asset','liability','equity','income','expense')),
    parent_id BIGINT REFERENCES accounts(id),
    is_gst_related BOOLEAN NOT NULL DEFAULT FALSE,
    is_system BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- journal_entries
CREATE TABLE journal_entries (
    id BIGSERIAL PRIMARY KEY,
    entry_date DATE NOT NULL,
    reference_type TEXT NOT NULL,  -- 'PURCHASE_INVOICE','SALES_INVOICE','BULK_SALE','MANUAL'
    reference_id BIGINT,
    narration TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- journal_lines (immutable, append-only)
CREATE TABLE journal_lines (
    id BIGSERIAL PRIMARY KEY,
    journal_entry_id BIGINT NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    debit BIGINT NOT NULL DEFAULT 0 CHECK (debit >= 0),
    credit BIGINT NOT NULL DEFAULT 0 CHECK (credit >= 0),
    gst_component TEXT CHECK (gst_component IN ('cgst','sgst','igst','cess','none')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### Parties

```sql
-- suppliers
CREATE TABLE suppliers (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    gstin TEXT UNIQUE,
    state_code TEXT NOT NULL,  -- 2-digit GST state code e.g. '29' for Karnataka
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- customers
CREATE TABLE customers (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    gstin TEXT UNIQUE,         -- NULL for B2C walk-in
    state_code TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### Products & Inventory

```sql
-- products
CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    hsn_code TEXT,
    gst_rate_pct INT NOT NULL DEFAULT 0,  -- 0,5,12,18,28 (no decimals for standard rates)
    track_inventory BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- inventory (running balance per product, no batches initially)
CREATE TABLE inventory (
    product_id BIGINT PRIMARY KEY REFERENCES products(id),
    qty_on_hand BIGINT NOT NULL DEFAULT 0,
    weighted_avg_cost BIGINT NOT NULL DEFAULT 0,  -- paise per unit
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- inventory_movements
CREATE TABLE inventory_movements (
    id BIGSERIAL PRIMARY KEY,
    product_id BIGINT NOT NULL REFERENCES products(id),
    movement_type TEXT CHECK (movement_type IN ('purchase','sale','adjustment')),
    quantity BIGINT NOT NULL,    -- positive = in, negative = out
    rate BIGINT NOT NULL,        -- paise per unit at time of movement
    cogs_amount BIGINT,          -- populated on sale movements
    reference_type TEXT,
    reference_id BIGINT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### Business Documents

```sql
-- purchase_invoices
CREATE TABLE purchase_invoices (
    id BIGSERIAL PRIMARY KEY,
    invoice_number TEXT NOT NULL UNIQUE,  -- supplier's invoice number
    invoice_date DATE NOT NULL,
    supplier_id BIGINT NOT NULL REFERENCES suppliers(id),
    supply_type TEXT NOT NULL CHECK (supply_type IN ('intra','inter')),  -- determines CGST+SGST vs IGST
    total_taxable_value BIGINT NOT NULL,
    total_cgst BIGINT NOT NULL DEFAULT 0,
    total_sgst BIGINT NOT NULL DEFAULT 0,
    total_igst BIGINT NOT NULL DEFAULT 0,
    total_cess BIGINT NOT NULL DEFAULT 0,
    grand_total BIGINT NOT NULL,
    status TEXT CHECK (status IN ('draft','posted')) DEFAULT 'draft',
    narration TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- purchase_invoice_lines
CREATE TABLE purchase_invoice_lines (
    id BIGSERIAL PRIMARY KEY,
    purchase_invoice_id BIGINT NOT NULL REFERENCES purchase_invoices(id) ON DELETE CASCADE,
    product_id BIGINT REFERENCES products(id),
    description TEXT,           -- for non-product lines (freight, charges)
    quantity BIGINT NOT NULL DEFAULT 1,
    rate BIGINT NOT NULL,       -- paise per unit
    taxable_value BIGINT NOT NULL,
    gst_rate_pct INT NOT NULL DEFAULT 0,
    cgst_amount BIGINT NOT NULL DEFAULT 0,
    sgst_amount BIGINT NOT NULL DEFAULT 0,
    igst_amount BIGINT NOT NULL DEFAULT 0,
    cess_amount BIGINT NOT NULL DEFAULT 0,
    line_total BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- sales_entries (handles both individual invoice and bulk daily sales)
CREATE TABLE sales_entries (
    id BIGSERIAL PRIMARY KEY,
    sale_type TEXT CHECK (sale_type IN ('invoice','bulk')) NOT NULL,
    sale_date DATE NOT NULL,
    invoice_number TEXT,        -- NULL for bulk sales
    customer_id BIGINT REFERENCES customers(id),  -- NULL for bulk/B2C
    supply_type TEXT NOT NULL CHECK (supply_type IN ('intra','inter')),
    total_taxable_value BIGINT NOT NULL,
    total_cgst BIGINT NOT NULL DEFAULT 0,
    total_sgst BIGINT NOT NULL DEFAULT 0,
    total_igst BIGINT NOT NULL DEFAULT 0,
    total_cess BIGINT NOT NULL DEFAULT 0,
    grand_total BIGINT NOT NULL,
    status TEXT CHECK (status IN ('draft','posted')) DEFAULT 'draft',
    narration TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- sales_entry_lines (for individual invoice sales with line detail)
CREATE TABLE sales_entry_lines (
    id BIGSERIAL PRIMARY KEY,
    sales_entry_id BIGINT NOT NULL REFERENCES sales_entries(id) ON DELETE CASCADE,
    product_id BIGINT REFERENCES products(id),
    description TEXT,
    quantity BIGINT NOT NULL DEFAULT 1,
    rate BIGINT NOT NULL,
    taxable_value BIGINT NOT NULL,
    gst_rate_pct INT NOT NULL DEFAULT 0,
    cgst_amount BIGINT NOT NULL DEFAULT 0,
    sgst_amount BIGINT NOT NULL DEFAULT 0,
    igst_amount BIGINT NOT NULL DEFAULT 0,
    cess_amount BIGINT NOT NULL DEFAULT 0,
    cogs_amount BIGINT NOT NULL DEFAULT 0,  -- computed from weighted avg at time of sale
    line_total BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### GST & Agent Support

```sql
-- agent_sessions (conversation context for multi-turn clarification)
CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY,           -- UUID
    state JSONB NOT NULL DEFAULT '{}',
    pending_proposal JSONB,
    status TEXT CHECK (status IN ('active','approved','rejected','expired')) DEFAULT 'active',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMP NOT NULL DEFAULT NOW() + INTERVAL '30 minutes'
);

-- agent_logs
CREATE TABLE agent_logs (
    id BIGSERIAL PRIMARY KEY,
    session_id TEXT REFERENCES agent_sessions(id),
    action_type TEXT,              -- 'propose','approve','reject','clarify'
    input_payload JSONB,
    output_payload JSONB,
    tokens_used INT,
    status TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- users
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT CHECK (role IN ('admin','accountant','viewer')),
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

---

## Chart of Accounts (Seed Data)

```
ASSET
  1000  Cash in Hand
  1010  Bank Account (Current)
  1100  Accounts Receivable (Trade Debtors)
  1200  Inventory
  1310  GST Input Credit - CGST
  1311  GST Input Credit - SGST
  1312  GST Input Credit - IGST
  1313  GST Input Credit - CESS

LIABILITY
  2000  Accounts Payable (Trade Creditors)
  2100  GST Payable - CGST
  2101  GST Payable - SGST
  2102  GST Payable - IGST
  2103  GST Payable - CESS

EQUITY
  3000  Owner's Capital
  3100  Retained Earnings

INCOME
  4000  Sales Revenue
  4100  Other Income

EXPENSE
  5000  Cost of Goods Sold
  5100  Rent
  5200  Salaries
  5300  Freight & Cartage
  5400  Other Operating Expenses
```

---

## GST Engine (Service Layer, Not AI)

The AI agent must NOT compute GST amounts. GST is deterministic — it must be computed in the service layer.

### Key GST Rules

```go
// GSTComputation is deterministic — called by service layer, not AI
type GSTComputation struct {
    TaxableValue  decimal.Decimal
    GSTRatePct    int
    SupplyType    string  // "intra" or "inter"
}

func ComputeGST(c GSTComputation) GSTAmounts {
    gst := c.TaxableValue.Mul(decimal.NewFromInt(int64(c.GSTRatePct))).Div(decimal.NewFromInt(100))
    if c.SupplyType == "intra" {
        half := gst.Div(decimal.NewFromInt(2))
        return GSTAmounts{CGST: half, SGST: half}
    }
    return GSTAmounts{IGST: gst}
}
```

### Intra vs Inter State Determination

```go
// Determined by comparing supplier/customer state_code with business state_code
// Business state code is a config variable (set once for the trader)
func IsInterState(partyStateCode, businessStateCode string) bool {
    return partyStateCode != businessStateCode
}
```

### Journal Entry for Purchase Invoice (Intra-State)

```
DR  Inventory (taxable value)
DR  GST Input CGST (cgst amount)
DR  GST Input SGST (sgst amount)
    CR  Accounts Payable (grand total)
```

### Journal Entry for Sales Invoice (Intra-State, with COGS)

```
DR  Accounts Receivable (grand total)
    CR  Sales Revenue (taxable value)
    CR  GST Payable CGST (cgst amount)
    CR  GST Payable SGST (sgst amount)
DR  Cost of Goods Sold (cogs amount)
    CR  Inventory (cogs amount)
```

---

## AI Agent Design

### Agent Role

The agent's job is **intent extraction and data completion**, not computation. It:
1. Parses natural language input into a structured transaction intent
2. Calls read tools to fill in missing data (supplier GSTIN, product GST rate, state codes)
3. Asks clarification questions if data is missing or ambiguous
4. Returns a structured proposal (amounts pre-filled, GST NOT computed by AI)
5. The service layer validates and computes GST deterministically

### Tool Set

**Read tools (autonomous, no approval needed):**

| Tool | Purpose |
|------|---------|
| `search_suppliers` | Find supplier by name/GSTIN |
| `search_customers` | Find customer by name/GSTIN |
| `search_products` | Find product by name/HSN code |
| `get_product_gst_rate` | Get GST rate and HSN for a product |
| `get_account_balance` | Check current balance |
| `get_inventory_level` | Current stock for a product |
| `get_gstr1_summary` | GSTR-1 data for a period |
| `get_gstr3b_summary` | GSTR-3B net GST liability |

**Write tools (require human approval):**

| Tool | Purpose |
|------|---------|
| `create_purchase_invoice` | Record a supplier invoice |
| `create_sales_invoice` | Record an individual sale |
| `create_bulk_sales_entry` | Record daily bulk/counter sales |
| `create_manual_journal` | Freeform journal entry |
| `create_supplier` | Add new supplier |
| `create_customer` | Add new customer |
| `create_product` | Add new product |

### Agent Prompt Strategy

The system prompt must explicitly state:
- "You are an accounting assistant for an Indian GST-registered trader."
- "NEVER compute GST amounts yourself. Extract taxable value and GST rate; the system will compute."
- "Always determine if the transaction is intra-state or inter-state before proposing."
- "If supplier/customer state is unknown, ask before proposing."
- "For bulk daily sales: assume intra-state unless told otherwise."
- "Propose entries in `Approve / Reject / Add Comments` format."

### Clarification Pattern

```
User:    "Received 200 Paracetamol strips from Cipla, invoice no CI-2024-881"
Agent:   [calls search_suppliers "Cipla"] → found, state_code=07 (Delhi)
         [calls search_products "Paracetamol"] → found, gst_rate=12%, HSN=3004
         "Your business is in Karnataka (state 29) and Cipla is in Delhi (state 07).
          This is an INTER-STATE purchase — IGST will apply.
          Missing: quantity per strip rate. What was the price per strip?"
User:    "₹4.20 per strip"
Agent:   Proposes complete entry with IGST computed by service layer
```

### Multi-Turn Session Management

- Each chat session has a UUID stored in browser `sessionStorage`
- Session state kept in `agent_sessions` table (not in memory — survives server restart)
- Pending proposals stored in session; web UI shows confirm/reject buttons
- Sessions expire after 30 minutes of inactivity

---

## Implementation Phases

### Phase 0 — Foundation (REPL) [Start Here]

**Goal**: Working AI agent that can propose GST-correct purchase and sales entries via REPL.

**Deliverables:**
- Database schema (migrations 001-003)
- Chart of accounts seed data
- `internal/core/ledger.go` — double-entry commit with balance enforcement
- `internal/core/gst_service.go` — deterministic GST computation
- `internal/ai/agent.go` — OpenAI Responses API, tool calling, clarification loop
- `internal/ai/tools.go` — ToolRegistry with 4 read tools + 2 write tools
- `internal/app/service.go` — ApplicationService interface
- `cmd/repl/main.go` — simple REPL (< 300 lines)

**Exit criteria:**
- Agent correctly handles: purchase invoice (intra-state), purchase invoice (inter-state), individual sales invoice, bulk daily sales
- Agent asks clarification when supplier state or product GST rate is missing
- Balance enforcement rejects unbalanced entries
- GST amounts always computed in service layer, never by AI

**Testing approach:**
- Write real transaction descriptions, run in REPL, verify proposed entries manually
- Deliberately give incomplete inputs to test clarification flow
- Test with both intra and inter-state transactions

---

### Phase 1 — Inventory Engine

**Goal**: Weighted average COGS on sales.

**Deliverables:**
- `internal/core/inventory_service.go`
- `inventory` and `inventory_movements` tables
- `ReceiveStock` (called on purchase invoice post)
- `IssueStock` (called on sales invoice post, returns COGS)
- Weighted average recomputed on each purchase receipt

**GST note**: Inventory value is taxable value only (GST input credit is separate).

---

### Phase 2 — Supplier & Customer Master

**Goal**: Persistent party data with GST state management.

**Deliverables:**
- `internal/core/supplier_service.go`, `customer_service.go`
- pg_trgm GIN indexes for fuzzy search
- AI tools: `search_suppliers`, `search_customers`, `create_supplier`, `create_customer`
- REPL commands: `/suppliers`, `/customers`

**GST note**: `state_code` on supplier/customer is critical — agent uses it for intra/inter determination.

---

### Phase 3 — GST Reporting

**Goal**: GSTR-1 and GSTR-3B summary queries.

**Deliverables:**
- `internal/core/gst_reporting_service.go`
- GSTR-1 summary (sales by GST rate category, B2B vs B2C)
- GSTR-3B summary (output tax - input credit = net payable)
- Trial balance, P&L queries
- AI tools: `get_gstr1_summary`, `get_gstr3b_summary`

---

### Phase 4 — Web UI

**Goal**: Full web application (chat home + dashboard).

**Deliverables:**
- `cmd/server/main.go` — web server
- `internal/adapters/web/` — handlers, middleware, SSE chat
- Templates (templ): login, chat home (full-screen), dashboard
- Auth: JWT HS256 httpOnly cookie
- SSE streaming for AI responses
- Confirm/reject action cards for write tool proposals
- Dashboard: trial balance, P&L, GSTR-1, GSTR-3B

**Key pages:**
```
GET  /            → Chat home (AI agent, full screen)
GET  /dashboard   → Reports and lists
GET  /reports/trial-balance
GET  /reports/pl
GET  /reports/gstr1
GET  /reports/gstr3b
POST /chat        → SSE stream
POST /chat/confirm → Execute approved write tool
POST /chat/clear
```

---

### Phase 5 — Document Upload

**Goal**: Agent can read purchase invoices from images.

**Deliverables:**
- `POST /chat/upload` — accept JPG/PNG/PDF
- Pass image to OpenAI vision as part of agent message
- Agent extracts: supplier name, invoice number, date, line items, GST amounts
- Agent proposes purchase invoice entry from extracted data
- Human verifies and approves

---

## Future: Tally Sync

Design notes for when this becomes relevant:

```sql
-- staging_transactions (outbound queue for Tally/legacy ERP)
CREATE TABLE staging_transactions (
    id BIGSERIAL PRIMARY KEY,
    source_type TEXT,           -- 'purchase_invoice', 'sales_entry', 'journal_entry'
    source_id BIGINT,
    tally_voucher_type TEXT,    -- 'Purchase', 'Sales', 'Journal'
    payload JSONB NOT NULL,     -- Tally XML/JSON equivalent
    status TEXT CHECK (status IN ('pending','synced','failed')) DEFAULT 'pending',
    synced_at TIMESTAMP,
    error_message TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

Sync strategy:
- This app is the **master** — Tally is the legacy output destination
- Sync is one-way: this app → staging → Tally import
- Tally XML format is well-documented and stable
- A separate sync service (can be a simple Go cron) reads `staging_transactions` and pushes to Tally via its ODBC/XML import

---

## Technology Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Same as parent project, single binary |
| Database | PostgreSQL 14+ | pgx/v5, no ORM |
| HTTP | `net/http` + chi | Lightweight |
| Templates | `github.com/a-h/templ` | Type-safe, same as parent |
| CSS | Tailwind v4 + `tailwindcss.exe` | Same as parent, no Node.js |
| JS | HTMX + Alpine.js | Same as parent, vendored |
| AI | OpenAI GPT-4o via Responses API | `openai-go` SDK, strict JSON schema |
| Decimal | `shopspring/decimal` | No float64 for money |
| Auth | JWT HS256 httpOnly cookie | Same as parent |

---

## Key Lessons From This Project

1. **REPL first always pays off.** 300 lines of throwaway REPL saved weeks on web UI iterations.

2. **AI tools > AI prompts.** Structured tool calling (search_suppliers, get_product_gst_rate) is more reliable than asking the AI to "figure it out" from its training data. Always ground the agent with real database lookups.

3. **Deterministic GST, not AI GST.** Never ask the AI to compute tax amounts. It will hallucinate rates. Extract the taxable value and rate; compute in Go.

4. **State code determines everything.** Inter vs intra-state is a binary decision with huge GST consequences. Get supplier and customer state codes from DB, not from the AI.

5. **Write tool approval loop.** Read tools run autonomously; write tools always require human confirmation. This is the key trust pattern for accounting AI. Do not deviate.

6. **Clarification limit.** Cap clarification rounds at 3 (or the agent loops forever on ambiguous input). After 3 rounds, output a structured error: "Could not process — please provide invoice details directly."

7. **Amount storage.** Store all amounts as BIGINT paise. Display as INR with `decimal.Div(100)`. Never mix paise and rupees.

8. **Session state in DB.** Store conversation context in `agent_sessions` table, not in server memory. This makes the server stateless and restartable without losing pending proposals.

9. **The balance check is sacred.** `SUM(debit) == SUM(credit)` before every commit. This check has caught real bugs in AI proposals multiple times.

10. **pg_trgm for fuzzy search.** Traders know supplier names approximately ("Cipla" vs "Cipla Ltd" vs "Cipla Limited"). pg_trgm similarity search is essential for the AI agent to reliably resolve party names from DB.

---

## Minimal Viable REPL (Phase 0 Target)

```
$ ./app
GST Accounting Agent. Type a transaction or /help.
> Received purchase from Sun Pharma, invoice SP-2024-1234 dated today,
  100 Digene strips at ₹6 each, 12% GST, our state is Karnataka

[Searching suppliers... Sun Pharma found, state: Maharashtra (27) - INTER-STATE]
[Searching products... Digene found, HSN: 3004, GST rate: 12%]
[Computing: taxable=₹600, IGST=₹72 (12%), total=₹672]

Proposed Journal Entry — Purchase Invoice SP-2024-1234
Date: 2024-01-15
─────────────────────────────────────────────────────
DR  Inventory              ₹600.00
DR  GST Input IGST          ₹72.00
    CR  Accounts Payable         ₹672.00
─────────────────────────────────────────────────────
Total Debit: ₹672.00 | Total Credit: ₹672.00 ✓

Approve? [Y/N/C (comment)]: Y
✓ Posted — Journal Entry #1, Purchase Invoice #1
```

---

## Project Structure

```
gst-accounting/
├── cmd/
│   ├── repl/main.go          # Phase 0 REPL (< 300 lines)
│   ├── server/main.go        # Phase 4 web server
│   └── verify-db/main.go     # Migration runner
├── internal/
│   ├── ai/
│   │   ├── agent.go          # OpenAI Responses API + agentic loop
│   │   └── tools.go          # ToolRegistry
│   ├── app/
│   │   ├── service.go        # ApplicationService interface
│   │   ├── app_service.go    # Implementation
│   │   └── types.go          # Result/Request types
│   ├── adapters/
│   │   └── web/              # Phase 4 (handlers, middleware, SSE)
│   ├── core/
│   │   ├── ledger.go         # Double-entry commit
│   │   ├── gst_service.go    # GST computation (deterministic)
│   │   ├── inventory_service.go
│   │   ├── purchase_service.go
│   │   ├── sales_service.go
│   │   ├── supplier_service.go
│   │   ├── customer_service.go
│   │   └── reporting_service.go
│   └── db/
│       └── db.go             # pgx pool
├── migrations/
│   ├── 001_init.sql          # Core schema
│   ├── 002_accounts_seed.sql
│   └── 003_products_seed.sql
├── web/
│   ├── templates/
│   │   ├── layouts/
│   │   └── pages/
│   └── static/
│       └── js/               # HTMX, Alpine.js, Chart.js (vendored)
├── CLAUDE.md
├── Makefile
└── .env
```

---

## Makefile (Phase 0 Minimal)

```makefile
.PHONY: db repl build test

db:
	go run ./cmd/verify-db

repl:
	go run ./cmd/repl

build:
	go build -o app.exe ./cmd/repl

test:
	go test ./internal/core -v
```

---

## Summary

This is fundamentally **the same architecture as this project** but scoped differently:

| This project | GST App |
|-------------|---------|
| Multi-domain (orders, inventory, vendors, PO) | GST-first (purchase, sales, reporting) |
| REPL + Web UI | REPL (Phase 0) → Web UI only |
| General accounting | India GST compliance |
| 24 migrations, 70 tests | Start lean: 5 migrations, 20 tests |
| SAP-style complex | Simple trader accounting |
| Tally sync: deferred | Tally sync: planned |

**Build the agent right first (REPL). Then wrap it in a web UI. The agent is the product.**

---

## AI Agent & REPL — Code Architecture Reference

> This section is a precise mapping from the existing accounting-agent codebase to the new GST app.
> For each source file: what to copy verbatim, what to change, what to drop, and what to add.

---

### Architectural Simplification: Drop `InterpretEvent`

The biggest difference from the parent project: **the new app has no `InterpretEvent` path**.

In the parent project, the agent loop has two paths:
```
InterpretDomainAction → (1) Answer / Clarification / Proposed write tool
                      → (2) JournalEntry → InterpretEvent → structured Proposal → CommitProposal
```

Path (2) was built for flexible, open-ended journal entries using strict JSON schema structured output. It is powerful but complex — it requires a separate schema, a separate OpenAI call, a `Proposal` struct, and `CommitProposal` logic.

In the new GST app, **all transactions go through typed write tools**:

| Transaction | Write tool |
|-------------|-----------|
| Supplier purchase invoice | `create_purchase_invoice` |
| Individual sales invoice | `create_sales_invoice` |
| Daily bulk counter sales | `create_bulk_sales_entry` |
| Owner's capital, expenses, manual | `create_journal_entry` |
| New supplier | `create_supplier` |
| New customer | `create_customer` |
| New product | `create_product` |

The service layer handler for each write tool: validates inputs → computes GST deterministically → commits the transaction atomically. No unstructured AI-proposed journal lines ever reach the database.

This collapses the routing to three outcomes:
```
InterpretDomainAction → Answer         (read query answered)
                      → Clarification  (more info needed)
                      → Proposed       (write tool awaiting human confirm)
```

No `JournalEntry` kind. No `InterpretEvent`. No `Proposal` struct. No `CommitProposal`.

**The REPL `domainLoop` therefore has no `journalLoop` inside it.** Simpler, more correct.

---

### `internal/ai/tools.go` — Copy Verbatim

No changes needed. `ToolRegistry`, `ToolDefinition`, `ToolHandler`, `Attachment` are fully generic.

```
Source: internal/ai/tools.go  (82 lines)
Target: internal/ai/tools.go  → copy as-is
```

The only module path change: `accounting-agent/internal/...` → `gst-accounting/internal/...` (or whatever the new module name is). This applies to all files.

---

### `internal/ai/agent.go` — Copy, Strip `InterpretEvent`, Rewrite System Prompt

**Drop completely:**
- `InterpretEvent()` method and its entire body (lines 84–196)
- `generateSchema()` function (lines 203–239)
- `proposalSchema()` function (lines 464–539)
- `AgentDomainResultKindJournalEntry` constant
- The `EventDescription` field on `AgentDomainResult`
- The `route_to_journal_entry` meta-tool registration (lines 296–313)

**Keep verbatim:**
- `AgentDomainResultKind` type and the three remaining constants: `Answer`, `Clarification`, `Proposed`
- `AgentDomainResult` struct (minus `EventDescription`)
- `AgentService` interface (simplified — no `InterpretEvent`)
- `Agent` struct and `NewAgent()`
- `InterpretDomainAction()` — entire agentic loop (lines 249–461), only the system prompt changes

**Rewrite the system prompt** in `InterpretDomainAction` (currently lines 253–269):

```go
// Current (parent project):
systemPrompt := fmt.Sprintf(`You are an expert business assistant for %s (%s, base currency: %s).
...
2. If the user is describing a financial accounting event: call route_to_journal_entry...
...`)

// New (GST app):
systemPrompt := fmt.Sprintf(`You are an accounting assistant for a GST-registered Indian trader (%s, state: %s).
Your job: help record purchase invoices, sales, and other transactions with correct GST entries.

CRITICAL GST RULES:
1. NEVER compute GST amounts yourself. Extract the taxable value and GST rate — the system computes CGST/SGST/IGST.
2. Intra-state supply (supplier/customer in same state as business): CGST + SGST each at half the GST rate.
3. Inter-state supply (different states): IGST at full GST rate. No CGST or SGST.
4. Always verify supplier or customer state via search tools before proposing any transaction.
5. Common GST rates in India: 0%%, 5%%, 12%%, 18%%, 28%% (plus cess for some goods).

ROUTING:
- User asks a question (balances, stock, GST report, supplier info): call read tools and answer.
- User describes a purchase or sale transaction: call create_purchase_invoice or create_sales_invoice.
- User describes daily counter/cash sales without invoice: call create_bulk_sales_entry.
- User describes an expense, capital contribution, or adjustment: call create_journal_entry.
- User wants to add a new supplier/customer/product: call the relevant create tool.
- Information is missing to complete a transaction: call request_clarification with a specific question.

TOOL USAGE:
- Always call search_suppliers or search_customers before recording a transaction to get state_code and GSTIN.
- Always call search_products before recording a transaction to get gst_rate_pct and hsn_code.
- Do not guess state codes or GST rates — always look them up.
- After gathering context, propose the write tool with all required parameters filled in.

Business state code: %s  (used to determine intra vs inter-state)
Today's date: %s`,
    company.Name, company.StateCode, company.StateCode, time.Now().Format("2006-01-02"))
```

**Remove the `route_to_journal_entry` meta-tool** from the tools list. Keep only `request_clarification`.

**The loop itself (lines 345–461) is unchanged.** The only change is removing the `route_to_journal_entry` branch in the `fc.Name` check (line 387).

**New field on Company model**: `StateCode string` (the 2-digit GST state code, e.g. "29" for Karnataka). This is stored in the `companies` table or in a config table for the single-tenant deployment.

---

### `internal/adapters/repl/repl.go` — Copy, Strip Domain Commands, Add GST Commands

**The core REPL loop structure is copied verbatim** — this is the best-tested, most hardened part of the codebase. Specifically keep:

1. **The dispatch pattern** — `strings.HasPrefix(input, "/")` routes to deterministic commands, everything else goes to AI
2. **The `domainLoop`** — rounds capped at 3, four outcome kinds handled
3. **The clarification escape** — if user types `/command` during clarification, cancel AI flow and dispatch
4. **The `case app.DomainActionKindProposed` handler** — show tool name + args, prompt y/n

**Drop from slash commands** (not applicable to GST app):
- `/orders`, `/new-order`, `/confirm`, `/ship`, `/invoice`, `/payment` — no order management
- `/warehouses`, `/stock`, `/receive` — no warehouse system
- `/refresh` — materialized views are an optimization, not needed in Phase 0

**Drop from domain loop** (simplified architecture):
- `case app.DomainActionKindJournalEntry` — does not exist in new app
- The entire `journalLoop` — no `InterpretEvent` path
- `CommitProposal` call — write tools handle their own commit

**Add GST-specific slash commands:**
```
/suppliers              List all suppliers with GSTIN and state code
/customers              List all customers with GSTIN and state code
/products               List products with HSN code and GST rate
/invoices [from] [to]   List purchase invoices in date range
/sales [from] [to]      List sales entries in date range
/gstr1 [YYYY-MM]        GSTR-1 summary for a period
/gstr3b [YYYY-MM]       GSTR-3B net GST payable
/bal                    Trial balance
/pl [year] [month]      Profit & Loss
/bs [date]              Balance Sheet
```

**The `case app.DomainActionKindProposed` handler changes** in one important way: when the user confirms (`y`), it calls `svc.ExecuteWriteTool(ctx, companyCode, result.ToolName, result.ToolArgs)` and displays the result. In the parent project, write tool execution was partially deferred — here it is wired from Phase 0 because write tools ARE the transaction posting mechanism.

```go
// In the REPL, after user types 'y':
result, err := svc.ExecuteWriteTool(ctx, companyCode, domainResult.ToolName, domainResult.ToolArgs)
if err != nil {
    fmt.Printf("Error: %v\n", err)
} else {
    fmt.Printf("Done: %s\n", result)  // result is JSON from the tool handler
}
```

**The Add Comments option** (from the prompt spec: Yes/No/Add Comments): implement as a 4th REPL input option:

```go
fmt.Print("\nApprove? (y / n / c — add comment): ")
choice, _ := reader.ReadString('\n')
choice = strings.TrimSpace(strings.ToLower(choice))
switch {
case choice == "y" || choice == "yes":
    // execute write tool
case choice == "c" || choice == "comment":
    fmt.Print("Comment: ")
    comment, _ := reader.ReadString('\n')
    // re-run InterpretDomainAction with appended comment
    accumulatedInput = fmt.Sprintf("%s\nUser correction: %s", accumulatedInput, strings.TrimSpace(comment))
    // continue domainLoop
default:
    fmt.Println("Cancelled.")
}
```

---

### `internal/adapters/repl/display.go` — Mostly New (GST Columns)

**Keep the pattern** (`printBalances`, `printPL`, `printBS`) — same columnar format, copy structure.

**Drop** (not applicable): `printOrders`, `printOrderDetail`, `printWarehouses`, `printStockLevels`, `printProposal`, `printStatement`.

**Add GST-specific display functions:**

```go
// printPurchaseInvoices — shows date, supplier, invoice#, taxable, CGST, SGST, IGST, total, status
// printSalesEntries    — shows date, type (invoice/bulk), customer, taxable, GST, total, status
// printSuppliers       — shows name, GSTIN, state_code
// printCustomers       — shows name, GSTIN, state_code
// printProducts        — shows name, HSN code, GST rate%
// printGSTR1Summary    — taxable value, CGST, SGST, IGST, cess for the period
// printGSTR3B          — output tax, input credit, net payable
```

---

### `internal/adapters/repl/wizards.go` — Drop Entirely

Wizards (`handleNewOrder`) are order-management code. The GST app has no wizard-style slash commands — the AI agent handles all data entry interactively via the clarification flow. No `wizards.go` in the new app.

---

### `cmd/app/main.go` (REPL entry point) — Copy, Simplify Wiring

The parent project's `main.go` wires 9 services. The new app starts lean:

```go
// Phase 0 wiring (cmd/repl/main.go):
pool         := db.NewPool(ctx)
ledger       := core.NewLedger(pool)
gstService   := core.NewGSTService()           // stateless, no DB
purchaseSvc  := core.NewPurchaseService(pool, ledger, gstService)
salesSvc     := core.NewSalesService(pool, ledger, gstService)
reportingSvc := core.NewReportingService(pool)
agent        := ai.NewAgent(apiKey)
svc          := app.NewAppService(pool, ledger, purchaseSvc, salesSvc, reportingSvc, agent)
```

No `docService` (no gapless document numbers in Phase 0 — use simple BIGSERIAL).
No `ruleEngine` (no account_rules table — accounts are hardcoded in GSTService).
No `orderService`, `vendorService`, `purchaseOrderService`.

Add these only when Phase 2+ domains are built.

---

### `internal/app/service.go` — Copy Interface Pattern, Different Methods

The `ApplicationService` interface structure is copied exactly — but the methods reflect the GST domain:

```go
type ApplicationService interface {
    // Read
    GetTrialBalance(ctx context.Context) (*TrialBalanceResult, error)
    GetProfitAndLoss(ctx context.Context, year, month int) (*PLResult, error)
    GetBalanceSheet(ctx context.Context, asOfDate string) (*BSResult, error)
    ListSuppliers(ctx context.Context) (*SuppliersResult, error)
    ListCustomers(ctx context.Context) (*CustomersResult, error)
    ListProducts(ctx context.Context) (*ProductsResult, error)
    ListPurchaseInvoices(ctx context.Context, from, to string) (*PurchaseInvoicesResult, error)
    ListSalesEntries(ctx context.Context, from, to string) (*SalesEntriesResult, error)
    GetGSTR1Summary(ctx context.Context, year, month int) (*GSTR1Result, error)
    GetGSTR3BSummary(ctx context.Context, year, month int) (*GSTR3BResult, error)

    // Write (called by ExecuteWriteTool after human approval)
    PostPurchaseInvoice(ctx context.Context, req PurchaseInvoiceRequest) (*PurchaseInvoiceResult, error)
    PostSalesInvoice(ctx context.Context, req SalesInvoiceRequest) (*SalesInvoiceResult, error)
    PostBulkSalesEntry(ctx context.Context, req BulkSalesRequest) (*SalesEntryResult, error)
    PostManualJournalEntry(ctx context.Context, req ManualJournalRequest) error
    CreateSupplier(ctx context.Context, req CreateSupplierRequest) (*SupplierResult, error)
    CreateCustomer(ctx context.Context, req CreateCustomerRequest) (*CustomerResult, error)
    CreateProduct(ctx context.Context, req CreateProductRequest) (*ProductResult, error)

    // AI
    InterpretDomainAction(ctx context.Context, text string, attachments ...Attachment) (*DomainActionResult, error)
    ExecuteWriteTool(ctx context.Context, toolName string, args map[string]any) (string, error)

    // Auth
    AuthenticateUser(ctx context.Context, username, password string) (*UserSession, error)

    // Meta
    LoadCompany(ctx context.Context) (*Company, error)
}
```

Note: No `companyCode` parameter on most methods — single-tenant, the company is loaded once at startup.

---

### `internal/app/app_service.go` — `buildToolRegistry()` Pattern Copied, GST Tools

The `buildToolRegistry()` method is the heart of the agent integration. The pattern is:

```go
func (s *appService) buildToolRegistry(ctx context.Context) *ai.ToolRegistry {
    registry := ai.NewToolRegistry()

    // ── READ TOOLS (execute autonomously) ──

    registry.Register(ai.ToolDefinition{
        Name:        "search_suppliers",
        Description: "Search suppliers by name or GSTIN. Returns name, GSTIN, state_code.",
        IsReadTool:  true,
        InputSchema: stringQuerySchema("Supplier name or GSTIN to search for."),
        Handler: func(ctx context.Context, params map[string]any) (string, error) {
            query, _ := params["query"].(string)
            return s.searchSuppliersJSON(ctx, query)
        },
    })

    registry.Register(ai.ToolDefinition{
        Name:        "search_customers",
        Description: "Search customers by name or GSTIN. Returns name, GSTIN, state_code.",
        IsReadTool:  true,
        InputSchema: stringQuerySchema("Customer name or GSTIN to search for."),
        Handler: func(ctx context.Context, params map[string]any) (string, error) {
            query, _ := params["query"].(string)
            return s.searchCustomersJSON(ctx, query)
        },
    })

    registry.Register(ai.ToolDefinition{
        Name:        "search_products",
        Description: "Search products by name or HSN code. Returns name, HSN code, gst_rate_pct.",
        IsReadTool:  true,
        InputSchema: stringQuerySchema("Product name or HSN code to search for."),
        Handler: func(ctx context.Context, params map[string]any) (string, error) {
            query, _ := params["query"].(string)
            return s.searchProductsJSON(ctx, query)
        },
    })

    registry.Register(ai.ToolDefinition{
        Name:        "get_account_balance",
        Description: "Get current balance for an account code.",
        IsReadTool:  true,
        InputSchema: accountCodeSchema(),
        Handler: func(ctx context.Context, params map[string]any) (string, error) {
            code, _ := params["account_code"].(string)
            return s.getAccountBalanceJSON(ctx, code)
        },
    })

    registry.Register(ai.ToolDefinition{
        Name:        "get_gst_summary",
        Description: "Get GSTR-3B style summary: total output tax, input credit, and net GST payable for a month.",
        IsReadTool:  true,
        InputSchema: yearMonthSchema(),
        Handler: func(ctx context.Context, params map[string]any) (string, error) {
            year := int(params["year"].(float64))
            month := int(params["month"].(float64))
            return s.getGSTSummaryJSON(ctx, year, month)
        },
    })

    // ── WRITE TOOLS (require human confirmation) ──

    registry.Register(ai.ToolDefinition{
        Name:        "create_purchase_invoice",
        Description: "Record a purchase invoice from a supplier. The system will compute CGST/SGST/IGST based on supply type and post the journal entry.",
        IsReadTool:  false,
        InputSchema: purchaseInvoiceSchema(),
        Handler:     nil, // write tools have no handler — executed via ExecuteWriteTool after confirmation
    })

    registry.Register(ai.ToolDefinition{
        Name:        "create_sales_invoice",
        Description: "Record a sales invoice to a customer. The system will compute GST and COGS and post the journal entry.",
        IsReadTool:  false,
        InputSchema: salesInvoiceSchema(),
        Handler:     nil,
    })

    registry.Register(ai.ToolDefinition{
        Name:        "create_bulk_sales_entry",
        Description: "Record daily bulk/counter cash sales without individual invoices. Used for end-of-day totals.",
        IsReadTool:  false,
        InputSchema: bulkSalesSchema(),
        Handler:     nil,
    })

    registry.Register(ai.ToolDefinition{
        Name:        "create_journal_entry",
        Description: "Record a freeform double-entry journal entry for expenses, capital, adjustments.",
        IsReadTool:  false,
        InputSchema: manualJournalSchema(),
        Handler:     nil,
    })

    registry.Register(ai.ToolDefinition{
        Name:        "create_supplier",
        Description: "Add a new supplier with GSTIN and state code.",
        IsReadTool:  false,
        InputSchema: createSupplierSchema(),
        Handler:     nil,
    })

    registry.Register(ai.ToolDefinition{
        Name:        "create_customer",
        Description: "Add a new customer with optional GSTIN and state code.",
        IsReadTool:  false,
        InputSchema: createCustomerSchema(),
        Handler:     nil,
    })

    return registry
}
```

**`ExecuteWriteTool`** dispatches by tool name and calls the service layer:
```go
func (s *appService) ExecuteWriteTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
    switch toolName {
    case "create_purchase_invoice":
        req, err := parsePurchaseInvoiceArgs(args)
        if err != nil { return "", err }
        result, err := s.PostPurchaseInvoice(ctx, req)
        if err != nil { return "", err }
        return json.Marshal(result)
    case "create_sales_invoice":
        // ...
    case "create_bulk_sales_entry":
        // ...
    case "create_journal_entry":
        // ...
    case "create_supplier":
        // ...
    case "create_customer":
        // ...
    default:
        return "", fmt.Errorf("unknown write tool: %s", toolName)
    }
}
```

---

### Write Tool Input Schemas (Critical for Agent Reliability)

The schemas must be precise — the agent fills these from natural language. Vague schemas produce hallucinated parameters.

**`purchaseInvoiceSchema`** (what the agent fills in):
```go
map[string]any{
    "type": "object", "additionalProperties": false,
    "required": []string{"supplier_id", "invoice_number", "invoice_date", "supply_type", "lines"},
    "properties": map[string]any{
        "supplier_id":     {"type":"integer", "description":"Supplier ID from search_suppliers result."},
        "invoice_number":  {"type":"string",  "description":"Supplier's invoice number as printed."},
        "invoice_date":    {"type":"string",  "description":"Invoice date YYYY-MM-DD."},
        "supply_type":     {"type":"string",  "enum":[]string{"intra","inter"}, "description":"intra=same state (CGST+SGST), inter=different state (IGST)."},
        "lines": {
            "type":"array",
            "items": {
                "type":"object", "additionalProperties":false,
                "required":[]string{"product_id","quantity","rate_paise","gst_rate_pct"},
                "properties": map[string]any{
                    "product_id":   {"type":"integer", "description":"Product ID from search_products."},
                    "quantity":     {"type":"integer", "description":"Quantity purchased."},
                    "rate_paise":   {"type":"integer", "description":"Price per unit in paise (multiply rupees by 100). e.g. ₹8.50 = 850."},
                    "gst_rate_pct": {"type":"integer", "description":"GST rate as integer percentage: 0,5,12,18,28."},
                    "description":  {"type":"string",  "description":"Optional: free text for non-product lines."},
                },
            },
        },
        "narration": {"type":"string", "description":"Optional: note for the journal entry."},
    },
}
```

**Key design choices in the schemas:**
- `rate_paise` not `rate_rupees` — forces the agent to convert, surfacing any amount ambiguity
- `gst_rate_pct` as integer — prevents the agent from computing "12% of ₹600 = ₹72" (it must leave that to the service)
- `supply_type` as enum `["intra","inter"]` — binary, no ambiguity
- `supplier_id` as integer (from DB) — forces the agent to call `search_suppliers` first

---

### For the Web UI (Phase 4) — What to Copy from the Parent

**Copy verbatim:**
- `internal/adapters/web/middleware.go` — RequestID, Logger, Recoverer, CORS, RequestBodyLimit — zero changes needed
- `internal/adapters/web/ai.go` — chatHome, chatUpload, chatClear, startUploadCleanup — zero changes
- `internal/adapters/web/chat.go` — pendingStore, sendSSE, chatMessage, chatConfirm

**Changes to `chat.go`:**
- Remove `case app.DomainActionKindJournalEntry` in `chatMessage` — does not exist in new app
- Remove `pendingKindJournalEntry` and the `CommitProposal` branch in `chatConfirm`
- After removing journal entry path, `chatMessage` has only 3 SSE outcome cases: answer, clarification, action_card
- `chatConfirm` only has one case: `pendingKindWriteTool` → `ExecuteWriteTool`

**New in `chat.go` for GST app:**
- Add `Approve / Reject / Add Comments` pattern: the frontend sends action `"approve"`, `"reject"`, or `"comment"` (with a `comment_text` field). On `"comment"`, return a new SSE stream with the comment appended to the pending action context and re-run the agent.

**Copy the SSE event contract exactly:**
```
status      → {"status":"thinking"}
answer      → {"text":"..."}
clarification → {"question":"...","context":"..."}
action_card → {"token":"uuid","tool":"create_purchase_invoice","args":{...}}
error       → {"message":"...","code":"..."}
done        → {}
```

**The `action_card` event** is what the frontend shows as the approve/reject/comment card. The card must display the tool args in a human-readable GST format (supplier name, invoice number, taxable value, GST amounts computed from the args).

---

### GST-Specific Display in Action Cards (Web UI)

When the agent proposes `create_purchase_invoice`, the web UI must render:

```
─────────────────────────────────────────────────
Proposed: Purchase Invoice
─────────────────────────────────────────────────
Supplier:       Sun Pharma (state: Maharashtra, IGST)
Invoice No:     SP-2024-1234
Date:           2024-01-15
─────────────────────────────────────────────────
 # Product          Qty    Rate   Taxable   GST
 1 Digene 500mg     100  ₹6.00   ₹600.00   12%
─────────────────────────────────────────────────
Taxable Value:                    ₹600.00
IGST (12%):                        ₹72.00
Total:                            ₹672.00
─────────────────────────────────────────────────
Journal Entry (will be posted):
  DR  Inventory               ₹600.00
  DR  GST Input IGST           ₹72.00
      CR  Accounts Payable          ₹672.00
─────────────────────────────────────────────────
[Approve]  [Reject]  [Add Comment]
```

The journal entry preview is **computed in the frontend** from the tool args (using the same deterministic rules as the service layer) so the user sees exactly what will be posted before confirming.

---

### Agent Failure Modes to Design Around

Learned from operating the parent project:

| Failure | How to handle |
|---------|--------------|
| Agent calls `create_purchase_invoice` without calling `search_suppliers` first | System prompt must say "always search suppliers first". Enforce: if `supplier_id` is 0 or absent, `ExecuteWriteTool` returns an error asking the agent to search first |
| Agent guesses GST rate (e.g. assumes 18% for medicines which are actually 12%) | `search_products` tool must return the HSN code and configured rate from the DB. System prompt: "use the rate returned by search_products, not your training data" |
| Agent mixes CGST+SGST and IGST in the same invoice | `supply_type` enum forces a single choice. Service layer rejects if both are set |
| Agent puts rupees in `rate_paise` field (e.g. 8 instead of 800) | The action card shows the computed taxable value — the human reviewer will spot ₹8 for 100 strips at ₹8 = ₹800 total but agent put ₹8 total |
| Clarification loop runs more than 3 rounds | REPL caps at 3 rounds. Web UI tracks round count in sessionStorage and shows "too many clarifications" message |
| Agent produces `create_purchase_invoice` with wrong line count | Service layer validates: at least 1 line, taxable_value > 0, gst_rate_pct in [0,5,12,18,28] |

---

### The REPL → Web UI Migration Checklist

When Phase 0 REPL is working and Phase 4 web UI is ready to start:

- [ ] All tool handlers from `buildToolRegistry` work (same code, reused)
- [ ] `ExecuteWriteTool` switch statement works (same code, reused)
- [ ] `InterpretDomainAction` works (same code, reused)
- [ ] Copy `internal/adapters/web/middleware.go` verbatim
- [ ] Copy `internal/adapters/web/ai.go` verbatim
- [ ] Copy `internal/adapters/web/chat.go`, remove journal entry path
- [ ] Add auth: JWT middleware from parent project `internal/adapters/web/auth.go`
- [ ] Build `cmd/server/main.go` (new, ~60 lines of wiring)
- [ ] Add `web/templates/` (login, chat_home, dashboard, report pages)
- [ ] Vendor HTMX, Alpine.js, Chart.js into `web/static/js/`

The agent and service layer require **zero changes** for the web migration.
