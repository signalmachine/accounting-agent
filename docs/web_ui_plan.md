# Web UI Implementation Plan

> **Purpose**: Defines the strategy, architecture, and phase-by-phase plan for building the web interface as the primary user-facing product.
> **Last Updated**: 2026-02-25
> **Status**: Approved direction — web UI is the primary interface for all business users.

**This document supersedes Phase 32 of `docs/Implementation_plan_upgrade.md`.** The REST API and web UI are not a Tier 5 afterthought — they are foundational infrastructure built immediately after the rule engine is wired in (Phase 7).

---

## 1. Strategic Direction

### 1.1 Interface Ownership

| Interface | Role | Target User |
|---|---|---|
| **Web UI** | Primary product interface | Business owners, accountants, warehouse staff, operations |
| **CLI** | Automation, ops, JSON pipeline scripting | Developers, DevOps, batch processes |
| **REPL** | **Deprecated** — transitional only, no new commands | Developer testing during transition period |

The REPL proved the application works and served well during development. It is not a viable interface for the stated target users (small business owners, non-accountant operators) and will be phased out as web UI coverage reaches parity with REPL functionality. No new REPL slash commands will be added after Phase 7.

The CLI retains a stable, narrow scope: `propose`, `validate`, `commit`, and `balances` — sufficient for JSON pipeline automation, monitoring scripts, and CI/CD hooks. No new CLI commands beyond these four.

### 1.2 Design Principles

- **Web UI is a thin adapter.** All business logic stays in `ApplicationService` and domain services. HTTP handlers parse requests, call `ApplicationService`, return JSON. No business logic in handlers.
- **AI is woven in, not bolted on.** The AI chat panel and inline compliance warnings are first-class web UI elements from Phase WF3 onwards — not a feature added at the end.
- **Progressive enhancement.** Core operations (trial balance, orders, invoices) work fully without AI. AI adds efficiency and guidance but is never the only path to an operation.
- **Mobile-aware.** The UI uses responsive layout. Warehouse staff and field operations may use it on tablets or phones.
- **Advisory-only AI is unchanged.** Every AI-proposed action in the web UI requires explicit human confirmation before any `ApplicationService` write call. The web confirmation modal is the equivalent of the REPL's `[y/n]` prompt.

---

## 2. Technology Stack

### 2.1 Backend API

| Component | Choice | Rationale |
|---|---|---|
| HTTP router | `chi` v5 | Lightweight, idiomatic Go, no magic, middleware composable |
| API style | REST + JSON | Maps cleanly to `ApplicationService` methods, well-understood |
| Auth tokens | JWT in httpOnly cookie | No localStorage exposure; survives page refresh |
| Real-time | Server-Sent Events (SSE) | AI response streaming; simpler than WebSocket for one-directional push |
| Entrypoint | `cmd/server/main.go` | Separate binary from CLI `cmd/app/`; same wiring pattern |

The `internal/adapters/web/` package contains all HTTP handlers. Each handler does exactly: parse → validate → call `ApplicationService` → format JSON response. Nothing else.

### 2.2 Frontend

The frontend is server-rendered Go HTML with HTMX for dynamic updates and Alpine.js for local interactivity. No heavy JavaScript framework, no build step for templates, no separate Node.js process. The Go server is the single deployment artifact.

| Component | Choice | Rationale |
|---|---|---|
| Template engine | `a-h/templ` (Go) | Type-safe, compiled HTML templates; catches template errors at build time, not at runtime |
| Interactivity | HTMX 2.x | Partial page updates, form submission, SSE — without writing JavaScript |
| Local UI state | Alpine.js 3.x | Lightweight JS sprinkle for dropdowns, modals, toggles, chat history state |
| Styling | Tailwind CSS v4 | Utility-first; consistent design system; works directly in templ files |
| Charts | Chart.js 4.x | Lightweight, no framework dependencies; ~200 KB vs multi-MB alternatives |
| Icons | Heroicons (SVG inline) | Go-friendly; no JS icon library needed |
| AI chat streaming | HTMX SSE extension | Native SSE support via `hx-ext="sse"`; Alpine.js manages confirm/cancel button state |

**Why this stack vs React:**
- Single Go binary deployment — no Node.js runtime, no `npm build` in CI
- Server-rendered HTML means the Go type system validates templates at compile time
- HTMX partial swaps handle 90% of interactivity (form submissions, list refreshes, status updates) without custom JS
- Significantly simpler mental model: a handler renders a template; HTMX replaces a DOM fragment
- Alpine.js handles the remaining 10% (chat panel state, dynamic form rows, modal dialogs)

**`templ` overview:** Templates are `.templ` files compiled to Go functions. A handler calls `component.Render(ctx, w)` directly — no `html/template` parsing at runtime, no injection risk from missed escaping.

### 2.3 Directory Structure

