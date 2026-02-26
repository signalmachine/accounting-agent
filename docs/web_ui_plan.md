# Web UI Implementation Plan

> **Purpose**: Defines the strategy, architecture, and phase-by-phase plan for building the web interface as the primary user-facing product.
> **Last Updated**: 2026-02-26
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

### 1.3 Two-Area Application Structure

The web application is divided into two distinct areas with separate layouts, navigation patterns, and purposes. This is the foundational structural decision — every screen belongs to one of these two areas.

#### Area 1 — AI Chat Home (`/`)

The root route is a full-screen, dedicated conversational interface — the first thing a logged-in user sees. It is designed to look and feel like Claude.ai or ChatGPT: a clean page dominated by a large chat input at the bottom, chat history scrolling above it, and nothing else competing for attention. No sidebar, no accounting tables, no navigation chrome.

The AI handles any accounting task from here through natural language. Non-expert users who think in business terms ("I sold 50 units of Widget A to Acme Corp — create the invoice") never need to know which accounting screen to navigate to.

```
┌────────────────────────────────────────────────────────────────────┐
│  ◈ AccountingAI          CORP1 — FY 2026     [⊞ Open App]  [👤]  │
│  ──────────────────────────────────────────────────────────────────│
│                                                                    │
│                                                                    │
│                   What do you need help with?                      │
│                                                                    │
│   [📊 Trial balance]  [🧾 New invoice]    [📦 Check stock]        │
│   [📈 P&L this month] [💳 Record payment] [📋 Open orders]        │
│                                                                    │
│                                                                    │
│                                                                    │
│ ┌──────────────────────────────────────────────────────────────┐  │
│ │  Ask me anything — create invoices, check balances, receive  │  │
│ │  goods, record payments, explain entries...                  │  │
│ │                                                         [📎] │  │
│ │  ──────────────────────────────────────────────────── [Send] │  │
│ └──────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────┘
```

The quick shortcut chips are links to common accounting app screens — they navigate to Area 2. They are not AI prompts.

#### Area 2 — Accounting App (`/dashboard`, `/accounting/*`, `/sales/*`, `/reports/*`, …)

All structured screens — dashboards, lists, forms, reports, and settings — live here. This area uses the standard SaaS accounting layout: collapsible left sidebar for module navigation, top header with breadcrumbs and controls, and a full-width main content pane. It is designed to look and feel exactly like established cloud accounting products (Xero, Zoho Books, FreshBooks) — familiar, data-dense, and optimised for users who perform repeated structured operations.

```
┌────────────────────────────────────────────────────────────────────┐
│  ◈  CORP1       Accounting > Trial Balance     [Ask AI]  [👤]  ☰  │
├─────────────┬──────────────────────────────────────────────────────┤
│ ⌂ Home      │  Trial Balance                     [Refresh]         │
│ ─────────── │  ──────────────────────────────────────────────────  │
│ 📊 Dashboard│  Code  │ Account Name          │  Debit   │ Credit   │
│ ─────────── │  1000  │ Cash                  │ 5,00,000 │          │
│ Sales    ▼  │  1100  │ Bank Account          │ 8,20,000 │          │
│  Orders     │  1200  │ Accounts Receivable   │ 2,10,000 │          │
│  Customers  │  2000  │ Accounts Payable      │          │ 80,000   │
│ Purchases ▼ │  3000  │ Share Capital         │          │ 5,00,000 │
│  POs        │  ────  │                       │ ──────── │ ──────── │
│  Vendors    │        │ Total                 │15,30,000 │15,30,000 │
│ Inventory ▼ │                                                      │
│  Stock      │                                                      │
│  Warehouses │                                                      │
│ Reports   ▼ │                                                      │
│  P&L        │                                                      │
│  Balance Sh.│                                                      │
│ Settings  ▼ │                                                      │
└─────────────┴──────────────────────────────────────────────────────┘
```

#### Navigation Between Areas

| User is at | Destination | How to get there |
|---|---|---|
| AI Chat Home | Accounting App (dashboard) | "Open App" button in the chat home header |
| AI Chat Home | Specific app screen | Click a quick-shortcut chip |
| AI Chat Home | Specific app screen | Click "Edit & Submit (page)" on an AI action card |
| Accounting App | AI Chat Home | "Home" (⌂) link at top of sidebar, or logo click |
| Accounting App | AI (without leaving) | "Ask AI" button in app header — opens right-side slide-over drawer |

