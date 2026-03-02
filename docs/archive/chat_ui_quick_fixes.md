# Chat UI Quick Fixes — Account Name Display + Amend Button

**Status:** Ready to implement
**Priority:** High — visible UX gaps, no risk
**Scope:** 2 independent fixes, ~4 files total
**Prerequisite:** None
**Relationship to WF6:** These fixes are RBAC-neutral. Role-aware button behaviour (Submit vs Post) belongs to WF6, not here.

---

## Fix 1 — Account Name Column in the Proposal Card

### Problem

The journal lines table in the proposal card shows only the account code (`5100`, `2000`).
Users who haven't memorised the chart of accounts cannot verify the entry without
navigating away to look up the account. This undermines the review step.

### Goal

Add a **Description** column between Account and Amount showing the account's full name,
e.g. `Rent Expense`, `Accounts Payable`.

```
Type  Account  Description            Amount
DR    5100     Rent Expense         1000 INR
CR    2000     Accounts Payable     1000 INR
```

### Implementation

#### Step 1 — New ApplicationService method

**File: `internal/app/service.go`** — add to interface:

```go
// GetAccountNamesByCode returns a map of account_code → name for the given company.
// Codes not found are silently omitted from the result.
GetAccountNamesByCode(ctx context.Context, companyCode string, codes []string) (map[string]string, error)
```

**File: `internal/app/app_service.go`** — add implementation:

```go
func (s *appService) GetAccountNamesByCode(ctx context.Context, companyCode string, codes []string) (map[string]string, error) {
    company, err := s.ledger.GetCompanyByCode(ctx, companyCode)
    if err != nil {
        return nil, err
    }
    rows, err := s.db.Query(ctx,
        `SELECT code, name FROM accounts WHERE company_id = $1 AND code = ANY($2)`,
        company.ID, codes,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    result := make(map[string]string, len(codes))
    for rows.Next() {
        var code, name string
        if err := rows.Scan(&code, &name); err != nil {
            return nil, err
        }
        result[code] = name
    }
    return result, rows.Err()
}
```

No migration needed — queries the existing `accounts` table.

#### Step 2 — Enriched SSE payload in chat.go

`core.Proposal` and `core.ProposalLine` must NOT be modified (ProposalLine is used in the
strict OpenAI JSON schema — adding fields would break schema generation).
Enrichment is web-adapter-only.

**File: `internal/adapters/web/chat.go`** — add local display types (unexported):

```go
type enrichedProposalLine struct {
    AccountCode string `json:"account_code"`
    AccountName string `json:"account_name"` // display only, not in core.ProposalLine
    IsDebit     bool   `json:"is_debit"`
    Amount      string `json:"amount"`
}

type enrichedProposal struct {
    DocumentTypeCode    string                 `json:"document_type_code"`
    CompanyCode         string                 `json:"company_code"`
    TransactionCurrency string                 `json:"transaction_currency"`
    ExchangeRate        string                 `json:"exchange_rate"`
    Summary             string                 `json:"summary"`
    PostingDate         string                 `json:"posting_date"`
    DocumentDate        string                 `json:"document_date"`
    Confidence          float64                `json:"confidence"`
    Reasoning           string                 `json:"reasoning"`
    Lines               []enrichedProposalLine `json:"lines"`
}
```

**In `chatMessage` handler (`chat.go:195-222`)**, after `InterpretEvent` returns a proposal,
collect all account codes, call `GetAccountNamesByCode`, build the enriched struct, and send it:

```go
case app.DomainActionKindJournalEntry:
    aiResult, err := h.svc.InterpretEvent(r.Context(), result.EventDescription, req.CompanyCode)
    // ... existing error and clarification handling unchanged ...

    // Enrich with account names (best-effort — fall back to empty string on error)
    codes := make([]string, len(aiResult.Proposal.Lines))
    for i, l := range aiResult.Proposal.Lines {
        codes[i] = l.AccountCode
    }
    nameMap, _ := h.svc.GetAccountNamesByCode(r.Context(), req.CompanyCode, codes)
    if nameMap == nil {
        nameMap = map[string]string{}
    }

    enriched := buildEnrichedProposal(aiResult.Proposal, nameMap)

    token := uuid.NewString()
    h.pending.put(token, pendingAction{
        Kind:      pendingKindJournalEntry,
        Proposal:  aiResult.Proposal,  // original — commit path is unchanged
        CreatedAt: time.Now(),
    })
    sendSSE(w, flusher, "proposal", map[string]any{
        "token":    token,
        "proposal": enriched,  // enriched — display path
    })
```

Add a helper:
```go
func buildEnrichedProposal(p *core.Proposal, names map[string]string) enrichedProposal {
    lines := make([]enrichedProposalLine, len(p.Lines))
    for i, l := range p.Lines {
        lines[i] = enrichedProposalLine{
            AccountCode: l.AccountCode,
            AccountName: names[l.AccountCode],
            IsDebit:     l.IsDebit,
            Amount:      l.Amount,
        }
    }
    return enrichedProposal{
        DocumentTypeCode:    p.DocumentTypeCode,
        CompanyCode:         p.CompanyCode,
        TransactionCurrency: p.TransactionCurrency,
        ExchangeRate:        p.ExchangeRate,
        Summary:             p.Summary,
        PostingDate:         p.PostingDate,
        DocumentDate:        p.DocumentDate,
        Confidence:          p.Confidence,
        Reasoning:           p.Reasoning,
        Lines:               lines,
    }
}
```