```
web/
  templates/
    layouts/           base layout, sidebar, header (templ files)
    pages/             full-page templ components (one per screen)
    partials/          HTMX swap targets (order row, stock row, chat message, etc.)
    components/        reusable UI components (data table, form field, modal, badge)
  static/
    css/
      app.css          Tailwind CSS build output (committed; regenerated on change)
    js/
      htmx.min.js      HTMX 2.x (vendored)
      htmx-sse.js      HTMX SSE extension (vendored)
      alpine.min.js    Alpine.js 3.x (vendored)
      chart.min.js     Chart.js 4.x (vendored; loaded only on report pages)
      app.js           minimal custom JS (<100 lines: CSRF token injection, flash messages)

cmd/server/
  main.go              HTTP server entrypoint (<60 lines, wiring only)

internal/adapters/web/
  handlers.go          chi router setup and route registration
  middleware.go        logging, panic recovery, CORS, request ID, auth guard
  auth.go              login, logout, session handlers; JWT generation and validation
  accounting.go        trial balance, statement, journal entry, P&L, balance sheet
  orders.go            sales order CRUD and lifecycle (full page + HTMX partials)
  inventory.go         warehouse, stock, receive stock handlers
  purchasing.go        vendor, purchase order lifecycle
  jobs.go              job order lifecycle
  rentals.go           rental asset and contract lifecycle
  tax.go               tax rates, GST export, TDS, period locking
  admin.go             users, chart of accounts, account rules
  ai.go                chat endpoint, SSE streaming handler
  errors.go            error page renderer; structured JSON errors for API-style calls
  ctx.go               request context helpers (current user, company, flash messages)
```

**Vendoring JS libraries:** HTMX, Alpine.js, and Chart.js are vendored in `web/static/js/` as single minified files. No npm, no `package.json`, no build pipeline. `tailwindcss` CLI generates `app.css` via a Makefile target — the only external tooling required.

---

## 3. Authentication

Authentication is brought forward from Phase 33 to the web foundation. Without it, multi-user web access is not possible.

### 3.1 Schema

```sql
-- migrations/013_users.sql
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    company_id INT NOT NULL REFERENCES companies(id),
    username VARCHAR(100) NOT NULL,
    email VARCHAR(200) NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(30) NOT NULL DEFAULT 'ACCOUNTANT',  -- ACCOUNTANT | FINANCE_MANAGER | ADMIN
    is_active BOOL DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(company_id, username),
    UNIQUE(company_id, email)
);
```

### 3.2 JWT Flow

1. `POST /api/auth/login` — bcrypt-verifies credentials, issues JWT in httpOnly `Set-Cookie` (1-hour expiry, rolling refresh on active use).
2. All `/api/` routes require valid JWT cookie — enforced by auth middleware.
3. JWT payload: `{ user_id, company_id, role, exp }`.
4. `POST /api/auth/logout` — clears cookie.
5. `GET /api/auth/me` — returns `{ username, role, company_code }` for the frontend auth context.

### 3.3 Role Summary (aligned with Phase 33)

| Role | Capabilities |
|---|---|
| `ACCOUNTANT` | Read all data; create orders, receive stock, add job lines; propose journal entries |
| `FINANCE_MANAGER` | All ACCOUNTANT rights; approve POs, lock periods, commit AI proposals, cancel invoiced orders |
| `ADMIN` | All FINANCE_MANAGER rights; manage users, edit account rules, configure tax rates |

Role enforcement lives in `ApplicationService` methods — not just in HTTP middleware. The web handler passes the user's role in the request context; `ApplicationService` checks it before executing sensitive operations.

### 3.4 CLI / Automation Access

The CLI (`cmd/app/`) does not use JWT. It reads `DATABASE_URL` and `OPENAI_API_KEY` directly from the environment and calls domain services without HTTP. This path is unchanged.

For external integrations (Phase 34), a separate `api_keys` table provides static bearer token access to the REST API — suitable for webhooks and inter-system calls.

---

### 3.5 Multi-User Architecture

**What "multi-user" means here:** Multiple staff members of the same company use the system simultaneously through the web interface. Each has their own login credentials, session, and role-scoped access. This is **single-company multi-user access** — not multi-tenancy. True multi-company / multi-branch support is Phase 35.

**Concurrent access safety:** The existing architecture already handles concurrent access correctly:
- The immutable ledger (append-only `journal_entries`) eliminates UPDATE conflicts entirely.
- Gapless document numbering uses `SELECT … FOR UPDATE` row-level locks — safe under concurrent users.
- Inventory running totals are updated under `SELECT … FOR UPDATE` — safe under concurrent users.
- PostgreSQL transaction isolation handles race conditions at the data layer. No application-level locking is needed.

**Audit trail (add to Phase WF2 schema):** To answer "who posted this?", add `created_by_user_id INT REFERENCES users(id) ON DELETE SET NULL` to the following tables in a new migration after `013_users.sql`:

