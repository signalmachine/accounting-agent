# UI Style Improvements Plan

## Baseline Assessment

- **CSS:** `web/static/css/input.css` has zero custom styles — just `@import "tailwindcss"`.
  All styling is inline Tailwind utility classes in 20 templ files.
- **Font:** Browser default system font (`ui-sans-serif, system-ui`). No custom font loaded.
- **Design tokens:** None defined. Colors, spacing, and typography are ad-hoc per template.
- **Tailwind version:** v4.2.1 — supports `@theme` blocks and `@layer components` in `input.css`.
  No `tailwind.config.js` needed; configuration lives in CSS.

All improvements below are CSS/template changes only. No backend changes. No new dependencies
except a font (self-hosted or system-stack upgrade). All 70 tests unaffected.

---

## Priority 1 — Foundation (Do First, Everything Depends on This)

### 1.1 Typography — Font Upgrade

**Current:** `ui-sans-serif, system-ui` — looks different on every OS.
**Target:** Inter — purpose-built for data-dense UIs, excellent number legibility, free, widely used
in accounting and finance products (Linear, Stripe, Notion all use Inter or similar).

**How to add (self-hosted, no CDN dependency):**

```bash
# Download Inter variable font (~95 KB woff2) into the static assets
# Source: https://rsms.me/inter/ or Google Fonts static download
web/static/fonts/Inter.woff2
web/static/fonts/Inter-italic.woff2
```

In `web/static/css/input.css`, add before `@import "tailwindcss"`:

```css
@font-face {
  font-family: 'Inter';
  src: url('/static/fonts/Inter.woff2') format('woff2');
  font-weight: 100 900;
  font-style: normal;
  font-display: swap;
}

@font-face {
  font-family: 'Inter';
  src: url('/static/fonts/Inter-italic.woff2') format('woff2');
  font-weight: 100 900;
  font-style: italic;
  font-display: swap;
}
```

Override the Tailwind v4 default font via `@theme`:

```css
@import "tailwindcss";

@theme {
  --font-sans: 'Inter', ui-sans-serif, system-ui, sans-serif;
  --font-numeric: 'Inter', ui-sans-serif, system-ui, sans-serif;
}
```

**Impact:** Immediate, global, visible on every page. Single highest-ROI change.

### 1.2 Design Tokens — Consistent Color Palette

Currently colors are chosen ad-hoc per template (slate, gray, blue, amber, green, red mixed
without a system). Define a semantic palette in `@theme`:

```css
@theme {
  /* Brand */
  --color-brand-900: oklch(20.8% .042 265.755);   /* current slate-900 — sidebar bg */
  --color-brand-800: oklch(27.9% .041 260.031);   /* slate-800 — hover */
  --color-brand-700: oklch(37.2% .044 257.287);   /* slate-700 — active nav */

  /* Surface */
  --color-surface: #ffffff;
  --color-surface-subtle: oklch(98.4% .003 247.858);  /* slate-50 */
  --color-surface-muted:  oklch(96.8% .007 247.896);  /* slate-100 */

  /* Border */
  --color-border: oklch(92.9% .013 255.508);      /* slate-200 */
  --color-border-subtle: oklch(92.8% .006 264.531); /* gray-200 */

  /* Text */
  --color-text-primary:   oklch(20.8% .042 265.755); /* slate-900 */
  --color-text-secondary: oklch(44.6% .043 257.281); /* slate-600 */
  --color-text-muted:     oklch(55.4% .046 257.417); /* slate-500 */

  /* Financial semantic colors */
  --color-debit:   oklch(57.7% .245 27.325);   /* red-600  — debits, negative values */
  --color-credit:  oklch(52.7% .154 150.069);  /* green-700 — credits, positive values */
  --color-balanced: oklch(48.8% .243 264.376); /* blue-700 — balanced totals */
  --color-warning: oklch(55.4% .135 66.442);   /* yellow-700 */
}
```

