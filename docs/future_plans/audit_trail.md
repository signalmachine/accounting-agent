# Audit Trail Implementation Plan

**Status:** Planned — not started
**Priority:** Medium
**Effort:** ~1–2 days
**Depends on:** Nothing — purely additive work

---

## Overview

Two independent audit gaps exist in the current system:

1. **Database audit trail** — `created_by_user_id` columns were added in migration 018 but are never populated. No audit log table exists for mutations.
2. **AI agent audit trail** — `InterpretDomainAction` and `InterpretEvent` are black boxes at runtime. Tool calls, loop iterations, outcomes, and errors are not persisted.

Both gaps are well-understood patterns. Neither requires architectural redesign — this is additive work.

---

## Part 1: Database Audit Trail

### Current State

Migration `018_audit_trail_columns.sql` added `created_by_user_id INTEGER REFERENCES users(id)` to:
- `journal_entries`
- `sales_orders`
- `documents`

None of the INSERT statements in `internal/core/ledger.go` or `internal/core/order_service.go` populate this column. It is always NULL.

No dedicated audit log table exists. No PostgreSQL triggers.

### What to Build

#### 1a. Populate existing audit columns

Thread `userID int` through the call stack so inserts can record who triggered the action.

**Changes required:**

`internal/core/ledger.go` — `Commit` and `CommitInTx` signatures:
```go
// Before
func (l *Ledger) Commit(ctx context.Context, p Proposal) (int64, error)

// After
func (l *Ledger) Commit(ctx context.Context, p Proposal, userID int) (int64, error)
```

Add `userID` to the `journal_entries` INSERT:
```sql
INSERT INTO journal_entries (company_id, narration, posting_date, document_date,
    reasoning, reference_type, reference_id, idempotency_key, created_by_user_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
```

Same pattern for `sales_orders` INSERT in `internal/core/order_service.go` and the `documents` INSERT in `internal/core/ledger.go`.

`ApplicationService` methods already receive context with JWT claims — extract `userID` from context at the app layer and pass it down. Do not change domain service constructors to hold userID state.

**Files touched:** `internal/core/ledger.go`, `internal/core/order_service.go`, `internal/app/app_service.go`, `internal/adapters/web/` (where context carries claims).

#### 1b. Add a general-purpose audit log table