| Table | Column | Set when |
|---|---|---|
| `journal_entries` | `created_by_user_id` | Entry committed via `Ledger.Commit()` or `Ledger.CommitInTx()` |
| `sales_orders` | `created_by_user_id` | Order created via `CreateOrder()` |
| `documents` | `created_by_user_id` | Document posted via `DocumentService.Post()` |

The `ApplicationService` receives `userID` from the request context (injected by the auth middleware at `internal/adapters/web/ctx.go`) and passes it through to domain service calls. Domain services include it in INSERT statements. No separate audit log table is needed for Phase WF2 — `created_by_user_id` on business records is sufficient for basic accountability. A full change log / event sourcing layer is a Phase 33+ concern.

**Session management:** JWT httpOnly cookies with 1-hour expiry are stateless — the server holds no session table. On logout the cookie is cleared client-side. A stolen JWT remains valid until expiry (acceptable for Phase WF2). If server-side revocation is later needed ("log out all devices"), add a `sessions` table with a token hash and `invalidated_at` column — the auth middleware then checks it on each request. This is a Phase 33+ concern.

**User lifecycle:**

| Operation | How it works |
|---|---|
| First login | Admin user seeded via `014_seed_admin_user.sql`; bcrypt-hashed password from `ADMIN_INITIAL_PASSWORD` env (required at first boot) |
| Invite new user | ADMIN creates user via `POST /api/admin/users`; a temporary one-time password is either printed to stdout (Phase WF2) or emailed (requires `SMTP_*` env vars, Phase 33+) |
| Deactivate user | ADMIN sets `is_active = false`; auth middleware rejects login immediately; existing JWTs expire naturally within 1 hour |
| Password reset | Phase WF2: ADMIN resets via `PATCH /api/admin/users/:id/password`. Phase 33+: self-service email reset with time-limited token |
| Role change | ADMIN updates role via `PATCH /api/admin/users/:id/role`; takes effect at next JWT refresh (within 1 hour) |

**Gradual rollout path:**

| Phase | Multi-user capability delivered |
|---|---|
| WF2 | Login, JWT session, 3 roles, user management API, audit trail columns on business tables |
| WF3 | Login page UI, current user displayed in header, logout button |
| WF4 | All accounting screens show `created_by` on journal entry detail; auth guard covers all routes |
| Phase 33 | Role enforcement at `ApplicationService` level (FINANCE_MANAGER gate on commit/approve/period-lock; ADMIN gate on user management and account rules) |
| Phase 35 | Multi-branch: separate `company_id` per branch, branch-scoped user access, cross-branch reporting |

---

## 4. Migration Numbering Shift

Inserting `013_users.sql` and `014_seed_admin_user.sql` before the reporting views causes a numbering shift for all subsequent migrations. The corrected sequence from migration 013 onwards:

| Old number | New number | Description |
|---|---|---|
| *(new)* | `013_users.sql` | Users table |
| *(new)* | `014_seed_admin_user.sql` | Seed admin user for Company 1000 |
| `013_reporting_views.sql` | `015_reporting_views.sql` | Materialized views (Phase 9) |
| `014_trial_balance_view.sql` | `016_trial_balance_view.sql` | Trial balance view (Phase 10) |
| `014_vendors.sql` | `017_vendors.sql` | Vendor master (Phase 11) |
| … | … | All subsequent migrations shift +2 |

**Rule**: When writing a new migration, check the latest file in `migrations/` and use the next number. Never reuse or skip numbers.

---

## 5. Web Foundation Phases

These four phases are inserted as **Tier 2.5** in the main implementation plan, between Tier 2 (Business Rules) and Tier 3 (Domain Expansion). They replace Phase 32 from Tier 5.

---

### Phase WF1: REST API Foundation

**Goal**: Stand up the HTTP server with middleware, error handling, and stub endpoints. No frontend yet.

**Pre-requisites**: Phase 7 complete (all domain services exist and are wired into `ApplicationService`).

**Tasks:**

- [ ] Create `cmd/server/main.go` — HTTP server entrypoint. Wires `ApplicationService` (same wiring as `cmd/app/main.go`) and starts `chi` router on port from `SERVER_PORT` env (default `8080`).
- [ ] Create `internal/adapters/web/handlers.go` — register all routes.
- [ ] Create `internal/adapters/web/middleware.go` — request logging (method, path, duration, status), panic recovery, CORS (`ALLOWED_ORIGINS` env), `X-Request-ID` injection.
- [ ] Create `internal/adapters/web/errors.go` — `writeError(w, code, message string, status int)` helper. Standard format: `{"error": "...", "code": "...", "request_id": "..."}`.
- [ ] `GET /api/health` — returns `{"status": "ok", "company": "<loaded company code>"}`.
- [ ] Implement stub handlers for every `ApplicationService` method — return HTTP 501 with `{"error": "not implemented", "code": "NOT_IMPLEMENTED"}`.
- [ ] Write `docs/api/openapi.yaml` — OpenAPI 3.0 spec documenting all planned endpoints, request/response shapes, and auth requirements. This is the contract the frontend is built against.
- [ ] `go build ./cmd/server` compiles and runs. `GET /api/health` returns `200`.