This does not require changing all templates immediately — it's additive. Gradually replace
ad-hoc color classes with semantic ones as templates are touched.

### 1.3 Tabular Numbers — Critical for Accounting

All monetary values must use tabular (monospace) numbers so columns align:

```css
@layer components {
  .num {
    font-variant-numeric: tabular-nums;
    font-feature-settings: "tnum";
    text-align: right;
    white-space: nowrap;
  }

  .num-debit  { color: var(--color-debit); }
  .num-credit { color: var(--color-credit); }
  .num-zero   { color: var(--color-text-muted); }
  .num-total  {
    font-weight: 600;
    border-top: 1px solid var(--color-border);
    padding-top: 0.25rem;
  }
}
```

Apply `.num` to every `<td>` containing a monetary value across all report templates
(`trial_balance.templ`, `pl_report.templ`, `balance_sheet.templ`, `account_statement.templ`).

---

## Priority 2 — Financial Tables (Core of the App)

Every report page is a table. This is the most-used surface in an accounting app.

### 2.1 Table Base Styles

Add to `input.css`:

```css
@layer components {
  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--text-sm);
  }

  .data-table thead th {
    background-color: var(--color-surface-muted);
    color: var(--color-text-secondary);
    font-weight: 600;
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 0.625rem 1rem;
    border-bottom: 1px solid var(--color-border);
    white-space: nowrap;
  }

  .data-table thead th:last-child,
  .data-table td:last-child {
    text-align: right;
    padding-right: 1.25rem;
  }

  .data-table tbody tr {
    border-bottom: 1px solid var(--color-border-subtle);
    transition: background-color 0.1s;
  }

  .data-table tbody tr:hover {
    background-color: var(--color-surface-subtle);
  }

  .data-table tbody td {
    padding: 0.625rem 1rem;
    color: var(--color-text-primary);
  }

  /* Subtotal row */
  .data-table tr.row-subtotal td {
    background-color: var(--color-surface-muted);
    font-weight: 600;
    border-top: 1px solid var(--color-border);
  }

  /* Total row */
  .data-table tr.row-total td {
    background-color: oklch(96% .01 255);
    font-weight: 700;
    border-top: 2px solid var(--color-border);
    border-bottom: 2px solid var(--color-border);
  }

  /* Section header row (e.g. "Assets", "Liabilities") */
  .data-table tr.row-section td {
    background-color: var(--color-surface-muted);
    font-weight: 600;
    font-size: var(--text-xs);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-secondary);
    padding-top: 1rem;
  }

  /* Sticky header for long tables */
  .data-table thead {
    position: sticky;
    top: 0;
    z-index: 10;
  }
}
```

Apply class `data-table` to `<table>` elements in:
- `trial_balance.templ`
- `pl_report.templ`
- `balance_sheet.templ`
- `account_statement.templ`
- `orders_list.templ`
- `po_list.templ`
- `customers_list.templ`
- `vendors_list.templ`
- `products_list.templ`
- `stock_levels.templ`

### 2.2 Page Header Component

Each page currently has inconsistent header patterns. Standardise:

```css
@layer components {
  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1.5rem;
    gap: 1rem;
  }

  .page-title {
    font-size: var(--text-xl);
    font-weight: 600;
    color: var(--color-text-primary);
    line-height: 1.25;
  }

  .page-subtitle {
    font-size: var(--text-sm);
    color: var(--color-text-muted);
    margin-top: 0.125rem;
  }
}
```

### 2.3 Card Component

Replace ad-hoc `bg-white rounded-xl shadow p-6` scattered across templates:

```css
@layer components {
  .card {
    background-color: var(--color-surface);
    border-radius: 0.75rem;
    border: 1px solid var(--color-border-subtle);
    box-shadow: 0 1px 3px 0 rgb(0 0 0 / 0.06), 0 1px 2px -1px rgb(0 0 0 / 0.06);
  }

  .card-header {
    padding: 1rem 1.25rem;
    border-bottom: 1px solid var(--color-border-subtle);
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .card-body {
    padding: 1.25rem;
  }

  .card-body-flush {
    /* For tables that need full-width treatment */
    overflow-x: auto;
  }
}
```