New migration: `028_audit_log.sql`

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id              BIGSERIAL PRIMARY KEY,
    company_id      INTEGER NOT NULL REFERENCES companies(id),
    user_id         INTEGER REFERENCES users(id),
    action          TEXT NOT NULL,          -- e.g. 'CREATE_ORDER', 'COMMIT_JE', 'APPROVE_PO'
    entity_type     TEXT NOT NULL,          -- e.g. 'sales_order', 'journal_entry', 'purchase_order'
    entity_id       BIGINT,                 -- the PK of the affected row
    detail          JSONB,                  -- arbitrary context (old/new values, amounts, etc.)
    ip_address      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_company_created ON audit_log (company_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_entity ON audit_log (entity_type, entity_id);
```

Write a thin `AuditLogger` in `internal/core/` or `internal/db/`:

```go
type AuditLogger struct { db *pgxpool.Pool }

func (a *AuditLogger) Log(ctx context.Context, companyID, userID int, action, entityType string, entityID int64, detail map[string]any) error
```

Call it from `ApplicationService` after successful mutations (create order, commit JE, approve PO, pay vendor, etc.). Fire-and-forget with a logged error on failure — never block the business transaction.

#### Scope of audit log entries (suggested initial set)

| Action | Entity |
|---|---|
| `CREATE_ORDER` | `sales_order` |
| `CONFIRM_ORDER` | `sales_order` |
| `SHIP_ORDER` | `sales_order` |
| `INVOICE_ORDER` | `sales_order` |
| `RECORD_PAYMENT` | `sales_order` |
| `COMMIT_JE` | `journal_entry` |
| `CREATE_PO` | `purchase_order` |
| `APPROVE_PO` | `purchase_order` |
| `RECEIVE_PO` | `purchase_order` |
| `RECORD_VENDOR_INVOICE` | `purchase_order` |
| `PAY_VENDOR` | `purchase_order` |
| `CREATE_VENDOR` | `vendor` |
| `CREATE_USER` | `user` |
| `UPDATE_USER_ROLE` | `user` |
| `SET_USER_ACTIVE` | `user` |
| `REGISTER_COMPANY` | `company` |

---

## Part 2: AI Agent Audit Trail

### Current State

`internal/ai/agent.go` logs only:
- Token usage (`log.Printf` at end of each call)
- OpenAI API errors (status code + raw dump)

Nothing is logged about tool calls, loop iterations, outcomes, clarification requests, or validation failures. `internal/ai/` has no database access.

### What to Build

#### 2a. Inline tool call logging (stdout — immediate, low effort)

Add `log.Printf` calls inside the agentic loop in `InterpretDomainAction`:

```go
// Before tool execution
log.Printf("[AI] tool_call iter=%d tool=%s args=%s", iter, fc.Name, fc.Arguments)

// After tool execution
log.Printf("[AI] tool_result iter=%d tool=%s ok=%v result_len=%d", iter, fc.Name, handlerErr == nil, len(resultStr))

// Loop exit
log.Printf("[AI] loop_exit iter=%d outcome=%s", iter, result.Kind)
```

This gives immediate runtime visibility with zero schema changes. Implement this first.

**Files touched:** `internal/ai/agent.go` only.

#### 2b. Persistent AI audit table (full history)

New migration: `029_ai_audit_log.sql`

```sql
CREATE TABLE IF NOT EXISTS ai_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    session_id      UUID NOT NULL,          -- generated per InterpretDomainAction call
    company_id      INTEGER NOT NULL REFERENCES companies(id),
    user_id         INTEGER REFERENCES users(id),
    input_text      TEXT NOT NULL,
    has_attachment  BOOLEAN NOT NULL DEFAULT FALSE,
    tool_calls      JSONB,                  -- array of {iter, tool, args, result, error, duration_ms}
    outcome_kind    TEXT NOT NULL,          -- 'journal_entry', 'write_tool', 'clarification', 'route_to_je', 'error'
    outcome_detail  JSONB,                  -- proposal summary, tool name, clarification text, error message
    iterations      INTEGER NOT NULL DEFAULT 0,
    tokens_input    INTEGER,
    tokens_output   INTEGER,
    error_message   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_audit_log_company ON ai_audit_log (company_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_audit_log_user ON ai_audit_log (user_id);
CREATE INDEX IF NOT EXISTS idx_ai_audit_log_outcome ON ai_audit_log (outcome_kind);
```

**Architecture approach:** Keep `internal/ai/` free of DB access. Instead:

1. `InterpretDomainAction` returns an `AuditRecord` alongside its existing `DomainActionResult`. The AI layer assembles the record (session_id, tool_calls JSONB, outcome, tokens) without touching the DB.
2. `ApplicationService.InterpretDomainAction` persists the record after the call returns.

This keeps the layering clean — AI layer has no infrastructure dependency; the app layer owns persistence.

**Files touched:** `internal/ai/agent.go` (assemble record), `internal/app/app_service.go` (persist record), new migration.

#### 2c. InterpretEvent logging

Apply the same inline `log.Printf` pattern to `InterpretEvent` for token usage and proposal outcome. This path is frozen per policy — no structural changes, logging only.

---

## Implementation Order

Do these in sequence. Each step is independently useful.

| Step | Work | Effort |
|---|---|---|
| **1** | Add `log.Printf` tool call logging in `agent.go` | ~1 hour |
| **2** | Thread `userID` into `Ledger.Commit` and key INSERTs | ~2–3 hours |
| **3** | Migration 028: `audit_log` table + `AuditLogger` | ~half day |
| **4** | Wire `AuditLogger` into `ApplicationService` for all mutations | ~2–3 hours |
| **5** | Migration 029: `ai_audit_log` table | ~1 hour |
| **6** | Assemble `AuditRecord` in `InterpretDomainAction`, persist in app layer | ~half day |

Steps 1–2 can be done in a single session. Steps 3–4 together, 5–6 together.

---

## Constraints and Rules

- **AI Agent Change Policy applies**: Steps 1 and 6 touch `internal/ai/agent.go`. Follow the policy — run `go run ./cmd/verify-agent` and full integration suite before and after. Invoke `/openai-integration` skill before touching any OpenAI SDK code.
- **`InterpretEvent` is frozen**: Only add `log.Printf` calls — no structural changes, no new parameters.
- **Ledger immutability**: `Ledger.Commit` signature change is permitted here because it adds an optional audit parameter, not business logic. The immutability rule (no UPDATEs, compensating entries only) is unaffected.
- **Fire-and-forget audit writes**: Audit log failures must never fail the business transaction. Log the error and continue.
- **Test DB**: Apply migrations 028–029 to test DB before running integration tests.
- **No new integration test requirement**: Audit logging is infrastructure — unit-test `AuditLogger` in isolation. Existing 70 integration tests must still pass after threading `userID` through signatures.