**Acceptance criteria**: Server starts. All endpoints return 501 with correct JSON error structure. OpenAPI spec committed.

---

### Phase WF2: Authentication

**Goal**: Secure the API with JWT authentication and establish user management.

**Pre-requisites**: Phase WF1.

**Tasks:**

- [ ] Create `migrations/013_users.sql` — users table as above.
- [ ] Create `migrations/014_seed_admin_user.sql` — one admin user for Company 1000 (bcrypt-hashed password from `ADMIN_INITIAL_PASSWORD` env or a printed default at first boot).
- [ ] Create `internal/adapters/web/auth.go`:
  - `POST /api/auth/login` — bcrypt verify, issue JWT httpOnly `Set-Cookie`
  - `POST /api/auth/logout` — clear cookie
  - `GET /api/auth/me` — return current user (requires auth middleware)
  - JWT validation middleware — extracts and validates cookie; injects `userID`, `companyID`, `role` into request context
- [ ] Add to `ApplicationService` interface: `AuthenticateUser(ctx, username, password string) (*UserSession, error)` and `GetUser(ctx, userID int) (*UserResult, error)`.
- [ ] Implement in `appService`.
- [ ] Unit test: JWT generation, JWT validation (valid / expired / tampered).
- [ ] Apply migrations to both live and test DBs.

**Acceptance criteria**: `POST /api/auth/login` with valid credentials returns JWT cookie. `GET /api/orders` without cookie returns `401`. Logout clears the cookie.

---

### Phase WF3: Frontend Scaffold

**Goal**: Establish the Go/templ app shell with login, navigation, and routing. No Node.js — a single `go build` produces the complete server including all UI.

**Pre-requisites**: Phase WF2 (login endpoint exists).

**Tasks:**

- [ ] Install `templ` CLI: `go install github.com/a-h/templ/cmd/templ@latest`. Add `templ generate ./web/templates/...` to `go generate` and Makefile.
- [ ] Vendor JS libraries into `web/static/js/` (no npm, no node_modules):
  - `htmx@2.x.min.js`
  - `htmx-ext-sse.js` (HTMX SSE extension)
  - `alpine@3.x.min.js`
  - `chart@4.x.min.js` (loaded only on report pages via `<script>` in page-specific templates)
- [ ] Install Tailwind CSS standalone CLI binary. Add `tailwindcss -i web/static/css/input.css -o web/static/css/app.css` to Makefile. Input file uses `@tailwind` directives; output is committed.
- [ ] Create `web/templates/layouts/base.templ` — base HTML layout:
  - `<body hx-boost="true">` for HTMX-enhanced navigation (no full page reload on link clicks)
  - Left sidebar with navigation sections (Accounting, Sales, Purchasing, Inventory, Jobs, Rentals, Tax, Admin); active item highlighted via Go context
  - Header: company name, current user, logout link, AI chat panel toggle button
  - Flash message area (success/error banners, Alpine.js `x-show` with auto-dismiss timer)
  - Breadcrumb component
  - Responsive: sidebar collapses on mobile via Alpine.js `x-data` toggle
- [ ] Create `web/templates/pages/login.templ` — login form. On POST error, HTMX swaps error message partial in-place (no full page reload).
- [ ] Auth middleware redirects unauthenticated requests to `/login`. On successful login, redirect to `/`.
- [ ] Go server: embed `web/static/` via `//go:embed web/static` for single-binary deployment. Templates are compiled Go code (templ), so no embedding needed for them.
- [ ] Add Makefile targets: `make generate` (`templ generate`), `make css` (Tailwind CLI), `make dev` (parallel: css watch + templ watch + `go run ./cmd/server`), `make build` (generate + css + `go build`).

**Acceptance criteria**: `make dev` starts server. Login page functional. Auth guard works. Sidebar navigation renders. `hx-boost` navigation transitions work (network tab shows partial HTML responses, not full page loads).

---

### Phase WF4: Core Accounting Screens

**Goal**: Implement the web screens that replace the REPL's primary accounting commands — the first phase where REPL usage becomes redundant for reporting.

**Pre-requisites**: Phase WF3. Phases 8–10 (ReportingService) must be complete for full coverage; screens are added incrementally as each reporting phase completes.

**Tasks:**