Key safety point: `pendingStore` still holds the original `*core.Proposal`.
`chatConfirm` calls `CommitProposal(*action.Proposal)` — completely unchanged.
The enriched struct is display-only and never touches the commit path.

#### Step 3 — Template update

**File: `web/templates/pages/chat_home.templ`**

Table header (around line 150):
```html
<tr class="bg-blue-50 border-b border-blue-100">
    <th class="text-left px-3 py-1.5 text-slate-500 font-medium w-10">Type</th>
    <th class="text-left px-3 py-1.5 text-slate-500 font-medium w-16">Account</th>
    <th class="text-left px-3 py-1.5 text-slate-500 font-medium">Description</th>
    <th class="text-right px-3 py-1.5 text-slate-500 font-medium">Amount</th>
</tr>
```

Table rows (around line 158):
```html
<td class="px-3 py-1.5 font-mono text-slate-700 w-16" x-text="line.account_code"></td>
<td class="px-3 py-1.5 text-slate-600 text-xs" x-text="line.account_name || '—'"></td>
<td class="px-3 py-1.5 font-mono text-right text-slate-800" x-text="..."></td>
```

Run `make generate` after template changes.

### Files Changed

| File | Change |
|---|---|
| `internal/app/service.go` | Add `GetAccountNamesByCode` to interface |
| `internal/app/app_service.go` | Implement `GetAccountNamesByCode` |
| `internal/adapters/web/chat.go` | Add local enriched types + enrichment call in `chatMessage` |
| `web/templates/pages/chat_home.templ` | Add Description column to proposal table |
| `web/templates/pages/chat_home_templ.go` | Regenerated by `make generate` |

### Risk

Zero. `core.Proposal`, `core.ProposalLine`, `chatConfirm`, and the commit path are not touched.
`GetAccountNamesByCode` is best-effort — if the DB call fails, names default to `""` and
`account_name || '—'` shows a dash gracefully.

---

## Fix 2 — Amend Button on the Proposal Card

### Problem

When the AI proposes an entry with a wrong account or amount, the user must Cancel and
re-type the entire description from scratch. This is unnecessary friction — the user
just wants to refine the same event.

### Goal

Add an **Amend** button that:
1. Cancels the current pending token (releases server-side memory)
2. Pre-fills the chat input with the original proposal summary for easy editing
3. Focuses the textarea so the user can immediately correct and re-send

No server round-trip is needed beyond the existing cancel endpoint.

### Implementation

**Template-only change in `web/templates/pages/chat_home.templ`.**

#### Add `amendAction` to the Alpine.js `chatHome()` function

```javascript
amendAction(msg) {
    // Pre-fill with the original event summary for easy editing
    this.input = (msg.proposal && msg.proposal.summary)
        ? 'Please revise: ' + msg.proposal.summary
        : '';
    // Release the server-side token (same as cancel)
    this.confirmAction(msg, 'cancel');
    // Focus the textarea
    this.$nextTick(() => {
        const ta = document.querySelector('textarea');
        if (ta) ta.focus();
    });
},
```

Place this alongside `confirmAction` in the `chatHome()` return object.

#### Add the Amend button to the proposal card action row

Replace the current two-button row (around `chat_home.templ:174`):

```html
<div x-show="msg.status === undefined || msg.status === 'pending'" class="flex gap-2">
    <button
        class="flex-1 px-3 py-1.5 border border-blue-300 text-slate-800 hover:text-slate-900 text-sm font-medium rounded-lg hover:bg-blue-100 transition-colors"
        x-on:click="confirmAction(msg, 'confirm')"
    >✓ Post Entry</button>
    <button
        class="px-3 py-1.5 border border-blue-300 text-blue-700 text-sm rounded-lg hover:bg-blue-100 transition-colors"
        x-on:click="amendAction(msg)"
    >✎ Amend</button>
    <button
        class="px-3 py-1.5 border border-blue-300 text-blue-700 text-sm rounded-lg hover:bg-blue-100 transition-colors"
        x-on:click="confirmAction(msg, 'cancel')"
    >✕ Cancel</button>
</div>
```

Run `make generate` after template changes.

### Files Changed

| File | Change |
|---|---|
| `web/templates/pages/chat_home.templ` | Add `amendAction` JS function + Amend button |
| `web/templates/pages/chat_home_templ.go` | Regenerated by `make generate` |

### Risk

Zero. No backend changes. The cancel flow is already reliable and tested.
The token expiry (15-min TTL + background purge) provides a second safety net even
if the cancel call is skipped.

---

## Implementation Order

Implement Fix 1 and Fix 2 independently. Fix 2 can be done in minutes.
Fix 1 requires the Go changes first, then the template. Both can be shipped in the
same commit or separately — they do not interact.

## What These Fixes Do NOT Change

- Role-based button visibility (Post vs Submit for Review) — that is **Phase WF6**
- The ACCOUNTANT 403 issue — that is **Phase WF6**
- `chatConfirm`, `pendingStore`, `CommitProposal` — untouched
- `core.Proposal`, `core.ProposalLine` — untouched
- `InterpretEvent`, `InterpretDomainAction` — untouched
- All 70 tests — no service layer changes, all must continue to pass

## Post-Implementation Checklist

- [ ] `go build ./...` — clean
- [ ] `go test ./internal/core -v` — all 70 tests pass
- [ ] Log in as any role → generate a proposal → account names appear in the table
- [ ] Log in as any role → generate a proposal → click Amend → input pre-filled, token cancelled
- [ ] Verify that clicking Post Entry after Amend on the same card returns 404 (token consumed)