The AI is always reachable from within the accounting app via the slide-over drawer. Navigating back to the chat home is never required to use the AI.

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
    layouts/
      chat_layout.templ   AI chat home layout — minimal header, no sidebar, full-height chat
      app_layout.templ    Accounting app layout — collapsible sidebar + header + breadcrumbs
      login_layout.templ  Standalone login/auth layout (no nav, centred card)
      modal_shell.templ   Full-screen Alpine.js overlay for popup forms (shared, not a page layout)
    pages/             full-page templ components (one per screen)
    partials/          HTMX swap targets (order row, stock row, chat message, etc.)
    components/        reusable UI components (data table, form field, badge, flash message)
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
  purchases.go         vendor, purchase order lifecycle
  jobs.go              job order lifecycle
  rentals.go           rental asset and contract lifecycle
  tax.go               tax rates, GST export, TDS, period locking
  admin.go             users, chart of accounts, account rules
  ai.go                chat endpoint, SSE streaming handler
  errors.go            error page renderer; structured JSON errors for API-style calls
  ctx.go               request context helpers (current user, company, flash messages)
```

**Vendoring JS libraries:** HTMX, Alpine.js, and Chart.js are vendored in `web/static/js/` as single minified files. No npm, no `package.json`, no build pipeline. `tailwindcss` CLI generates `app.css` via a Makefile target — the only external tooling required.

### 2.4 URL and Route Conventions

Every URL in this application belongs to exactly one of three types. All handlers must follow this convention — no exceptions.

| Type | Pattern | Returns | Used by |
|---|---|---|---|
| **Page route** | No prefix — e.g. `GET /accounting/trial-balance` | Full HTML page wrapped in a layout | Browser navigation, `hx-boost` links |
| **API route** | `/api/` prefix — e.g. `GET /api/trial-balance` | JSON only | HTMX `hx-get` for data, external callers, CLI |
| **Partial route** | Same as page route, detected by `HX-Request` header | HTML fragment — no layout wrapper | HTMX swap targets (search results, table refresh, chat partials) |

**Page vs partial detection:** HTMX sends an `HX-Request: true` header on every HTMX-driven request. Handlers check this header to decide whether to render a full page (with layout) or a fragment (no layout):

```go
func (h *Handler) TrialBalance(w http.ResponseWriter, r *http.Request) {
    data := h.svc.GetTrialBalance(r.Context(), ...)
    if r.Header.Get("HX-Request") == "true" {
        // HTMX request — render table partial only
        components.TrialBalanceTable(data).Render(r.Context(), w)
        return
    }
    // Full page request — render with app_layout
    pages.TrialBalancePage(data).Render(r.Context(), w)
}
```

This means every page route doubles as its own partial route — no separate `/partial` URLs needed. `hx-boost` navigation and direct URL access both use the same route; only the render path differs.

**No API versioning prefix.** This is an internal web UI, not a public API. Plain routes now (`/api/trial-balance`), no `/v1/` prefix. If breaking API changes are needed in future, introduce versioning at that point. The OpenAPI spec documents the current contract.

**CSRF protection — synchroniser token pattern.** Every server-rendered page embeds a CSRF token in a `<meta name="csrf-token">` tag. A small block in `app.js` configures HTMX to send it as an `X-CSRF-Token` header on every non-GET request. The `middleware.go` CSRF middleware validates this header on all state-changing routes. The double-submit cookie pattern is not used — the synchroniser token pattern is more robust and integrates naturally with server-rendered templates.

```js
// app.js — run once on page load
document.addEventListener('htmx:configRequest', (e) => {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) e.detail.headers['X-CSRF-Token'] = token;
});
```

### 2.5 List Screen Conventions

All list screens (orders, customers, vendors, products, journal entries, etc.) follow these conventions consistently:

**Pagination:** Server-side, URL-based. Default 25 rows per page. Query parameters: `?page=N&per_page=25`. The table partial is HTMX-swapped; the URL is pushed with `hx-push-url="true"` so pagination is bookmarkable and the back button works.

**Search and filter:** Query parameters — e.g. `?search=acme&status=SHIPPED&from=2026-01-01`. The search input triggers an HTMX `GET` on the current URL with updated parameters: `hx-trigger="keyup changed delay:300ms"` for text inputs, `hx-trigger="change"` for selects and date pickers. The server returns the updated table partial. `hx-push-url="true"` keeps the URL in sync so filtered views are shareable and bookmarkable.

**Sort:** `?sort=date&dir=desc` in the URL. Column header clicks trigger an HTMX `GET` with updated sort parameters. Active sort column is highlighted; direction indicated by an arrow icon.

**Empty state:** Every list screen must render a meaningful empty state (not a blank table). Example: "No open orders — [Create new order]" with a link to the form.

**Loading state:** HTMX adds `htmx-request` class to the element making a request. Use `[aria-busy="true"]` + a CSS-driven skeleton overlay on the table container during HTMX swaps. Never show a full-page spinner for list refreshes.

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

### Phase WF1: Server + Chat UI Shell

**Goal**: Stand up the HTTP server with middleware, error handling, and the AI chat endpoint. Accounting domain endpoints remain as 501 stubs — they are implemented with real handlers in WF4/WD0–WD3, not here.

**Pre-requisites**: Phase 7 complete (server foundation). Phase 7.5 complete (chat endpoint requires `InterpretDomainAction`).

**Status**: 🔲 Partial — Foundation complete; chat UI shell pending Phase 7.5.

**Foundation tasks (✅ complete):**

- [x] Create `cmd/server/main.go` — HTTP server entrypoint. Wires `ApplicationService` and starts chi router on port from `SERVER_PORT` env (default `8080`).
- [x] Create `internal/adapters/web/handlers.go` — register all routes (accounting endpoints stubbed as 501).
- [x] Create `internal/adapters/web/middleware.go` — request logging, panic recovery, CORS, `X-Request-ID` injection.
- [x] Create `internal/adapters/web/errors.go` — `writeError` helper. Standard format: `{"error": "...", "code": "...", "request_id": "..."}`.
- [x] `GET /api/health` — returns `{"status": "ok", "company": "<code>"}`.
- [x] `go build ./cmd/server` compiles and runs.

**Chat UI shell tasks (🔲 pending — requires Phase 7.5):**

- [ ] `POST /api/chat/message` — accepts `{"text": "...", "company_code": "..."}`, calls `svc.InterpretDomainAction()`, streams response via SSE.
- [ ] SSE streaming handler — `text/event-stream` content type, `data:` events for each agent message chunk, `event: action_card` for write tool proposals requiring confirmation.
- [ ] `POST /api/chat/confirm` — accepts a `PendingAction` token, executes the confirmed write tool.
- [ ] Minimal chat frontend (single static HTML file served at `/`): chat input, scrolling history, SSE listener, action card confirm/cancel buttons. No auth yet — localhost dev use only.
- [ ] Error response format locked for all endpoints: `{"error": {"code": "VALIDATION_ERROR", "message": "..."}}`.
- [ ] `go build ./cmd/server` compiles. Chat endpoint responds to a test message.

**Acceptance criteria**: Server starts. `/api/health` returns 200. `/api/chat/message` streams an AI response via SSE. Accounting endpoints return 501. OpenAPI spec deferred — written incrementally as WF4/WD0 handlers are implemented.

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
- [ ] Create `web/templates/layouts/login_layout.templ` — standalone layout for the login page. Centred card, no navigation, company logo at top. On POST error, HTMX swaps error message partial in-place (no full page reload).
- [ ] Create `web/templates/layouts/app_layout.templ` — accounting app layout. Used by all screens in Area 2 (`/dashboard`, `/accounting/*`, `/sales/*`, etc.):
  - `<body hx-boost="true">` for HTMX-enhanced navigation (no full page reload on link clicks)
  - Left sidebar: collapsible via Alpine.js `x-data` toggle; module sections with expand/collapse (Sales ▼, Purchases ▼, Inventory ▼, Reports ▼, Settings ▼); active item highlighted; "⌂ Home" link at top returns to AI chat home
  - Top header: company name + FY badge, breadcrumb trail, "Ask AI" button (opens chat slide-over), user avatar + dropdown (profile, logout), hamburger for mobile
  - Flash message area (success/error banners, Alpine.js `x-show` with auto-dismiss timer)
  - Chat slide-over: right-side drawer (`x-show`, `x-transition`) containing the full AI chat component; persists across page navigations (Alpine.js state preserved in parent `x-data`)
  - Responsive: sidebar hidden on mobile, accessible via hamburger; slide-over is full-screen on mobile
- [ ] Create `web/templates/layouts/chat_layout.templ` — AI chat home layout. Used only by the `/` route. Minimal header (logo, company name, "Open App" button, user avatar). No sidebar. Full-height chat panel. See Section 7.1 for the full chat home page spec.
- [ ] Auth middleware: redirects unauthenticated requests to `/login`. On successful login, redirect to `/` (the AI chat home).
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
- [ ] **Dashboard** (`/dashboard`): KPI cards — AR balance, AP balance, Cash balance, Revenue MTD, Expense MTD. Pending actions panel: unconfirmed orders, unshipped orders, uncollected invoices, unapproved POs. Quick-action buttons linking to the relevant forms. Uses `app_layout.templ`.
- [ ] **Trial Balance** (`/accounting/trial-balance`): full account table, sortable by code/name/balance. Debit and credit totals. Out-of-balance warning banner if totals differ.
- [ ] **Account Statement** (`/accounting/statement`): account code search (typeahead), date range picker, table with date/narration/reference/debit/credit/running-balance columns. CSV export button.
- [ ] **Manual Journal Entry** (`/accounting/journal-entry`): line-item form. AI-assist button sends description to chat panel and pre-fills lines. Validate → shows `Proposal.Validate()` result. Commit → calls `CommitProposal`.
- [ ] **P&L Report** (`/reports/pl`): year/month selector. Revenue section (expandable by account). Expense section (expandable). Net income total. Trailing 6-month bar chart.
- [ ] **Balance Sheet** (`/reports/balance-sheet`): as-of date picker. Assets / Liabilities / Equity sections (expandable). `IsBalanced` green/red indicator.

**Acceptance criteria**: All six screens render with live data. Trial balance matches database totals. REPL commands `/bal`, `/pl`, `/bs`, `/statement` are fully superseded. REPL is still present but no longer the primary interface for reporting.

---

### Phase WF5: AI Chat Home

**Goal**: Build the full-screen AI chat home page at `/`, replacing the `/dashboard` redirect that served as a placeholder in WF4. The chat accepts natural language input, streams AI responses via SSE, renders action cards for write operations (with page and popup submission paths), and supports image file attachments. Ships in journal-entry-only mode; full domain skills are added incrementally from Phase 31 onwards.

**Pre-requisites**: Phase WF4 complete. Phase 8 (AccountStatement) complete so the AI has at least one read tool to call.

**Tasks:**

- [ ] Create `web/templates/layouts/chat_layout.templ` — minimal layout: header (logo, company name + FY badge, "Open App" button, user avatar). No sidebar. Full-height flex column: header → chat thread → pinned input bar.
- [ ] Create `web/templates/pages/chat_home.templ` — uses `chat_layout.templ`:
  - **Welcome state** (empty history): centred logo, greeting, 2×3 grid of quick-shortcut chips that link to Area 2 screens (not AI prompts).
  - **Thread area**: scrollable `<div>` that fills remaining height. On first message, welcome state is replaced by the thread via HTMX OOB swap.
  - **Input bar** (pinned bottom): multi-line `<textarea>` (auto-resize on input, max 6 rows), paperclip button, send button. `hx-post="/chat"` + `hx-ext="sse"`.
- [ ] Create chat partial templates:
  - `partials/chat_message_user.templ` — right-aligned user bubble
  - `partials/chat_message_ai_text.templ` — left-aligned AI text bubble; streamed token-by-token via SSE
  - `partials/chat_action_card.templ` — Mode B: entity summary, optional compliance warning banner, "Edit & Submit" + "Open in popup" + "Cancel" buttons (confirm-only variant for state-change operations)
  - `partials/chat_result_card.templ` — green success card after a form submission completes
  - `partials/chat_stream_cursor.templ` — blinking cursor during SSE stream (removed on stream end)
- [ ] Create `web/templates/layouts/modal_shell.templ` — full-screen Alpine.js overlay (`fixed inset-0 bg-black/50`), centred panel (`max-w-3xl max-h-[90vh] overflow-y-auto`). Listens for `open-modal` custom event dispatched by action card buttons; uses HTMX to load form content by URL.
- [ ] Implement `internal/adapters/web/ai.go`:
  - `GET /` — serves chat home page. Reads last 20 messages from sessionStorage (client-side); server only provides the shell.
  - `POST /chat` — accepts `{message, session_history (JSON), attachment_ids (JSON array)}`. Calls `ApplicationService.InterpretEvent()` (Phase 31 upgrades to `InterpretDomainAction`). Streams SSE response: tokens for Mode A; full action card partial for Mode B.
  - `POST /chat/upload` — multipart upload; MIME validation; UUID filename stored in `UPLOAD_DIR`; returns `{attachment_id, filename, file_type, preview_text}`. Image-only in WF5 initial (JPG/PNG/WEBP).
  - `POST /chat/clear` — invalidates current user's server-side session context if any. Client clears sessionStorage.
- [ ] Implement **proposal store** in `ai.go`: `sync.Map` keyed by UUID, value is `ProposalEntry{Type, Params, ExpiresAt}`. Background goroutine purges expired entries every 5 minutes. TTL: 15 minutes.
- [ ] Update all form page handlers to check for `?proposal_id=<uuid>` query parameter. If present and valid, pre-fill form fields from the proposal. If expired: render form empty with a flash notice "AI suggestion expired — please fill in manually."
- [ ] Update `app_layout.templ`: wire the "Ask AI" header button to open the chat slide-over drawer. The slide-over contains the same chat input and thread components as the home page. Session history is shared via `sessionStorage` — the same conversation continues regardless of whether the user is on the chat home or using the slide-over in the app.
- [ ] **Session history storage**: use `sessionStorage` (not Alpine.js `x-data`). Alpine.js `x-data` is destroyed when `hx-boost` navigation replaces the page body. `sessionStorage` persists for the lifetime of the browser tab and survives page navigations. On chat home load, Alpine initialises its history array from `sessionStorage`. On every AI turn, the updated history is written back to `sessionStorage`.
- [ ] Remove the WF4 placeholder: `GET /` no longer redirects to `/dashboard`.
- [ ] Update auth middleware: post-login redirect target is `/` (chat home), not `/dashboard`.

**Acceptance criteria**:
- `GET /` serves the chat home page (not a redirect).
- `GET /dashboard` still works — accounting app is unaffected.
- Sending a message returns a streamed SSE response; text bubbles appear as tokens arrive.
- Uploading an image shows a file chip; sending the message includes the image in the AI context.
- For a journal entry proposal: action card renders with "Edit & Submit" and "Open in popup" buttons.
- "Open in popup" opens `modal_shell` with the journal entry form pre-filled.
- "New conversation" clears the thread, resets welcome state, clears `sessionStorage`.
- Navigating to `/dashboard` and back to `/` restores the conversation from `sessionStorage`.
- "Ask AI" button in app header opens the slide-over; the same conversation history is present.

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

**Pre-requisites**: Phase WF3 (frontend shell exists). Phase WF4 (dashboard and accounting screens exist — the chat home replaces the `/` → `/dashboard` redirect). Full skill-based tool calling requires Phase 31; the chat panel ships in Phase WF5 in journal-entry-only mode and gains domain skills incrementally.

**Goal**: Build the AI chat home page at `/` — a full-screen, dedicated conversational interface that is the first thing a logged-in user sees. This is Area 1 of the two-area application structure (see Section 1.3). The AI is also accessible from anywhere within the accounting app (Area 2) via the "Ask AI" slide-over drawer in the app header, without navigating away.

### 7.1 AI Chat Home Page (`/`)

The home page is a dedicated full-screen conversational interface. It uses `chat_layout.templ` — no sidebar, no accounting navigation. The design is intentionally minimal so the user's attention goes entirely to the chat.

```
┌────────────────────────────────────────────────────────────────────┐
│  ◈ AccountingAI                CORP1 — FY 2026  [⊞ Open App] [👤] │
│────────────────────────────────────────────────────────────────────│
│                                                                    │
│                                                                    │
│              ◈                                                     │
│        AccountingAI                                                │
│                                                                    │
│        How can I help you today?                                   │
│                                                                    │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│   │📊 Trial bal. │  │🧾 New invoice│  │📦 Check stock│           │
│   └──────────────┘  └──────────────┘  └──────────────┘           │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│   │📈 P&L report │  │💳 Record pay │  │📋 Open orders│           │
│   └──────────────┘  └──────────────┘  └──────────────┘           │
│                                                                    │
│                                                                    │
│  ─────────────────────────────────────────────────────────────── │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │  Message AccountingAI...                              [📎]   │ │
│  │                                                              │ │
│  │                                              ──────── [Send] │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
└────────────────────────────────────────────────────────────────────┘
```

**Layout elements:**

- **Header** (minimal): logo + product name on the left; company name + FY badge in the centre; "Open App" button (⊞) and user avatar on the right. No sidebar toggle — there is no sidebar on this page.
- **Welcome state** (shown when chat history is empty): centred logo, greeting text, and a grid of quick-shortcut chips. Chips are links to Area 2 screens — clicking them navigates to the accounting app, they do not send AI messages.
- **Chat history area**: scrollable, fills the space between header and input. When the user sends their first message, the welcome state is replaced by the conversation thread. Messages are left-aligned (AI) and right-aligned (user), identical to Claude.ai.
- **Input bar** (pinned to bottom): multi-line textarea, paperclip button for file attachments (→ `POST /chat/upload`), Send button. HTMX `hx-post="/chat"` + SSE response stream.
- **No KPI cards on this page.** The dashboard KPIs are at `/dashboard` within the accounting app. The chat home is purely conversational.

**Conversation thread (after first message):**

Once conversation begins, the welcome state disappears and the thread takes over the full area above the input bar. AI responses stream in real time. Action cards (Mode B) appear inline in the thread. The thread is managed by Alpine.js `x-data` and persists for the browser session.

**Returning users:**

If the user has an active session with prior messages, the most recent conversation is shown on load (up to last 20 messages, oldest messages pruned from view). A "New conversation" button in the header clears the thread.

**After WF4 (before WF5):**

The `/` route redirects to `/dashboard` until Phase WF5 delivers this page. Auth middleware handles the redirect.

### 7.2 Architecture

The agent produces one of two response types on every turn (see **Section 7.6** for the full spec):

- **Mode A — Text reply**: the agent answers conversationally in streaming plain text (identical UX to Claude.ai or ChatGPT). Used for queries, explanations, reporting questions, and clarification prompts.
- **Mode B — Action card**: the agent proposes a domain operation (create invoice, receive stock, post journal entry, etc.). Renders as a structured entity summary card with **"Edit & Submit (page)"** and **"Open in popup"** options — not a simple inline Confirm button. All writes go through a pre-populated form page or modal, giving the user full visibility and edit access before any data is saved.

```
User types in chat panel (dashboard or header slide-over)
        ↓
POST /chat  (HTMX hx-post, triggers SSE connection)
  form: { message, session_history (hidden input, JSON) }
        ↓
Handler calls ApplicationService.InterpretDomainAction()
  - read tool calls execute autonomously (context, search, compliance)
  - agent returns: plain text OR proposed write tool + parameters
        ↓
SSE handler streams response tokens to browser
  hx-ext="sse" appends tokens to chat thread as they arrive

  ── Mode A path ───────────────────────────────────────────────
  Stream ends: chat_message_ai_text.templ appended to thread
    "Your AR balance is ₹2,10,000. Acme Corp owes ₹85,000 of that."

  ── Mode B path ───────────────────────────────────────────────
  Agent proposes a write tool → server stores proposal (TTL 15 min)
  Stream ends: chat_action_card.templ appended to thread
    ┌─────────────────────────────────────────────────────────┐
    │  🧾 Sales Invoice                                       │
    │  Customer:  Acme Corp (C001)                            │
    │  Order ref: SO-2026-00012                               │
    │  Net: ₹85,000  ·  GST 18%: ₹15,300  ·  Total: ₹1,00,300│
    │  [✏ Edit & Submit (page)]  [⧉ Open in popup]  [✕ Cancel]│
    └─────────────────────────────────────────────────────────┘
        ↓
  User clicks "Edit & Submit (page)"
    → GET /sales/invoices/new?proposal_id=<id>
    → Full form page, pre-filled with AI's proposed values
    → User reviews / edits / submits
    → POST /sales/invoices → ApplicationService executes
    → Redirect to invoice detail page

  OR User clicks "Open in popup"
    → HTMX hx-get="/sales/invoices/new?proposal_id=<id>&modal=1"
    → Same form loads inside Alpine.js modal overlay (no page navigation)
    → User reviews / edits / submits inside popup
    → On success: modal closes + chat thread appended with green result card
```

Alpine.js manages the session history array (`x-data`) — updated after each exchange and serialised to the hidden form input before the next POST.

### 7.3 UI Components (templ partials)

- `chat_message_user.templ` — user bubble (right-aligned)
- `chat_message_ai_text.templ` — AI plain text bubble; used for Mode A responses (conversational replies, query results, clarification questions). Streamed token-by-token via SSE — identical typing effect to Claude.ai / ChatGPT.
- `chat_action_card.templ` — Mode B response: entity summary card with document icon, key fields, compliance warnings (if any), and three buttons: **Edit & Submit (page)**, **Open in popup**, **Cancel**. Contains an embedded `data-proposal-id` attribute used by both navigation paths.
- `chat_compliance_warning.templ` — amber inline banner embedded inside `chat_action_card.templ` (e.g. GST interstate supply detected, TDS threshold crossed, HSN missing). Appears between the fields and the action buttons.
- `chat_result_card.templ` — green success card appended to thread after a form submission completes (via page or popup). Shows document number assigned, affected accounts, and a plain-English summary.
- `chat_stream_cursor.templ` — blinking cursor shown during SSE streaming (removed on stream end)
- `modal_shell.templ` — full-screen Alpine.js overlay (`fixed inset-0`) with a centred scrollable form panel. Shared across all popup form paths — not chat-specific. Listens for `open-modal` custom event to load its content via HTMX.
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

### 7.6 Response Modes — Text Reply vs Action Card

The AI chat produces one of two response types on every turn, determined entirely by what the agent proposes. There is no mode the user must select.

#### 7.6.1 Mode A — Text Reply (Conversational)

Used whenever the agent answers a question, provides an explanation, returns query results, or asks the user for more information. The response streams token-by-token via SSE and renders as a plain text bubble — visually and behaviourally identical to Claude.ai or ChatGPT.

**When Mode A is used:**

| User says | Agent does | Rendered as |
|---|---|---|
| "What's my accounts receivable balance?" | Calls `get_account_balance` read tool, returns figure | Text bubble with the number and a brief explanation |
| "Explain what IGST means" | Answers from knowledge | Text bubble — streamed plain text |
| "Which customers haven't paid this month?" | Calls `get_open_orders` or `get_account_statement` | Text bubble with a markdown table |
| "Which warehouse did the stock arrive at?" | Agent requests clarification | Text bubble with a question |
| Any read-tool-only response | Reads DB, assembles answer | Text bubble with the results |

Mode A responses stream in real time. The user sees words appear as they are generated — no waiting for a complete response before anything is displayed.

#### 7.6.2 Mode B — Action Card (Entity Work)

Used when the agent proposes creating, modifying, or committing a business entity. The response is a structured summary card, not a text bubble. The card summarises the entity and gives the user two paths to review and submit it.

**When Mode B is used:**

| User says | Agent proposes | Rendered as |
|---|---|---|
| "Create an invoice for Acme Corp for last month's consulting" | `invoice_order` write tool | Invoice action card |
| "Receive 50 units of Widget A from Ravi Traders at ₹300" | `receive_stock` write tool | Goods receipt action card |
| "I paid the electricity bill — ₹12,000 from bank" | `propose_journal_entry` write tool | Journal entry action card |
| "Create a purchase order for 100 units of P003 from Ravi" | `create_purchase_order` write tool | PO action card |
| "Record Acme's payment of ₹1,00,300" | `record_payment` write tool | Payment action card |

**Action card layout:**

```
┌─────────────────────────────────────────────────────────────────┐
│  🧾  Sales Invoice                                              │
│  ─────────────────────────────────────────────────────────────  │
│  Customer:   Acme Corp (C001)                                   │
│  Order ref:  SO-2026-00012                                      │
│  Net:        ₹85,000                                            │
│  GST (18%):  ₹15,300  (CGST 9% + SGST 9% — intrastate supply) │
│  Total AR:   ₹1,00,300                                          │
│  Due date:   2026-03-25  (Net 30)                               │
│  ─────────────────────────────────────────────────────────────  │
│  [✏  Edit & Submit]      [⧉  Open in popup]      [✕  Cancel]  │
└─────────────────────────────────────────────────────────────────┘
```

The card shows a summary of key fields — not every field. The full form (opened via either button) contains all editable fields.

**Compliance warnings** (GST, TDS, HSN, period lock) appear as amber banners inside the card, between the field summary and the action buttons, so the user sees them before choosing to proceed.

```
┌─────────────────────────────────────────────────────────────────┐
│  💳  Vendor Payment                                             │
│  ─────────────────────────────────────────────────────────────  │
│  Vendor:    Ravi Traders                                        │
│  PO ref:    PO-2026-00015                                       │
│  Gross:     ₹50,000                                             │
│  TDS (1%):  ₹500  deducted  (Section 194C)                     │
│  Net bank:  ₹49,500                                             │
│  ─────────────────────────────────────────────────────────────  │
│  ⚠  TDS applies: FY aggregate ₹75,000 + this ₹50,000 =        │
│     ₹1,25,000 > ₹1,00,000 threshold (Section 194C)             │
│  ─────────────────────────────────────────────────────────────  │
│  [✏  Edit & Submit]      [⧉  Open in popup]      [✕  Cancel]  │
└─────────────────────────────────────────────────────────────────┘
```

#### 7.6.3 The Two Submission Paths

**Path 1 — Edit & Submit (page navigation)**

Navigates to the full form page for the entity type, pre-populated with the AI's proposed values. The user reviews every field, edits anything that needs changing, and submits normally.

```
User clicks "Edit & Submit"
    ↓
GET /sales/invoices/new?proposal_id=abc123
    ↓
Handler reads proposal from short-lived store (TTL 15 min)
    ↓
Form page rendered with all fields pre-filled
    ↓
User edits if needed → submits
    ↓
POST /sales/invoices → ApplicationService.InvoiceOrder() executes
    ↓
Redirect to the new invoice's detail page
```

**Path 2 — Open in popup**

Loads the same form inside an Alpine.js modal overlay on the current page. The chat context remains visible behind the overlay. HTMX submits the form without a full page reload.

```
User clicks "Open in popup"
    ↓
Alpine dispatches open-modal event with src = /sales/invoices/new?proposal_id=abc123&modal=1
    ↓
modal_shell.templ overlay opens; HTMX loads the form into the panel
    ↓
User edits if needed → submits inside the popup
    ↓
POST /sales/invoices (HTMX request, target = modal body)
    ↓
On success: modal closes, HTMX OOB swap appends green result card to chat thread
```

**When to use which path:**
The user chooses based on preference — both paths produce identical results. The popup is faster for simple documents (the user never leaves the dashboard); the page is better for complex multi-line documents (more screen space, full layout).

**Cancel:**
Clicking Cancel dismisses the action card. The agent appends a short text message: *"Cancelled. Let me know if you'd like to try something else."* No data is written.

#### 7.6.4 Proposal Store

When the agent proposes a write tool (Mode B), the handler stores the proposed parameters server-side before rendering the action card:

```go
type Proposal struct {
    ID         string          // UUID
    Type       string          // "invoice_order", "receive_stock", "propose_journal_entry", etc.
    Params     map[string]any  // the agent's proposed parameter values
    CreatedAt  time.Time
}
```

Storage: in-memory map with a TTL cleanup goroutine (15-minute expiry). The `proposal_id` is embedded in the action card's data attributes. Both navigation paths read the proposal by ID to pre-fill their forms. If a proposal has expired by the time the user clicks, the form opens empty with a notice: *"The AI's suggested values have expired — please fill in the form manually."*

No database table is needed for proposals. They are ephemeral.

#### 7.6.5 Simple Confirmations (Exception to Mode B)

A small category of operations has nothing to edit — they are pure state transitions with no new data entry. For these, the action card retains an inline **Confirm** button alongside Cancel, because opening a form page or popup would add no value.

| Operation | Why inline Confirm is acceptable |
|---|---|
| Confirm a sales order (`confirm_order`) | No data to enter — just changes status from DRAFT to CONFIRMED |
| Mark an order as shipped (`ship_order`) | No data to enter — just changes status to SHIPPED |
| Approve a purchase order (`approve_po`) | Same — status change only |
| Cancel a reservation | Status change only |

For all document-creation operations (invoices, POs, journal entries, goods receipts, payments) — where amounts, accounts, or line items are involved — the page/popup path is always used. The inline Confirm button is never shown for document creation.

#### 7.6.6 Response Mode Decision Table

| Agent output | Response mode | UI component rendered |
|---|---|---|
| Plain text (reasoning, answer, explanation) | Mode A — text bubble | `chat_message_ai_text.templ` |
| Read tool result only (no write tool proposed) | Mode A — text bubble with data | `chat_message_ai_text.templ` |
| `request_clarification` write tool | Mode A — question text bubble | `chat_message_ai_text.templ` |
| Domain write tool (document creation, payment, receipt) | Mode B — action card with page/popup | `chat_action_card.templ` |
| `propose_journal_entry` write tool | Mode B — journal entry action card | `chat_action_card.templ` |
| Simple state-change write tool (confirm, ship, approve) | Mode B — action card with inline Confirm | `chat_action_card.templ` (confirm variant) |

The server decides which component to render — the frontend has no conditional logic for this. HTMX appends whatever partial the server returns.

---

## 8. Screen Inventory

Screens are grouped by application area. **Area 1** is the AI chat home (its own layout, no sidebar). **Area 2** is the accounting app (sidebar + header). All Area 2 screens share `app_layout.templ`.

### 8.0 Area 1 — AI Chat Home

| Screen | Path | Layout | Description |
|---|---|---|---|
| AI Chat Home | `/` | `chat_layout.templ` | Full-screen conversational AI interface. Quick-shortcut chips link to Area 2 screens. Chat history, SSE streaming, file attachments. See Section 7.1. |

---

### Sections 8.1–8.8 — Area 2: Accounting App

All screens below use `app_layout.templ` (collapsible sidebar + header + breadcrumbs). List screens follow the pagination and search conventions in Section 2.5. Routes follow the page/API/partial convention in Section 2.4.

---

### 8.1 Accounting

| Screen | Path | Key actions |
|---|---|---|
| Dashboard | `/dashboard` | KPI cards (AR, AP, Cash, Revenue MTD, Expense MTD), pending actions list, quick-action buttons |
| Trial Balance | `/accounting/trial-balance` | View, refresh materialized views |
| Account Statement | `/accounting/statement` | Account + date range search, CSV export |
| Manual Journal Entry | `/accounting/journal-entry` | AI-assist (pre-fills from chat), validate, commit |
| P&L Report | `/reports/pl` | Period selector, expand by account, 6-month chart |
| Balance Sheet | `/reports/balance-sheet` | As-of date, Assets/Liabilities/Equity, balance check |

### 8.2 Sales

| Screen | Path | Key actions |
|---|---|---|
| Customers | `/sales/customers` | List, create, view detail |
| Sales Orders | `/sales/orders` | List + status filter, new order wizard |
| Order Detail | `/sales/orders/:ref` | Confirm, ship, invoice, record payment |

### 8.3 Purchases

| Screen | Path | Key actions |
|---|---|---|
| Vendors | `/purchases/vendors` | List, create, view detail |
| Purchase Orders | `/purchases/orders` | List + status filter, new PO wizard |
| PO Detail | `/purchases/orders/:ref` | Approve, receive, record vendor invoice, pay |

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
| 4 | `go:embed web/static` in production binary vs filesystem serving — single binary preferred | Phase WF3 |

**Resolved questions (no longer open):**

| # | Question | Decision |
|---|---|---|
| 3 | API versioning | **No versioning prefix.** Plain routes (`/api/trial-balance`, not `/api/v1/trial-balance`). This is an internal web UI, not a public API. Introduce versioning only if breaking API changes arise in future. See Section 2.4. |
| 5 | CSRF protection strategy | **Synchroniser token pattern.** Server embeds CSRF token in `<meta name="csrf-token">` on every page. `app.js` configures HTMX to send it as `X-CSRF-Token` header on all non-GET requests. `middleware.go` validates it. See Section 2.4. |
| 6 | ~~Dashboard~~ Chat home mobile layout | **Full-height flex column on all screen sizes.** The input bar is pinned to the bottom using `position: sticky` inside the flex container. When the mobile soft keyboard appears and reduces viewport height, the thread scrolls up and the input bar remains visible at the new bottom. No collapsing needed — the layout already adapts. |
| 7 | Session history storage | **`sessionStorage`.** Alpine.js `x-data` is destroyed when `hx-boost` swaps the page body during navigation (e.g. chat home → dashboard → back to chat home). `sessionStorage` persists for the lifetime of the browser tab, survives HTMX-driven navigations, and is cleared automatically when the tab is closed. Alpine.js reads from `sessionStorage` on init and writes back after every turn. See Phase WF5 task list. |
