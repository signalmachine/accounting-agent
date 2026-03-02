# Phase WF6 — Journal Entry Approval Workflow

**Status:** Planned — implement after Chat UI Quick Fixes
**Priority:** High — closes a conceptual gap in the role model
**Nature:** New domain feature — new DB table, new service methods, new UI page
**Breakage risk:** Low — additive. Existing FINANCE_MANAGER direct-post path is unchanged.

---

## Why This Exists

The system has three roles: `ACCOUNTANT`, `FINANCE_MANAGER`, `ADMIN`.
The current state is:

| Role | Generates AI Proposal | Can Post Entry |
|---|---|---|
| ACCOUNTANT | Yes | No (403 from server) |
| FINANCE_MANAGER | Yes | Yes |
| ADMIN | Yes | Yes |

The ACCOUNTANT role is a dead end. They can generate a proposal and see a "Post Entry"
button that will always fail. There is no workflow for them to hand the proposal to
someone who can post it. This violates segregation of duties — a core accounting
internal control — and makes the ACCOUNTANT role nearly pointless.

**The fix:** ACCOUNTANT submits proposals for review. FINANCE_MANAGER/ADMIN reviews from
a queue and posts (or rejects). FINANCE_MANAGER/ADMIN retain the ability to post their
own proposals directly without going through the queue.

```
ACCOUNTANT workflow:
  Chat → AI proposal → "Submit for Review" → parked entry in DB
                                               ↓
FINANCE_MANAGER/ADMIN workflow:               Review Queue page
  Chat → AI proposal → "Post Entry"            → Post (commits to ledger)
  (direct, unchanged)                          → Reject (with optional note)
```

---

## Relationship with `ai_chat_rbac.md`

`docs/future_plans/ai_chat_rbac.md` describes overlapping concerns. Here is the current
status of each item in that document:

| Concern in ai_chat_rbac.md | Current Status | WF6 action |
|---|---|---|
| `/chat/confirm` has no role check | **Already fixed** — `RequireRole("FINANCE_MANAGER", "ADMIN")` at `handlers.go:130` | No change needed |
| Write tool confirmation (create_vendor etc.) has no role check | **Already fixed** — same `RequireRole` at `handlers.go:130` covers both paths | No change needed |
| Option A: server returns 403, UI shows error | **Current behaviour** for ACCOUNTANT | Replaced by WF6 |
| Option B: UI hides confirm button based on role | **Not yet done** | Implemented in WF6 |
| ACCOUNTANT listed as allowed to post | **Incorrect** — WF6 changes this: ACCOUNTANT submits, does not post | WF6 supersedes this |

**After WF6 is implemented, `ai_chat_rbac.md` should be moved to `docs/archive/`.**
WF6 fully resolves every concern in that document.

---

## Database

### Migration: `028_parked_journal_entries.sql`

```sql
CREATE TABLE IF NOT EXISTS parked_journal_entries (
    id               SERIAL PRIMARY KEY,
    company_id       INTEGER      NOT NULL REFERENCES companies(id),
    submitted_by     INTEGER      NOT NULL REFERENCES users(id),
    submitted_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    proposal_json    JSONB        NOT NULL,  -- full core.Proposal, for commit on approval
    summary          TEXT         NOT NULL,  -- denormalised for quick list display
    document_type    TEXT         NOT NULL,  -- denormalised (JE, SI, PI, etc.)
    status           TEXT         NOT NULL DEFAULT 'PENDING',
                                             -- PENDING | POSTED | REJECTED
    reviewed_by      INTEGER      REFERENCES users(id),
    reviewed_at      TIMESTAMPTZ,
    review_note      TEXT,
    journal_entry_id INTEGER      REFERENCES journal_entries(id)
);

CREATE INDEX IF NOT EXISTS idx_parked_je_company_status
    ON parked_journal_entries (company_id, status, submitted_at DESC);
```

All design notes:
- `proposal_json` stores the full `core.Proposal` as JSONB so the reviewer can inspect
  it and the commit can reconstruct it exactly without AI involvement.
- `summary` and `document_type` are denormalised to avoid JSONB extraction in list queries.
- `journal_entry_id` is set when status transitions to POSTED, providing an audit link.
- No soft-delete — entries move through PENDING → POSTED or PENDING → REJECTED.
  The status column is the full lifecycle record.
- `review_note` is nullable — only populated on REJECTED.

---

## Domain Model

### File: `internal/core/parked_entry_model.go` (new)

