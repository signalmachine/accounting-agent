# Reporting Fixes — Implementation Plan (2026-03-03)

Two architectural corrections. Both stem from the same root principle:

> **Ledger answers financial questions. Workflow tables answer operational questions.
> Never cross that boundary.**

---

## Change 1 — Drop Materialized Views; Query `journal_lines` Directly

### Problem

`GetTrialBalance` reads from `mv_trial_balance` (a materialized view). All other
reports (`GetBalanceSheet`, `GetProfitAndLoss`, `GetAccountStatement`) query
`journal_lines` directly and are always current. The trial balance is therefore
silently stale until someone clicks "Refresh Views" — a workflow footgun that will
cause recurring support confusion.

`mv_account_period_balances` (migration 014) has the same problem and is already
unused in application code (it is only refreshed, never read).

Both views must go.

### Scope

**Migration 028** — one new file `migrations/028_drop_reporting_views.sql`:

```sql
-- Drop the two materialized views that are being replaced by direct queries.
-- Idempotent: IF EXISTS guards.
DROP MATERIALIZED VIEW IF EXISTS mv_trial_balance;
DROP MATERIALIZED VIEW IF EXISTS mv_account_period_balances;

-- Indexes to support the direct-query trial balance aggregation.
-- journal_lines(account_id) covers the GROUP BY in the new trial balance query.
-- journal_entries(company_id, posting_date) covers company-scoped date filters.
CREATE INDEX IF NOT EXISTS idx_journal_lines_account_id
    ON journal_lines (account_id);

CREATE INDEX IF NOT EXISTS idx_journal_entries_company_posting
    ON journal_entries (company_id, posting_date);
```

**`internal/app/app_service.go` — rewrite `GetTrialBalance`**

Replace the `mv_trial_balance` read with a direct aggregation identical in
structure to the `GetBalanceSheet` subquery:

```go
func (s *appService) GetTrialBalance(ctx context.Context, companyCode string) (*TrialBalanceResult, error) {
    var companyID int
    var companyName, currency string
    if err := s.pool.QueryRow(ctx,
        "SELECT id, name, base_currency FROM companies WHERE company_code = $1", companyCode,
    ).Scan(&companyID, &companyName, &currency); err != nil {
        return nil, fmt.Errorf("company %s not found: %w", companyCode, err)
    }

    rows, err := s.pool.Query(ctx, `
        SELECT a.code, a.name,
               COALESCE(SUM(jl.debit_base), 0) - COALESCE(SUM(jl.credit_base), 0) AS net_balance
        FROM accounts a
        JOIN companies c ON c.id = a.company_id
        LEFT JOIN journal_lines jl ON jl.account_id = a.id
        LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.company_id = $1
        WHERE c.id = $1
        GROUP BY a.code, a.name
        HAVING COALESCE(SUM(jl.debit_base), 0) - COALESCE(SUM(jl.credit_base), 0) <> 0
        ORDER BY a.code
    `, companyID)
    // ... rest unchanged
```

The `HAVING` clause suppresses zero-balance accounts, preserving the existing
behaviour of the MV-based query.

**Remove `RefreshViews` entirely** — it has no purpose once the MVs are dropped.

Files touched:

| File | Change |
|------|--------|
| `internal/core/reporting_service.go` | Remove `RefreshViews` from `ReportingService` interface and `reportingService` implementation |
| `internal/app/service.go` | Remove `RefreshViews` from `ApplicationService` interface |
| `internal/app/app_service.go` | Remove `RefreshViews` delegation method |
| `internal/adapters/repl/repl.go` | Remove `case "refresh":` block |
| `internal/adapters/web/accounting.go` | Remove `apiRefreshViews` handler |
| `internal/adapters/web/handlers.go` | Remove `POST .../reports/refresh` route |
| `web/templates/pages/trial_balance.templ` | Remove "↻ Refresh Views" button and its `hx-post` |
| `web/templates/pages/trial_balance_templ.go` | Regenerate via `make generate` |

Remove the `refresh_views` AI tool from `buildToolRegistry` in `app_service.go`.

### Performance Note

At SMB scale (< 1 M journal lines), the new direct query with
`idx_journal_lines_account_id` will aggregate in milliseconds. If this system ever
reaches a scale where trial balance latency matters, the right solution is a
materialized view updated by a trigger or a background job — not a manually
refreshed snapshot with no staleness indicator.

---

## Change 2 — Fix AP Balance Boundary Violation

### Problem

`get_ap_balance` (AI tool, `app_service.go:1107`) queries `purchase_orders` for
rows with `status = 'INVOICED'`. This is an operational metric — it tells you how
many outstanding PO invoices exist. It does **not** tell you what the AP ledger
account actually shows.

The two will diverge whenever:
- A manual AP journal entry is posted (correction, non-PO invoice)
- A payment journal entry is posted outside the PO workflow
- Any workflow-table/ledger reconciliation gap exists

When a user asks "What is the AP balance?", they are asking a financial question.
Financial questions get ledger answers.