- [ ] Implement handlers in `internal/adapters/web/accounting.go`:
  - `GET /api/companies/:code/trial-balance`
  - `GET /api/companies/:code/accounts/:accountCode/statement?from=YYYY-MM-DD&to=YYYY-MM-DD`
  - `GET /api/companies/:code/reports/pl?year=YYYY&month=MM`
  - `GET /api/companies/:code/reports/balance-sheet?date=YYYY-MM-DD`
  - `POST /api/companies/:code/reports/refresh` — refreshes materialized views
- [ ] **Dashboard** (`/`): AR balance card, AP balance card, Cash balance card, Revenue MTD, Expense MTD. Pending actions panel: unconfirmed orders, unshipped orders, uncollected invoices. Quick action buttons.
- [ ] **Trial Balance** (`/accounting/trial-balance`): full account table, sortable by code/name/balance. Debit and credit totals. Out-of-balance warning banner if totals differ.
- [ ] **Account Statement** (`/accounting/statement`): account code search (typeahead), date range picker, table with date/narration/reference/debit/credit/running-balance columns. CSV export button.
- [ ] **Manual Journal Entry** (`/accounting/journal-entry`): line-item form. AI-assist button sends description to chat panel and pre-fills lines. Validate → shows `Proposal.Validate()` result. Commit → calls `CommitProposal`.
- [ ] **P&L Report** (`/reports/pl`): year/month selector. Revenue section (expandable by account). Expense section (expandable). Net income total. Trailing 6-month bar chart.
- [ ] **Balance Sheet** (`/reports/balance-sheet`): as-of date picker. Assets / Liabilities / Equity sections (expandable). `IsBalanced` green/red indicator.

**Acceptance criteria**: All six screens render with live data. Trial balance matches database totals. REPL commands `/bal`, `/pl`, `/bs`, `/statement` are fully superseded. REPL is still present but no longer the primary interface for reporting.

---

## 6. Domain Web UI Phases

Each Tier 3 domain phase (Phases 11–21) gets a companion web UI phase immediately after the domain service is complete. The domain service is built first; the web UI consuming it follows. Full task lists are added to each domain's phase section in the main plan when they are scheduled.

| Domain built | Web UI phase | Screens added |
|---|---|---|
| Phase 11 (Vendor master) | **WD0** | Customers list/detail, Products list, Sales Orders list/detail/lifecycle |
| Phase 12–14 (Purchase cycle) | **WD1** | Vendors list/detail, Purchase Orders list/wizard/detail, PO lifecycle actions |
| Phase 15–17 (Job Orders) | **WD2** | Jobs list, new job wizard, job detail with line management, complete/invoice/pay |
| Phase 18 (Job inventory) | *(integrated into WD2)* | Material consumption shown on job detail screen |
| Phase 19–21 (Rentals) | **WD3** | Rental Assets list/register, Rental Contracts list/create/activate/bill/return |

> **WD0 note**: Customers, Products, and Sales Orders are existing domains with no web UI yet. WD0 builds their screens concurrently with the Vendor master phase, since `ApplicationService` methods for customers, products, and orders already exist.

---

## 7. AI Chat Panel (Phase WF5)

**Pre-requisites**: Phase WF3 (frontend shell exists). Full skill-based tool calling requires Phase 31; the chat panel ships in Phase WF5 in journal-entry-only mode and gains domain skills incrementally.

**Goal**: Provide an AI chat interface **embedded directly on the dashboard (home page)** alongside the KPI cards and pending actions — not hidden behind a button or slide-out. The chat is the primary input method for non-expert users from the moment they log in.

### 7.1 Dashboard Layout with Embedded Chat

```
┌──────────────────────────────────────────────────────────────────┐
│  Sidebar  │  Dashboard                                           │
│           │  ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│           │  │ AR: ₹2.1L│ │AP: ₹80K │ │Cash: ₹5L │  KPI cards │
│           │  └──────────┘ └──────────┘ └──────────┘            │
│           │  ┌────────────────────────┐ ┌──────────────────────┐│
│           │  │ Pending Actions        │ │ AI Assistant         ││
│           │  │ · 3 orders to ship     │ │ ┌──────────────────┐ ││
│           │  │ · 2 invoices unpaid    │ │ │ Hello! How can I │ ││
│           │  │ · 1 PO to approve      │ │ │ help today?      │ ││
│           │  └────────────────────────┘ │ └──────────────────┘ ││
│           │                             │ > __________________ ││
│           │                             │   [📎] [Send]        ││
│           │                             └──────────────────────┘│
└──────────────────────────────────────────────────────────────────┘
```

The AI Assistant panel occupies the right column of the dashboard. On other pages, the same chat is accessible via a fixed "Ask AI" button in the header that opens it as a slide-over panel — so users can ask questions or trigger actions without leaving their current context.

### 7.2 Architecture