```go
package core

import "time"

// ParkedEntry is a journal entry proposal submitted by an ACCOUNTANT for review.
type ParkedEntry struct {
    ID             int
    CompanyID      int
    SubmittedBy    int    // user ID
    SubmittedByName string // denormalised for display
    SubmittedAt    time.Time
    Proposal       *Proposal
    Summary        string
    DocumentType   string
    Status         string // PENDING | POSTED | REJECTED
    ReviewedBy     *int   // nil if not yet reviewed
    ReviewedAt     *time.Time
    ReviewNote     string
    JournalEntryID *int
}

const (
    ParkedStatusPending  = "PENDING"
    ParkedStatusPosted   = "POSTED"
    ParkedStatusRejected = "REJECTED"
)
```

No new service interface in `internal/core` — the parked entry workflow is thin enough
to live entirely in the application service layer (`internal/app/`). It does not introduce
new domain invariants beyond what `Ledger.Commit` already enforces on posting.

---

## Application Service

### New request/result types in `internal/app/`

**`internal/app/request_types.go`** — add:

```go
type SubmitForReviewRequest struct {
    CompanyCode string
    UserID      int
    Proposal    core.Proposal
}

type RejectParkedEntryRequest struct {
    CompanyCode string
    EntryID     int
    ReviewerID  int
    Note        string // optional
}
```

**`internal/app/result_types.go`** — add:

```go
type ParkedEntryResult struct {
    ID           int
    Summary      string
    DocumentType string
    Status       string
    SubmittedBy  string
    SubmittedAt  time.Time
    ReviewedAt   *time.Time
    ReviewNote   string
    Proposal     *core.Proposal
}

type ParkedEntriesResult struct {
    Entries []ParkedEntryResult
}
```

### New methods on `ApplicationService` interface (`internal/app/service.go`)

```go
// SubmitForReview saves a proposal to the parked_journal_entries table for review.
// Called by ACCOUNTANT role after AI generates a proposal.
SubmitForReview(ctx context.Context, req SubmitForReviewRequest) (*ParkedEntryResult, error)

// GetParkedEntries returns all PENDING parked entries for a company, newest first.
// Called by FINANCE_MANAGER / ADMIN to populate the review queue.
GetParkedEntries(ctx context.Context, companyCode string) (*ParkedEntriesResult, error)

// PostParkedEntry commits a parked entry to the ledger and marks it POSTED.
// Called by FINANCE_MANAGER / ADMIN from the review queue.
PostParkedEntry(ctx context.Context, companyCode string, entryID, reviewerID int) error

// RejectParkedEntry marks a parked entry as REJECTED with an optional note.
// Called by FINANCE_MANAGER / ADMIN from the review queue.
RejectParkedEntry(ctx context.Context, req RejectParkedEntryRequest) error
```

### Implementation in `internal/app/app_service.go`

**`SubmitForReview`:**
- JSON-marshal the proposal into `proposal_json`
- INSERT into `parked_journal_entries` with status PENDING
- Return the new entry ID + summary

**`GetParkedEntries`:**
- SELECT from `parked_journal_entries` JOIN `users` (for submitter name)
  WHERE `company_id = ? AND status = 'PENDING'` ORDER BY `submitted_at DESC`
- Unmarshal `proposal_json` back into `*core.Proposal` for each row