---

## Priority 3 — Sidebar Polish

The sidebar is visible on every authenticated page. Small improvements compound across the
entire app.

### 3.1 Sidebar Changes (in `app_layout.templ`)

**Current:** Flat `bg-slate-900`, basic hover states.
**Target:** Subtle depth, cleaner section labels, accent border on active item.

```css
@layer components {
  /* Sidebar brand area */
  .sidebar-brand {
    background: linear-gradient(135deg,
      oklch(18% .05 270) 0%,
      oklch(22% .04 260) 100%
    );
  }

  /* Active nav item — left accent border instead of just background */
  .nav-item-active {
    background-color: rgba(255 255 255 / 0.1);
    border-left: 2px solid white;
    color: white;
    font-weight: 500;
  }

  /* Inactive nav item */
  .nav-item {
    border-left: 2px solid transparent;
    color: oklch(75% .02 260); /* slate-300 ish */
    transition: background-color 0.15s, color 0.15s;
  }

  .nav-item:hover {
    background-color: rgba(255 255 255 / 0.07);
    color: white;
  }
}
```

Update `navItemClass()` function in `app_layout.templ` to use these classes.

### 3.2 Section Label Styling

Current section labels ("Sales", "Purchases") use a toggle button. Make them visually
distinct as non-clickable category headers (or keep clickable but style better):

```
SALES            ▾
  Customers
  Orders
```

Add `text-xs uppercase tracking-widest text-slate-500` to section button labels — current
`text-sm text-slate-400` is too close to item label size.

---

## Priority 4 — Button System

Currently buttons are styled ad-hoc per template. Define a consistent system:

> **White text rule:** `color: white` is only acceptable when the button background is
> near-black (e.g. `--color-brand-900`, `--color-brand-800`). On light surfaces or
> light-colored card backgrounds, always use dark text (`text-slate-800` or
> `var(--color-text-primary)`).

```css
@layer components {
  /* Primary — dark actions (Save, Submit, Confirm) */
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.375rem;
    padding: 0.5rem 1rem;
    font-size: var(--text-sm);
    font-weight: 500;
    border-radius: 0.5rem;
    transition: background-color 0.15s, box-shadow 0.15s;
    white-space: nowrap;
    cursor: pointer;
  }

  .btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .btn-primary {
    background-color: var(--color-brand-900);
    color: white;
  }
  .btn-primary:hover { background-color: var(--color-brand-800); }
  .btn-primary:active { background-color: oklch(15% .042 265); }

  .btn-secondary {
    background-color: var(--color-surface);
    color: var(--color-text-primary);
    border: 1px solid var(--color-border);
  }
  .btn-secondary:hover { background-color: var(--color-surface-muted); }

  .btn-danger {
    background-color: oklch(57.7% .245 27.325);
    color: white;
  }
  .btn-danger:hover { background-color: oklch(50.5% .213 27.518); }

  .btn-ghost {
    color: var(--color-text-secondary);
  }
  .btn-ghost:hover {
    background-color: var(--color-surface-muted);
    color: var(--color-text-primary);
  }

  /* Sizes */
  .btn-sm { padding: 0.375rem 0.75rem; font-size: var(--text-xs); }
  .btn-lg { padding: 0.625rem 1.25rem; font-size: var(--text-base); }
}
```

---

## Priority 5 — Form Inputs

