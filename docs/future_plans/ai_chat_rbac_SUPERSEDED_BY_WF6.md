# AI Chat RBAC — Journal Entry Posting Authorization Plan

> **SUPERSEDED — DO NOT IMPLEMENT**
> This document has been fully incorporated into `docs/future_plans/WF6_journal_entry_approval.md`.
> Kept here for reference until WF6 is implemented and verified.
> Move to `docs/archive/` after WF6 is complete.

**Status:** Superseded by WF6
**Priority:** N/A — see WF6_journal_entry_approval.md
**Estimated scope:** N/A

---

## Problem Statement

The AI chat confirm endpoint (`POST /chat/confirm`) calls `CommitProposal` to post journal entries
without checking whether the authenticated user has permission to do so.

Any logged-in user — regardless of role — can currently confirm an AI-proposed journal entry.
This violates the principle that write operations require appropriate authorization. A read-only
user (e.g. a viewer or auditor) should be able to chat with the AI but must not be allowed to
post accounting entries.

---

## Current Flow (no RBAC enforcement)

```
User types natural language
  → POST /chat (SSE)          — authenticated, no role check
  → InterpretDomainAction     — proposes journal entry
  → pendingStore.set(token)   — stores proposal

User clicks "Confirm"
  → POST /chat/confirm        — authenticated, NO ROLE CHECK HERE
  → CommitProposal            — entry posted to ledger
```

---

## Proposed Flow (with RBAC enforcement)

```
User types natural language
  → POST /chat (SSE)          — authenticated, no role check (read is fine for all roles)
  → InterpretDomainAction     — proposes journal entry
  → pendingStore.set(token)   — stores proposal

User clicks "Confirm"
  → POST /chat/confirm        — authenticated
  → role check: user must have role "admin" or "accountant"
  → if denied: return 403 with JSON error {"error": "Insufficient permissions to post journal entries"}
  → if allowed: CommitProposal — entry posted to ledger
```

---

## Scope of Changes

### File 1: `internal/core/user_model.go`

Confirm (or add) a role constant for the roles that may post entries. Expected existing roles:

| Role | May post journal entries? |
|---|---|
| `admin` | Yes |
| `accountant` | Yes |
| `viewer` / `readonly` | No |

If a helper method `CanPostJournalEntry() bool` does not already exist on the `User` model,
add one here. Check the existing role constants before adding anything.

### File 2: `internal/adapters/web/ai.go`

In the `chatConfirm` handler (handles `POST /chat/confirm`):

1. Extract the authenticated user from the request context (via the existing `RequireAuth`
   middleware which already sets the user on the context).
2. Check `user.CanPostJournalEntry()` (or equivalent role check).
3. If the check fails, write a 403 JSON response and return. Do not call `CommitProposal`.

The role check belongs here — in the adapter layer — not in `ApplicationService` or domain
services. Authorization is an adapter-layer concern in this architecture.

### File 3: `internal/adapters/web/ai.go` — write tool confirmation (same handler)

The same `POST /chat/confirm` handler is also used to confirm write tools (create_vendor,
create_purchase_order, approve_po, etc.) via `ExecuteWriteTool`. These write tools should be
subject to the same or stricter role check. Ensure the RBAC check covers both paths
(journal entry commit AND write tool execution).

---

## Role Check Implementation

```go
// In chatConfirm handler, after extracting the pending action:

user := userFromContext(r.Context())  // existing helper
if !userCanConfirmActions(user) {
    writeError(w, r, "Insufficient permissions to confirm this action", "FORBIDDEN", http.StatusForbidden)
    return
}
```

Where `userCanConfirmActions` checks the user's role against the permitted set. Use the
existing role constants from `internal/core/user_model.go` — do not hardcode role strings
in the handler.

---

## UI Consideration

When the AI proposes an action (journal entry or write tool), the chat frontend renders an
action card with "Confirm" and "Cancel" buttons. If the user lacks permission to confirm,
the confirm button should either:

**Option A (server-enforced, simpler):** Show the confirm button regardless; the server returns
403 on click, and the UI displays an error message. No frontend changes needed.

**Option B (UI-enforced, better UX):** Include a `can_confirm` flag in the SSE event payload
for proposed actions. The frontend hides or disables the confirm button when `can_confirm`
is false, showing a tooltip like "You do not have permission to post entries."

Option A is recommended for the initial implementation. Option B is a follow-on UX improvement.

---

## What Does NOT Change

- `InterpretDomainAction` — unchanged
- `CommitProposal` — unchanged (no role parameter needed; check is in the adapter)
- `pendingStore` — unchanged
- `ApplicationService` interface — no role enforcement at this layer
- All read operations (searching accounts, browsing reports, chat queries) — no role check needed

---

## Testing Checklist (run after implementation)

- [ ] Log in as `admin` user → propose a journal entry → confirm → entry posts (200 OK)
- [ ] Log in as a `viewer` role user → propose a journal entry → confirm → 403 returned, entry NOT posted
- [ ] Log in as `viewer` → confirm a write tool (create_vendor etc.) → 403 returned
- [ ] Log in as `admin` → confirm a write tool → executes successfully
- [ ] `go build ./...` — must compile clean
- [ ] `go test ./internal/core -v` — all 70 tests must still pass (no service layer changes)

---

## Notes

- The existing `RequireAuth` middleware already validates the JWT and sets the user on the
  request context. The role check in `chatConfirm` simply reads from that context — no new
  middleware needed.
- No database migration required (user roles are already stored).
- This plan deliberately defers the Option B UI improvement to keep scope minimal.