```
User types in chat panel (dashboard or header slide-over)
        ↓
POST /chat  (HTMX hx-post, triggers SSE connection)
  form: { message, session_history (hidden input, JSON) }
        ↓
Handler calls ApplicationService.InterpretDomainAction()
  - selects skill (or falls back to journal entry Proposal)
  - returns proposed action + plain-English reasoning
        ↓
SSE handler streams response tokens to browser
  hx-ext="sse" appends tokens to chat thread as they arrive
        ↓
When stream ends: final message rendered as action card (templ partial)
  ┌────────────────────────────────────────────┐
  │ 📦 Receive Stock                           │
  │ Product: Widget A · Qty: 50 · Cost: ₹300  │
  │ Warehouse: MAIN · Credit: AP (Ravi Traders)│
  │ [Confirm]  [Cancel]  [Edit]                │
  └────────────────────────────────────────────┘
        ↓
User clicks Confirm → POST /chat/execute { skill_name, params }
        ↓
ApplicationService executes → renders result partial via HTMX swap
        ↓
Chat thread appended: green result card + plain-English explanation
```

Alpine.js manages the session history array (`x-data`) — updated after each exchange and serialised to the hidden form input before the next POST.

### 7.3 UI Components (templ partials)

- `chat_message_user.templ` — user bubble (right-aligned)
- `chat_message_ai_text.templ` — AI plain text bubble (clarification, explanation)
- `chat_action_card.templ` — structured proposed action with skill name, parameters table, Confirm/Cancel/Edit buttons
- `chat_compliance_warning.templ` — amber warning banner (GST jurisdiction, TDS threshold, HSN missing)
- `chat_result_card.templ` — green success card showing what was committed and affected accounts
- `chat_stream_cursor.templ` — blinking cursor shown during SSE streaming (removed on stream end)
- File upload input (`<input type="file">`) for invoice/receipt images → `POST /chat/upload`

### 7.4 Early vs Full Capability

| Phase | Chat panel capability |
|---|---|
| WF5 (before Phase 31) | Journal entry proposals only — same as REPL AI loop, better UX |
| After Phase 31 (tool calling) | Full domain navigation: orders, inventory, payments, jobs via skills |
| After Phase 25 (GST) | Inline GST compliance warning cards before confirmation |
| After Phase 27 (TDS) | TDS threshold alert cards before vendor payment confirmation |

---

### 7.5 Document Attachment to the AI Chat

Users can attach business documents directly to the chat window. The AI reads the document content as part of the request and uses it to complete the task — for example, reading a scanned vendor invoice and proposing the correct journal entry, or reading an Excel bank statement and proposing entries for each row.

#### 7.5.1 Supported File Types

| Category | Formats | AI processing method |
|---|---|---|
| Invoice / receipt photos | JPG, JPEG, PNG, WEBP | OpenAI vision API — GPT-4o is natively multimodal; images passed as `image_url` content blocks |
| PDF documents | PDF (text-based or scanned) | Text extraction via Go PDF library for text PDFs; first page rendered to image for scanned PDFs and passed to vision API |
| Spreadsheets | XLSX, XLS, CSV | Parsed to markdown table via Go library; injected as text context |
| Plain text | TXT | Read directly as text context |

Not supported in Phase WF5: DOCX, PPTX, ZIP archives. May be added in a later phase if needed.

#### 7.5.2 Upload Endpoint

```
POST /chat/upload
Content-Type: multipart/form-data
Body: file (binary), session_id (string)
```

Response:
```json
{
  "attachment_id": "550e8400-e29b-41d4-a716-446655440000",
  "filename": "ravi_invoice_jan2026.pdf",
  "file_type": "pdf",
  "page_count": 2,
  "size_bytes": 184320,
  "preview_text": "Ravi Traders\nInvoice #RT-2026-0042\nDate: 2026-01-15\n..."
}
```

The upload endpoint validates, processes, and stores the file immediately. It returns `attachment_id` and a `preview_text` (first ~500 chars of extracted content) so the chat UI can show a tooltip preview. Files are stored in `UPLOAD_DIR` (default: OS temp dir) with UUID filenames and cleaned up after 30 minutes of inactivity or when the session ends.

#### 7.5.3 File Constraints

| Constraint | Value | Reason |
|---|---|---|
| Max file size | 10 MB per file | OpenAI API limit for image inputs; PDF extraction memory |
| Max files per message | 5 | AI context window management |
| MIME type validation | Server-side via `net/http.DetectContentType` | File extensions alone are not trusted — MIME is checked from file bytes |
| Allowed MIME types | `image/jpeg`, `image/png`, `image/webp`, `application/pdf`, `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `text/csv`, `text/plain` | Security whitelist — no executable types |

#### 7.5.4 Processing Pipeline

```
File received (multipart upload)
        ↓
MIME type validation — reject immediately if not on whitelist
        ↓
Size check — reject if > 10 MB
        ↓
Store to UPLOAD_DIR with UUID filename (never use original filename on disk)
        ↓