```css
@layer components {
  .input {
    width: 100%;
    padding: 0.5rem 0.75rem;
    font-size: var(--text-sm);
    color: var(--color-text-primary);
    background-color: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 0.5rem;
    transition: border-color 0.15s, box-shadow 0.15s;
    outline: none;
  }

  .input:focus {
    border-color: var(--color-brand-700);
    box-shadow: 0 0 0 3px oklch(37.2% .044 257.287 / 0.15);
  }

  .input::placeholder { color: var(--color-text-muted); }

  .label {
    display: block;
    font-size: var(--text-sm);
    font-weight: 500;
    color: var(--color-text-secondary);
    margin-bottom: 0.375rem;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 0.375rem;
  }

  /* Select */
  .select {
    appearance: none;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 20 20'%3E%3Cpath stroke='%236b7280' stroke-linecap='round' stroke-linejoin='round' stroke-width='1.5' d='M6 8l4 4 4-4'/%3E%3C/svg%3E");
    background-position: right 0.5rem center;
    background-repeat: no-repeat;
    background-size: 1.25em 1.25em;
    padding-right: 2.5rem;
  }
}
```

Apply `.input`, `.label`, `.form-group` to:
- `order_wizard.templ`
- `po_wizard.templ`
- `vendor_form.templ`
- `journal_entry.templ`

---

## Priority 6 — Chat UI Polish

### 6.1 Markdown Content Styles

After adding `marked.js` (see `streaming_chat_plan.md`), add styles for rendered markdown
inside chat bubbles:

```css
@layer components {
  .chat-md h1, .chat-md h2, .chat-md h3 {
    font-weight: 600;
    margin-top: 0.75rem;
    margin-bottom: 0.25rem;
    color: var(--color-text-primary);
  }
  .chat-md h3 { font-size: var(--text-sm); }
  .chat-md h2 { font-size: var(--text-base); }

  .chat-md p  { margin-bottom: 0.5rem; line-height: 1.6; }
  .chat-md ul { list-style: disc; padding-left: 1.25rem; margin-bottom: 0.5rem; }
  .chat-md li { margin-bottom: 0.125rem; }

  .chat-md strong { font-weight: 600; color: var(--color-text-primary); }

  .chat-md table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--text-xs);
    margin-bottom: 0.5rem;
  }
  .chat-md th {
    background-color: var(--color-surface-muted);
    font-weight: 600;
    padding: 0.25rem 0.5rem;
    text-align: left;
    border: 1px solid var(--color-border);
  }
  .chat-md td {
    padding: 0.25rem 0.5rem;
    border: 1px solid var(--color-border);
  }
  .chat-md td:last-child { text-align: right; font-variant-numeric: tabular-nums; }

  .chat-md code {
    font-family: var(--font-mono);
    font-size: 0.8em;
    background-color: var(--color-surface-muted);
    padding: 0.1em 0.3em;
    border-radius: 0.25rem;
  }
}
```

Apply class `chat-md` to the AI text bubble `div` in `chat_home.templ`:

```html
<!-- Before -->
<div class="... whitespace-pre-wrap" x-html="msg.html || msg.text"></div>

<!-- After -->
<div class="... chat-md" x-html="msg.html || msg.text"></div>
```

Remove `whitespace-pre-wrap` when using rendered markdown — the markdown parser handles
paragraph spacing.

### 6.2 Chat Typing Indicator Animation

Add a CSS-only animated dots indicator (no JS needed):

```css
@layer components {
  .typing-dots span {
    display: inline-block;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--color-text-muted);
    animation: typing-bounce 1.2s infinite;
  }
  .typing-dots span:nth-child(2) { animation-delay: 0.2s; }
  .typing-dots span:nth-child(3) { animation-delay: 0.4s; }

  @keyframes typing-bounce {
    0%, 60%, 100% { transform: translateY(0); }
    30% { transform: translateY(-6px); }
  }
}
```

Replace "Thinking…" text in `chat_home.templ`:

```html
<!-- Before -->
<div class="...text-slate-400">Thinking…</div>

<!-- After -->
<div class="...flex items-center gap-1.5">
  <div class="typing-dots flex gap-1">
    <span></span><span></span><span></span>
  </div>
</div>
```

### 6.3 Chat Bubble Refinements