**`PostParkedEntry`:**
- SELECT FOR UPDATE the parked entry (ensure it's still PENDING)
- Call `s.ledger.Commit(ctx, *entry.Proposal)` — same path as direct posting
- UPDATE status to POSTED, set `reviewed_by`, `reviewed_at`, `journal_entry_id`
- All in one transaction

**`RejectParkedEntry`:**
- UPDATE `parked_journal_entries` SET status='REJECTED', reviewed_by=?, reviewed_at=now(), review_note=?
  WHERE id=? AND company_id=? AND status='PENDING'
- Verify exactly one row was updated (optimistic concurrency — prevent double-reject)

---

## Web Layer

### New routes in `internal/adapters/web/handlers.go`

```go
// Chat — ACCOUNTANT submit for review
r.Post("/chat/submit", h.chatSubmit)

// Review queue — browser page (FINANCE_MANAGER / ADMIN)
r.With(h.RequireRoleBrowser("FINANCE_MANAGER", "ADMIN")).
    Get("/accounting/review-queue", h.reviewQueuePage)

// Review queue — API actions (FINANCE_MANAGER / ADMIN)
r.With(h.RequireRole("FINANCE_MANAGER", "ADMIN")).
    Get("/api/companies/{code}/review-queue", h.apiGetParkedEntries)
r.With(h.RequireRole("FINANCE_MANAGER", "ADMIN")).
    Post("/api/companies/{code}/review-queue/{id}/post", h.apiPostParkedEntry)
r.With(h.RequireRole("FINANCE_MANAGER", "ADMIN")).
    Post("/api/companies/{code}/review-queue/{id}/reject", h.apiRejectParkedEntry)
```

### New handler: `chatSubmit` in `internal/adapters/web/chat.go`

```go
// chatSubmit handles POST /chat/submit — called by ACCOUNTANT to park a proposal.
func (h *Handler) chatSubmit(w http.ResponseWriter, r *http.Request) {
    var req chatConfirmRequest // reuses Token field
    if !decodeJSON(w, r, &req) { return }

    claims := authFromContext(r.Context())
    action, ok := h.pending.get(req.Token)
    if !ok {
        writeError(w, r, "token not found or expired", "NOT_FOUND", http.StatusNotFound)
        return
    }
    if action.Kind != pendingKindJournalEntry {
        writeError(w, r, "token does not refer to a journal entry", "BAD_REQUEST", http.StatusBadRequest)
        return
    }
    if !h.requireCompanyAccess(w, r, action.CompanyCode) { return }
    h.pending.delete(req.Token)

    result, err := h.svc.SubmitForReview(r.Context(), app.SubmitForReviewRequest{
        CompanyCode: action.CompanyCode,
        UserID:      claims.UserID,
        Proposal:    *action.Proposal,
    })
    if err != nil {
        writeError(w, r, err.Error(), "SUBMIT_ERROR", http.StatusUnprocessableEntity)
        return
    }
    writeJSON(w, map[string]any{
        "ok":      true,
        "id":      result.ID,
        "message": "Entry submitted for review. A Finance Manager will post it.",
    })
}
```

Note: `chatSubmit` is NOT restricted by `RequireRole` at the router — any authenticated
user with company access can submit. The `/chat/confirm` endpoint continues to require
FINANCE_MANAGER/ADMIN as before.

### New handlers file: `internal/adapters/web/review_queue.go`

Handles the three review queue API endpoints and the browser page. Each API handler:
- Validates company access
- Calls the corresponding `ApplicationService` method
- Returns JSON

### New template: `web/templates/pages/review_queue.templ`

**Page: `/accounting/review-queue`**

Layout: `AppLayout` (sidebar visible, nav item: Accounting → Review Queue)

Content:
- Heading: "Journal Entry Review Queue"
- Badge showing count of pending entries in the sidebar nav link
- Table of pending entries:
  - Submitted by / date
  - Document type badge (JE, SI, PI)
  - Summary text
  - Line count (DR/CR)
  - Actions: "Post Entry" button + "Reject" button
- Expandable row (Alpine.js) showing full journal lines table
  (same layout as proposal card, with account names — reuse `GetAccountNamesByCode`)
- Reject modal: text area for optional rejection note + confirm/cancel

**Empty state:** "No entries pending review."

---

## UI Changes to `chat_home.templ`

### Step 1 — Inject role into the Alpine.js component

**`web/templates/layouts/app_layout.templ`** — add `data-role` alongside `data-company-code`:

```html
<body
    ...
    data-company-code={ d.CompanyCode }
    data-role={ d.Role }
    ...
>
```

This is an additive change — no existing functionality is affected.

### Step 2 — Read role in `chatHome()` Alpine.js component

```javascript
const ROLE = document.body.dataset.role || '';
```

### Step 3 — Role-aware proposal card action row

Replace the current two-button row with role-conditional rendering:

```html
<div x-show="msg.status === undefined || msg.status === 'pending'" class="flex gap-2">
    <!-- FINANCE_MANAGER / ADMIN: direct post path (unchanged) -->
    <template x-if="canPost()">
        <button class="flex-1 ..." x-on:click="confirmAction(msg, 'confirm')">
            ✓ Post Entry
        </button>
    </template>

    <!-- ACCOUNTANT: submit for review -->
    <template x-if="!canPost()">
        <button class="flex-1 ..." x-on:click="submitForReview(msg)">
            ↑ Submit for Review
        </button>
    </template>

    <!-- Amend button (all roles — from Quick Fix 2) -->
    <button class="..." x-on:click="amendAction(msg)">✎ Amend</button>

    <!-- Cancel (all roles) -->
    <button class="..." x-on:click="confirmAction(msg, 'cancel')">✕ Cancel</button>
</div>

<!-- Submitted state -->
<div x-show="msg.status === 'submitted'" class="text-sm text-blue-700 font-medium">
    ↑ Submitted for review. A Finance Manager will post it.
</div>
```

### Step 4 — New Alpine.js functions in `chatHome()`

```javascript
canPost() {
    return ROLE === 'FINANCE_MANAGER' || ROLE === 'ADMIN';
},

async submitForReview(msg) {
    msg.status = 'submitting';
    try {
        const resp = await fetch('/chat/submit', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token: msg.token, action: 'submit' }),
        });
        const data = await resp.json();
        if (resp.ok && data.ok) {
            msg.status = 'submitted';
        } else {
            msg.status = 'error';
            msg.resultText = data.error || 'Failed to submit.';
        }
    } catch (e) {
        msg.status = 'error';
        msg.resultText = 'Network error.';
    }
    this.saveHistory();
},
```

---

## Sidebar Navigation Update

Add "Review Queue" to the Accounting section in `app_layout.templ`, visible to
FINANCE_MANAGER and ADMIN only:

```go
if d.Role == "FINANCE_MANAGER" || d.Role == "ADMIN" {
    // render Review Queue nav link with pending count badge
}
```

The pending count badge requires a separate lightweight API call on page load or can
be omitted in the first iteration (link without badge).

---

## What Stays Completely Unchanged

| Component | Status |
|---|---|
| `chatConfirm` handler | Unchanged — FINANCE_MANAGER/ADMIN direct-post path |
| `pendingStore` | Unchanged — same TTL/token logic |
| `CommitProposal` | Unchanged — called identically from both `chatConfirm` and `PostParkedEntry` |
| `InterpretEvent` / `InterpretDomainAction` | Frozen — zero changes |
| Action card confirm (create_vendor, approve_po, etc.) | Unchanged |
| All 70 integration tests | Must continue to pass — no domain service changes |
| REPL / CLI | Unchanged — REPL has no role concept |

---

## Implementation Sequence

Follow this order strictly. Each step is independently verifiable.

1. **Migration** — add `028_parked_journal_entries.sql`, run `go run ./cmd/verify-db`
2. **Model** — add `internal/core/parked_entry_model.go`
3. **App service** — add request/result types, add 4 methods to interface + implementation
   Run `go build ./...` to verify no interface mismatches
4. **`chatSubmit` handler** — add to `chat.go`, register route in `handlers.go` (no role guard on route)
5. **Review queue handlers** — add `review_queue.go` with page + 3 API handlers + routes
6. **`review_queue.templ`** — new template, run `make generate`
7. **`app_layout.templ`** — add `data-role` attribute, run `make generate`
8. **`chat_home.templ`** — role-aware proposal card buttons + `submitForReview` + `canPost`,
   run `make generate`
9. **CSS build** — run `make css` if any new Tailwind classes are added
10. **End-to-end test** — manual walkthrough of both workflows (see Testing section)

---

## Testing Checklist

### Automated (run after Step 3)
- [ ] `go build ./...` — clean
- [ ] `go test ./internal/core -v` — all 70 tests pass

### Manual — ACCOUNTANT workflow
- [ ] Log in as ACCOUNTANT → chat → describe a business event → AI proposes entry
- [ ] Proposal card shows "Submit for Review" (not "Post Entry")
- [ ] Click "Submit for Review" → card shows "Submitted for review" message
- [ ] Verify row appears in `parked_journal_entries` table with status PENDING
- [ ] Clicking "Submit for Review" a second time with the same token → 404 (token consumed)

### Manual — FINANCE_MANAGER review workflow
- [ ] Log in as FINANCE_MANAGER → navigate to `/accounting/review-queue`
- [ ] Submitted entry is visible with correct summary, submitter name, date
- [ ] Expand row → full journal lines visible with account names
- [ ] Click "Post Entry" → entry posted, status changes to POSTED, entry disappears from queue
- [ ] Verify journal entry in trial balance
- [ ] Submit another entry as ACCOUNTANT → log in as FINANCE_MANAGER → click "Reject" →
      enter rejection note → entry disappears from queue, status is REJECTED in DB

### Manual — FINANCE_MANAGER direct post (must remain unchanged)
- [ ] Log in as FINANCE_MANAGER → chat → describe event → proposal shows "Post Entry"
- [ ] Click "Post Entry" → entry posts immediately (no queue involvement)

### Manual — ACCOUNTANT cannot access direct post
- [ ] As ACCOUNTANT, manually POST to `/chat/confirm` with a valid proposal token →
      403 returned (RequireRole middleware unchanged)

### Manual — Security
- [ ] As ACCOUNTANT, POST to `/api/companies/{code}/review-queue/{id}/post` → 403
- [ ] As ACCOUNTANT, POST to `/api/companies/{code}/review-queue/{id}/reject` → 403

---

## Archive Instruction

After WF6 is successfully implemented and tested:
- Move `docs/future_plans/ai_chat_rbac.md` → `docs/archive/ai_chat_rbac.md`
- Add note at top of archived file: "Superseded by Phase WF6 (implemented [date])."