┌─────────────────────────────────────────────────────────────┐
│ Processing by file type                                     │
│                                                             │
│ Image (JPG / PNG / WEBP)                                    │
│   → Base64-encode                                           │
│   → Store as image payload — passed to OpenAI vision API   │
│     as an image_url content block on next chat request      │
│                                                             │
│ PDF (text-based)                                            │
│   → Extract text via github.com/ledongthuc/pdf (pure Go)   │
│   → Truncate to ~8 000 tokens if needed                     │
│   → Store as text payload                                   │
│                                                             │
│ PDF (scanned / image-based — no extractable text)           │
│   → Detect: extracted text < 50 chars → treat as image     │
│   → Convert page 1 to PNG via github.com/gen2brain/go-fitz │
│     (CGo; requires MuPDF shared library)                    │
│   → Store as image payload — passed to vision API           │
│                                                             │
│ XLSX / XLS                                                  │
│   → Parse with github.com/xuri/excelize/v2 (pure Go)       │
│   → Convert first sheet to markdown table                   │
│   → Truncate to ~6 000 tokens if needed                     │
│   → Store as text payload                                   │
│                                                             │
│ CSV                                                         │
│   → Parse with encoding/csv (stdlib)                        │
│   → Convert to markdown table                               │
│   → Store as text payload                                   │
│                                                             │
│ TXT                                                         │
│   → Read raw bytes, truncate to ~4 000 tokens              │
│   → Store as text payload                                   │
└─────────────────────────────────────────────────────────────┘
        ↓
Return attachment_id + preview_text to browser
```

#### 7.5.5 AI Context Integration

When the user sends a chat message with attachments, the handler assembles a multi-part OpenAI message:

```
User turn content blocks (in order):
  1. For each text attachment:
       Text block: "[Attachment: filename.pdf]\n<extracted text>"
  2. For each image attachment:
       Image block: base64-encoded image (image_url content block)
  3. Text block: the user's typed message