Small changes with noticeable impact:

- User bubble: add a subtle gradient (`from-slate-900 to-slate-800`) instead of flat color
- AI bubble: use `bg-white border border-gray-100 shadow-sm` instead of flat `bg-slate-100`
  — gives it lift and separates it visually from the page background
- Increase bubble padding from `px-4 py-2.5` to `px-4 py-3` — more breathing room
- Add `leading-relaxed` to bubble text for better line spacing

### 6.4 Action Card (Journal Entry Proposal / Tool Confirm Cards)

The action card (e.g. "Journal Entry Proposal") already has a good aesthetic: light
periwinkle-blue background (`bg-indigo-50`/`bg-blue-50`), rounded corners, a blue
description line, and a green success message on confirm. **Keep this look.**

**Problem:** The "Post Entry" confirm button currently renders as a ghost/text button with
`color: white` — invisible against the light-blue card background. Screenshot evidence:
the button label "✓ Post Entry" is barely readable. White text must not be used on light
surfaces (see Priority 4 white-text rule).

**Fix — one line change in `chat_home.templ`:**

The confirm button is a ghost/text button — that style is intentional and looks good. Keep
it. Only change the text color from `white` to a dark color:

```html
<!-- Before -->
<button class="... text-white ...">✓ Post Entry</button>

<!-- After -->
<button class="... text-slate-800 hover:text-slate-900 ...">✓ Post Entry</button>
```

No new CSS classes needed. No style change beyond the font color.

Apply in `chat_home.templ` action card template.

**Preserve:** The light-blue card background, rounded corners, blue description text, and
the green `✓ Journal entry posted.` success message — these all look good.

**Confirmed good pattern (image 2):**
- Card: `bg-indigo-50` or `bg-blue-50`, `rounded-xl`, `border border-indigo-100`
- Description line: `text-blue-700`
- Success message: `text-green-600 font-medium` with a `✓` checkmark prefix
- No white text anywhere on the light card surface

---

## Priority 7 — Dashboard KPI Cards

Current dashboard KPI cards are plain white boxes. Small changes:

```css
@layer components {
  .kpi-card {
    background-color: var(--color-surface);
    border-radius: 0.75rem;
    border: 1px solid var(--color-border-subtle);
    border-left: 3px solid;          /* accent color set per-card */
    padding: 1.25rem;
    box-shadow: 0 1px 3px 0 rgb(0 0 0 / 0.05);
  }

  .kpi-value {
    font-size: var(--text-2xl);
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
    color: var(--color-text-primary);
  }

  .kpi-label {
    font-size: var(--text-xs);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-muted);
    margin-bottom: 0.375rem;
  }
}
```

Each KPI card gets its own accent border color via inline style or a modifier class:
- Cash: `border-left-color: var(--color-credit)` (green)
- AR: `border-left-color: var(--color-balanced)` (blue)
- AP: `border-left-color: var(--color-debit)` (red)
- Revenue: `border-left-color: var(--color-credit)` (green)

---

## Priority 8 — Empty States

When lists have no data, show a styled placeholder instead of blank:

```css
@layer components {
  .empty-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 4rem 2rem;
    color: var(--color-text-muted);
    text-align: center;
  }

  .empty-state-icon {
    font-size: 2.5rem;
    margin-bottom: 1rem;
    opacity: 0.4;
  }

  .empty-state-title {
    font-size: var(--text-base);
    font-weight: 500;
    color: var(--color-text-secondary);
    margin-bottom: 0.25rem;
  }

  .empty-state-text {
    font-size: var(--text-sm);
    max-width: 20rem;
  }
}
```

Add empty state blocks to all list templates where the data array is empty.

---

## Priority 9 — Status Badges

Status badges (DRAFT, APPROVED, RECEIVED, INVOICED, PAID) are used across order and PO
templates. Standardise them:

```css
@layer components {
  .badge {
    display: inline-flex;
    align-items: center;
    padding: 0.125rem 0.5rem;
    border-radius: 9999px;
    font-size: var(--text-xs);
    font-weight: 500;
    white-space: nowrap;
  }

  .badge-draft    { background: oklch(96.8% .007 247.896); color: oklch(44.6% .043 257.281); }
  .badge-approved { background: oklch(97% .014 254.604);   color: oklch(48.8% .243 264.376); }
  .badge-received { background: oklch(98.7% .026 102.212); color: oklch(55.4% .135 66.442);  }
  .badge-invoiced { background: oklch(97% .014 254.604);   color: oklch(48.8% .243 264.376); }
  .badge-paid     { background: oklch(98.2% .018 155.826); color: oklch(52.7% .154 150.069); }
  .badge-cancelled{ background: oklch(97.1% .013 17.38);   color: oklch(57.7% .245 27.325);  }
  .badge-shipped  { background: oklch(97% .014 254.604);   color: oklch(48.8% .243 264.376); }
}
```

---

## Priority 10 — Eliminate the Top Header Bar

The top header (`h-16`, 64px) is **fully redundant on desktop** — the sidebar already shows:
- Company code + FY badge → can move to sidebar brand area
- User avatar + menu → already in sidebar footer
- Hamburger → can move into sidebar brand area
- Ask AI button → already duplicated by "AI Agent" nav link at `GET /`

**Recommendation: hide header entirely on `lg:` screens, keep a thin mobile-only bar.**

### 10.1 Desktop — Hide Header Completely (`lg:hidden`)

In `app_layout.templ`, add `lg:hidden` to the `<header>` element:

```html
<!-- Before -->
<header class="h-16 bg-white border-b border-gray-200 flex items-center px-4 gap-3 flex-shrink-0">

<!-- After -->
<header class="h-10 bg-white border-b border-gray-200 flex items-center px-4 gap-3 flex-shrink-0 lg:hidden">
```

On `lg:` screens (≥1024px) the header disappears entirely, reclaiming the full 64px.
On mobile/tablet the header shrinks to `h-10` (40px) — a thin bar with only the essential
mobile controls.

### 10.2 Mobile Header — Strip to Essentials Only

With the header hidden on desktop, the mobile version only needs two things:
- Hamburger (to open the sidebar)
- User avatar (for logout)

Remove from the mobile header:
- Company + FY badge (visible in sidebar brand area)
- Ask AI button (reachable via sidebar "AI Agent" link)

```html
<header class="h-10 bg-white border-b border-gray-200 flex items-center px-3 gap-3 flex-shrink-0 lg:hidden">
  <!-- Hamburger only -->
  <button class="text-gray-500 hover:text-gray-700 p-1 rounded-lg hover:bg-gray-100"
          x-on:click="sidebarOpen = !sidebarOpen">
    <svg class="w-4 h-4" ...hamburger icon...></svg>
  </button>
  <span class="text-xs font-medium text-slate-600 flex-1">{ d.CompanyName }</span>
  <!-- Compact user menu -->
  <div class="relative" x-data="{ open: false }">
    <button class="w-7 h-7 rounded-full bg-slate-200 text-xs font-bold text-slate-700"
            x-on:click="open = !open">
      { userInitial(d.Username) }
    </button>
    <!-- dropdown same as before -->
  </div>
</header>
```

### 10.3 Move Company Info into Sidebar Brand Area

The sidebar brand area currently shows just "⌂ Accounting". Extend it to show company
code + FY badge — visible on desktop where the header is hidden:

```html
<!-- Sidebar brand area — updated -->
<div class="h-16 flex flex-col justify-center px-4 border-b border-slate-700 flex-shrink-0">
  <a href="/dashboard" class="flex items-center gap-2 text-white hover:text-slate-200">
    <span class="text-lg">⌂</span>
    <span class="font-semibold text-sm tracking-wide">Accounting</span>
  </a>
  <!-- Company + FY — only meaningful on desktop where header is hidden -->
  <div class="flex items-center gap-1.5 mt-0.5 lg:flex hidden">
    <span class="text-xs text-slate-400 font-medium">{ d.CompanyName }</span>
    if d.FYBadge != "" {
      <span class="text-xs text-slate-500">{ d.FYBadge }</span>
    }
  </div>
</div>
```