### Scope

**Rename the existing function** — it answers a valid operational question; it
just has an incorrect name:

In `internal/app/app_service.go`:
- Rename `getAPBalanceJSON` → `getOutstandingVendorInvoicesJSON`
- Rename the corresponding AI tool from `get_ap_balance` to
  `get_outstanding_vendor_invoices` with description:
  > "Get the count and total amount of vendor invoices that have been recorded
  > but not yet paid (purchase orders in INVOICED status). This is an operational
  > metric, not the accounting AP balance."

**Add a correct `get_ap_balance` tool** backed by a new `getAPBalanceJSON`
function that:

1. Resolves the AP account code from `account_rules` for the company
   (`rule_type = 'AP'`). This makes it company-agnostic — no hardcoded `'2000'`.
2. Queries `journal_lines` for the net balance of that account.
3. Returns the balance as a **positive credit amount** (negates the
   `debit - credit` raw value, since AP is credit-normal).

```go
func (s *appService) getAPBalanceJSON(ctx context.Context, companyCode string) (string, error) {
    // Step 1: resolve AP account code from account_rules.
    var apAccountCode string
    err := s.pool.QueryRow(ctx, `
        SELECT ar.account_code FROM account_rules ar
        JOIN companies c ON c.id = ar.company_id
        WHERE c.company_code = $1 AND ar.rule_type = 'AP'
    `, companyCode).Scan(&apAccountCode)
    if err != nil {
        return `{"error":"AP account rule not configured for this company"}`, nil
    }

    // Step 2: aggregate from journal_lines (all-time, company-scoped).
    var accountName string
    var netDebit decimal.Decimal
    err = s.pool.QueryRow(ctx, `
        SELECT a.name,
               COALESCE(SUM(jl.debit_base), 0) - COALESCE(SUM(jl.credit_base), 0)
        FROM accounts a
        JOIN companies c ON c.id = a.company_id
        LEFT JOIN journal_lines jl ON jl.account_id = a.id
        LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.company_id = c.id
        WHERE c.company_code = $1 AND a.code = $2
        GROUP BY a.name
    `, companyCode, apAccountCode).Scan(&accountName, &netDebit)
    if err != nil {
        return fmt.Sprintf(`{"error":"AP account %s not found or has no activity"}`, apAccountCode), nil
    }

    // Step 3: negate — AP is credit-normal; positive result = amount owed.
    apBalance := netDebit.Neg()

    data, _ := json.Marshal(map[string]any{
        "ap_account_code": apAccountCode,
        "ap_account_name": accountName,
        "ap_balance":      apBalance.StringFixed(2),
        "note":            "Ledger AP balance from journal_lines. Positive = amount owed to vendors.",
    })
    return string(data), nil
}
```

Update the tool registration in `buildToolRegistry`:

```go
// Renamed — operational metric, not the accounting balance.
registry.Register(ai.ToolDefinition{
    Name:        "get_outstanding_vendor_invoices",
    Description: "Get the count and total of vendor invoices recorded but not yet paid (POs in INVOICED status). Operational metric — use get_ap_balance for the accounting balance.",
    IsReadTool:  true,
    // ... same InputSchema and Handler as the old get_ap_balance
})

// New — ledger-sourced AP balance.
registry.Register(ai.ToolDefinition{
    Name:        "get_ap_balance",
    Description: "Get the Accounts Payable balance from the ledger (journal_lines). Returns the net credit balance of the AP account. Use this for any financial question about how much is owed to vendors.",
    IsReadTool:  true,
    InputSchema: map[string]any{
        "type":                 "object",
        "additionalProperties": false,
        "properties":           map[string]any{},
        "required":             []string{},
    },
    Handler: func(hctx context.Context, params map[string]any) (string, error) {
        return s.getAPBalanceJSON(hctx, companyCode)
    },
})
```

The existing `get_account_balance` tool remains unchanged and can still be used
to query any account by code, including the AP account directly if needed.

---

## Verification Checklist

After implementing both changes:

- [ ] `go build ./...` clean
- [ ] `go test ./internal/core -v` — all 70 tests pass
- [ ] Trial balance page loads without "Refresh Views" button and shows correct
      current balances immediately after posting a journal entry
- [ ] `get_ap_balance` AI tool returns a value matching the AP account in the
      trial balance report
- [ ] `get_outstanding_vendor_invoices` returns PO-based count (may differ from
      ledger AP — that is expected and correct)
- [ ] REPL `/bal` no longer shows a `/refresh` command in `/help`
- [ ] `go run ./cmd/verify-db` applies migration 028 cleanly (idempotent on
      re-run)

---

## What Is Not Changing

- `GetBalanceSheet`, `GetProfitAndLoss`, `GetAccountStatement` — already correct,
  no changes needed.
- `get_account_balance` AI tool — already reads `journal_lines`, no changes.
- `get_vendor_payment_history` — operational history, correctly sourced from
  `purchase_orders`.
- The `account_rules` table and `RuleEngine` — unchanged.