```

The AI receives the full document content as part of the user turn. It extracts relevant fields (vendor name, invoice number, date, line items, amounts, tax), matches entities against the database (via read tools: `search_vendors`, `search_products`), and proposes the appropriate action.

**Advisory-only rule is unchanged.** Even with a document attached, the AI's response is an action card with Confirm/Cancel/Edit. No write occurs until explicit user confirmation.

#### 7.5.6 UI Changes to Chat Input (Phase WF5)

- Add a paperclip icon button beside the send button that triggers `<input type="file" multiple accept=".pdf,.jpg,.jpeg,.png,.webp,.xlsx,.xls,.csv,.txt">`.
- On file select: immediately POST to `/chat/upload` via HTMX (`hx-trigger="change"`). Show a file chip with filename and a loading spinner.
- On upload success: replace spinner with file chip showing filename, type icon, and a remove (×) button. On hover, show `preview_text` as a tooltip.
- On upload failure (wrong type, too large, server error): show an inline error chip under the input row (not a global flash message).
- Chip colours by type: blue (image), amber (PDF), green (spreadsheet), grey (text).
- Attachment IDs are stored in Alpine.js `attachments: [{id, filename, type}]` and serialised to a hidden form input on chat submit. The server resolves the stored payloads by ID when building the OpenAI request.

#### 7.5.7 Phase Rollout

| Phase | Document capability added |
|---|---|
| WF5 (initial) | Image upload only (JPG/PNG/WEBP) — no extra Go libraries needed; GPT-4o vision handles it natively |
| WF5 follow-on | PDF text extraction (text-based PDFs) — add `github.com/ledongthuc/pdf` |
| WF5 follow-on | Scanned PDF → image conversion — add `github.com/gen2brain/go-fitz` (CGo; requires MuPDF) |
| WF5 follow-on | Excel / CSV parsing — add `github.com/xuri/excelize/v2` |
| Phase 31+ | Multi-page PDF context management — chunking and RAG-like injection of the most relevant pages when document exceeds token budget |

---

## 8. Screen Inventory

### 8.1 Accounting

| Screen | Path | Key actions |
|---|---|---|
| Dashboard | `/` | KPI cards (AR, AP, Cash, Revenue MTD), pending actions list, **embedded AI chat panel** |
| Trial Balance | `/accounting/trial-balance` | View, refresh materialized views |
| Account Statement | `/accounting/statement` | Account + date range search, CSV export |
| Manual Journal Entry | `/accounting/journal-entry` | AI-assist, validate, commit |
| P&L Report | `/reports/pl` | Period selector, expand by account, 6-month chart |
| Balance Sheet | `/reports/balance-sheet` | As-of date, Assets/Liabilities/Equity, balance check |

### 8.2 Sales

| Screen | Path | Key actions |
|---|---|---|
| Customers | `/sales/customers` | List, create, view detail |
| Sales Orders | `/sales/orders` | List + status filter, new order wizard |
| Order Detail | `/sales/orders/:ref` | Confirm, ship, invoice, record payment |

### 8.3 Purchasing

| Screen | Path | Key actions |
|---|---|---|
| Vendors | `/purchasing/vendors` | List, create, view detail |
| Purchase Orders | `/purchasing/orders` | List + status filter, new PO wizard |
| PO Detail | `/purchasing/orders/:ref` | Approve, receive, record vendor invoice, pay |

### 8.4 Inventory

| Screen | Path | Key actions |
|---|---|---|
| Products | `/inventory/products` | List, view current stock levels |
| Warehouses | `/inventory/warehouses` | List, view per-warehouse stock |
| Stock Levels | `/inventory/stock` | Cross-warehouse table, low-stock indicator |
| Receive Stock | `/inventory/receive` | Form: product, warehouse, qty, unit cost, credit account |

### 8.5 Jobs

| Screen | Path | Key actions |
|---|---|---|
| Service Categories | `/jobs/categories` | List, create |
| Jobs | `/jobs` | List + status filter, new job wizard |
| Job Detail | `/jobs/:ref` | Start, add labour/material lines, complete, invoice, pay |

### 8.6 Rentals

| Screen | Path | Key actions |
|---|---|---|
| Rental Assets | `/rentals/assets` | List, register asset, view contracts |
| Rental Contracts | `/rentals/contracts` | List, create, activate |
| Contract Detail | `/rentals/contracts/:ref` | Bill period, return asset, record payment |
| Deposit Management | `/rentals/deposits` | Full or partial refund |

### 8.7 Tax & Compliance

| Screen | Path | Key actions |
|---|---|---|
| Tax Rates | `/tax/rates` | List configured rates and components |
| GST Reports | `/tax/gst` | Period selector, GSTR-1 JSON/CSV export, GSTR-3B export |
| TDS Tracker | `/tax/tds` | Cumulative by vendor + section, settle payment |
| Period Locking | `/tax/periods` | Lock / unlock accounting periods |

### 8.8 Administration

| Screen | Path | Key actions |
|---|---|---|
| Company Settings | `/admin/company` | Name, base currency, GST state code |
| Chart of Accounts | `/admin/accounts` | List, create account, view movements |
| Account Rules | `/admin/rules` | View and edit AR/AP/COGS/INVENTORY mappings |
| Users | `/admin/users` | List, create, set role, deactivate |

---

## 9. REPL Deprecation Timeline

| Milestone | Action |
|---|---|
| Phase WF1–WF3 complete | REPL still functions; web is in development |
| Phase WF4 complete | Web replaces REPL `/bal`, `/pl`, `/bs`, `/statement` |
| WD0 complete | Web replaces REPL `/orders`, `/customers`, `/products`, `/stock`, `/warehouses`, `/receive`, `/new-order`, `/confirm`, `/ship`, `/invoice`, `/payment` |
| WD1 complete | All existing REPL commands have web equivalents; REPL marked deprecated in README and `/help` output |
| WD2 complete | REPL removed from `cmd/app/main.go` routing; `cmd/app/` becomes CLI-only binary |
| WD3 complete (all domains) | `internal/adapters/repl/` package deleted; `repl.go`, `display.go`, `wizards.go` removed |

The REPL's AI clarification loop and display logic are not migrated — they are replaced by the web chat panel and HTML rendering respectively. There is no code reuse between REPL and web UI.

---

## 10. CLI Scope Definition

The CLI (`internal/adapters/cli/`, `cmd/app/`) is retained indefinitely with a stable, minimal interface:

| Command | Use case |
|---|---|
| `./app propose "event description"` | One-shot journal entry proposal (human-readable or JSON output) |
| `./app validate < proposal.json` | Validate a proposal in a CI/CD pipeline |
| `./app commit < proposal.json` | Commit a validated proposal in a pipeline |
| `./app balances` | Quick balance snapshot for monitoring and alerting scripts |

No new CLI commands will be added. The CLI binary is the automation and scripting interface — stable, minimal, and designed for non-interactive use.

---

## 11. Open Questions

| # | Question | Decision needed by |
|---|---|---|
| 1 | Migration numbering shift (+2 from 013 onwards) — rename existing migration files atomically before Phase WF2 | Before Phase WF2 |
| 2 | `ADMIN_INITIAL_PASSWORD` env vs printed random default at first boot | Phase WF2 |
| 3 | API versioning from day one (`/v1/` prefix on all routes) or plain routes initially | Phase WF1 |
| 4 | `go:embed web/static` in production binary vs filesystem serving — single binary preferred | Phase WF3 |
| 5 | CSRF protection strategy — double-submit cookie pattern or synchroniser token pattern with templ | Phase WF3 |
| 6 | Dashboard AI chat panel height on small screens — scrollable chat or collapsed to input bar only | Phase WF5 |
| 7 | Session history storage — Alpine.js `x-data` (lost on page navigate away) vs `sessionStorage` (persists within tab) | Phase WF5 |