### 10.4 Move Ask AI Button into Sidebar (Desktop)

The "Ask AI" slide-over trigger currently lives in the header. On desktop (where header is
hidden), it needs a home. Add it as a prominent button above the nav links in the sidebar,
visible only on `lg:` screens (on mobile it's accessible via the "AI Agent" nav link):

```html
<!-- Above the nav links, inside <aside> -->
<div class="px-3 pt-3 pb-2 lg:block hidden">
  <button
    class="w-full flex items-center gap-2 px-3 py-2 bg-slate-700 hover:bg-slate-600
           text-white text-sm font-medium rounded-lg transition-colors"
    x-on:click="chatOpen = true"
  >
    <span>✨</span>
    <span>Ask AI</span>
  </button>
</div>
```

This replaces the header button on desktop. The "AI Agent" nav link (`GET /`) remains for
navigating to the full-page chat. The slide-over trigger becomes a sidebar button.

### 10.5 Result

| Breakpoint | Header | Space Recovered |
|---|---|---|
| Desktop (`lg:`, ≥1024px) | Completely hidden | 64px (full header height) |
| Tablet/Mobile (`< lg`) | Thin 40px bar, hamburger + company name + avatar only | 24px vs current |

No functionality is lost — every header element is either moved to the sidebar or already
duplicated there.

### 10.6 Files to Change

| File | Change |
|------|--------|
| `web/templates/layouts/app_layout.templ` | Add `lg:hidden` to `<header>`, reduce to `h-10`, strip content |
| `web/templates/layouts/app_layout.templ` | Extend sidebar brand area with company/FY info |
| `web/templates/layouts/app_layout.templ` | Add Ask AI sidebar button (desktop only) |
| `web/templates/layouts/app_layout_templ.go` | Regenerate via `make generate` |

All changes are confined to a single file. No backend changes. No new CSS needed —
uses existing Tailwind utilities (`lg:hidden`, `h-10`, etc.).

---

## Implementation Order

Apply in this sequence — each phase is independently deployable:

| Phase | Changes | Files Touched | Impact |
|-------|---------|---------------|--------|
| **A** | Font + design tokens + tabular numbers | `input.css` only | High — global |
| **B** | `data-table` class + report templates | `input.css` + 4 report templates | High — core screens |
| **C** | `card`, `page-header`, `btn` components | `input.css` + all pages (class rename) | Medium — consistency |
| **D** | Form inputs | `input.css` + 4 wizard/form templates | Medium — usability |
| **E** | Chat markdown styles + typing indicator + action card button fix | `input.css` + `chat_home.templ` + `app_layout.templ` | High — primary interface |
| **F** | KPI cards + empty states + badges | `input.css` + dashboard + list templates | Medium — polish |
| **G** | Sidebar refinements | `app_layout.templ` | Low — cosmetic |
| **H** | Header elimination (desktop hidden, mobile thin) | `app_layout.templ` only | High — real estate |

Phases A → B deliver the majority of visible improvement. The rest is polish.

---

## What This Does NOT Include

- Responsive/mobile layout changes (separate work — requires layout restructuring)
- Dark mode (significant additional effort, deferred)
- Animations beyond typing indicator (not appropriate for accounting software)
- Chart.js visual improvements to dashboard charts (separate, minor)
- Login page redesign (low traffic, low priority)

---

## Build Step

After editing `input.css`, regenerate `app.css`:

```bash
make css
# or directly:
./tailwindcss.exe -i web/static/css/input.css -o web/static/css/app.css --minify
```

Run `make generate` first if any `.templ` files were also changed.
