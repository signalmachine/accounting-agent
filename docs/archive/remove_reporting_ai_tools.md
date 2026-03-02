# Remove Standard Report AI Tools — Implementation Plan

**Status:** Pending (not yet implemented)
**Priority:** Medium — correctness and UX improvement
**Estimated scope:** 2 files, ~50 lines changed

---

## Problem Statement

The AI agent chat (at `/`) currently has four AI tools registered that duplicate standard reports
already available under the **Reports** section of the web UI:

| AI Tool | Equivalent Report Page |
|---|---|
| `get_pl_report` | `/reports/pl` (Profit & Loss) |
| `get_balance_sheet` | `/reports/balance-sheet` (Balance Sheet) |
| `get_account_statement` | `/reports/statement` (Account Statement) |
| `refresh_views` | Refresh button on any report page |

In addition, **Trial Balance** has no AI tool at all — but the AI has no routing instruction to
redirect the user to the report page. Instead, it fabricates a response using its training
knowledge (a hallucinated subset of accounts, without the debit/credit split, and always showing
as "unbalanced"). This was observed in production:

- **AI chat response:** 5 accounts, net-balance column, no debit/credit split, shows negative
  balances for liability/equity accounts (incorrect accounting presentation).
- **Reports → Trial Balance page:** 10 accounts, proper DEBIT/CREDIT columns, "Balanced — Debit
  total equals Credit total" confirmed, materialized view backed.

The AI cannot do better than the dedicated report pages for these standard reports because:
1. The report pages use a proper materialized view (`mv_trial_balance`) or optimised SQL with
   correct debit/credit presentation logic.
2. The AI tools for P&L/BS/Statement call the same underlying SQL but return raw JSON which the
   AI must narrate — losing formatting, the balance check, currency display, and CSV export.
3. For trial balance, the AI has no tool at all, so it hallucinates.

**Conclusion:** Standard financial reports should always be accessed via the Reports section.
The AI agent should redirect users there rather than attempting to render or fabricate them.

---

## Scope of Changes

### File 1: `internal/app/app_service.go`

**Action:** Remove 4 tool registrations from `buildToolRegistry()`.

Tools to delete (locate by name in the `ToolDefinition` structs):

1. `get_account_statement` — located around line 814–843
2. `get_pl_report` — located around line 845–869
3. `get_balance_sheet` — located around line 871–890
4. `refresh_views` — located around line 892–908

Also delete the three JSON-adapter helper functions at the bottom of the file that these tools
call (they become dead code after removal):

- `getAccountStatementJSON()` — around line 1856–1891
- `getPLReportJSON()` — around line 1894–1921
- `getBalanceSheetJSON()` — around line 1925–1956

> **Do not remove** `get_account_balance` — it returns a single account's current balance
> inline, which is genuinely useful in chat context and has no dedicated report page.

### File 2: `internal/ai/agent.go`

**Action:** Extend the system prompt in `InterpretDomainAction` (around line 253–269) to add an
explicit REPORTS ROUTING section that covers all five standard reports.

Current system prompt ends with:

```
Today's date: %s
```

Replace or extend the ROUTING RULES section as follows (exact wording for the new block):

```
STANDARD REPORTS — never generate these yourself, always redirect:
The following reports are available in the web UI under the "Reports" menu. When the user asks
for any of these, respond with a short plain-text message directing them to the correct page.
Do NOT call any tool. Do NOT attempt to compute or narrate the data yourself.

- Trial Balance      → "You can view the full Trial Balance under Reports → Trial Balance.
                        It shows all accounts with debit/credit columns and a balance check."
- Profit & Loss      → "The Profit & Loss report is at Reports → Profit & Loss.
                        You can filter by year and month there."
- Balance Sheet      → "The Balance Sheet is at Reports → Balance Sheet.
                        You can choose an as-of date to see a point-in-time snapshot."
- Account Statement  → "Account statements are at Reports → Account Statement.
                        Enter an account code and optional date range to get the ledger history."
- Refresh Views      → "To refresh the materialized views, use the Refresh Views button at the
                        top-right of any report page."

These redirects apply regardless of how the user phrases the request (e.g. "show me the P&L",
"what is the trial balance", "give me a balance sheet as of today").
```

The updated full routing block should read:

```go
systemPrompt := fmt.Sprintf(`You are an expert business assistant for %s (%s, base currency: %s).

You have access to tools to look up accounts, customers, products, stock levels, and warehouses.
Use read tools to gather the information you need before responding.

ROUTING RULES — follow these exactly:
1. If the user asks a question about accounts, customers, products, or stock: call the relevant
   read tools and provide a clear answer.
2. If the user is describing a financial accounting event (recording a payment, posting an
   expense, booking a journal entry, recording revenue): call route_to_journal_entry with the
   event description.
3. If you need more information before you can help: call request_clarification with a specific
   question.
4. If you have gathered enough information via read tools: respond with a plain-text answer.

STANDARD REPORTS — redirect to the Reports section, do not generate yourself:
When the user asks for any of the following reports, respond with a short redirect message.
Do NOT call any tool. Do NOT attempt to compute or narrate the data yourself.

- Trial Balance:     Direct to Reports → Trial Balance
- Profit & Loss:     Direct to Reports → Profit & Loss (filterable by year/month)
- Balance Sheet:     Direct to Reports → Balance Sheet (filterable by date)
- Account Statement: Direct to Reports → Account Statement (enter account code + date range)
- Refresh Views:     Direct to the Refresh Views button on any report page

Example redirect: "The Trial Balance is available under Reports → Trial Balance in the
navigation menu. It shows all accounts with proper debit/credit columns and a balance check."

These redirects apply regardless of how the user phrases the request.

TOOL USAGE:
- Call read tools as many times as needed to gather context.
- Do not guess account codes or customer names — always verify via search tools.
- After calling read tools, provide a specific, actionable response.

Today's date: %s`, company.Name, company.CompanyCode, company.BaseCurrency, time.Now().Format("2006-01-02"))
```

---

## Why Not Just Leave the Tools In?

| Concern | Detail |
|---|---|
| **Trial balance hallucination** | No tool exists → AI invents data. Proven bug from screenshot. |
| **Formatting loss** | AI narrates raw JSON numbers; report pages show formatted tables with debit/credit split. |
| **No balance check** | AI cannot surface the "Balanced ✓" status; the report page does. |
| **No CSV export** | Account Statement page has CSV export; AI tool output has none. |
| **Redundant DB hits** | AI tools hit the same SQL as the page, with extra round-trips through GPT-4o. |
| **Token cost** | Sending full P&L or BS data through GPT-4o wastes tokens for no user value. |

---

## What the AI Agent Retains After This Change

The AI agent remains fully capable for the use-cases where it adds genuine value:

| Capability | Kept? |
|---|---|
| Post journal entries (natural language) | Yes |
| Look up account balances (`get_account_balance`) | Yes |
| Search accounts, customers, products | Yes |
| Get stock levels and warehouse info | Yes |
| Create vendors, purchase orders | Yes |
| Full PO lifecycle (approve, receive, invoice, pay) | Yes |
| Image-based invoice/document interpretation | Yes |
| Redirect users to report pages | Yes (new behavior via prompt) |

---

## Testing Checklist (run after implementation)

- [ ] Ask AI chat: "show me the trial balance" → expects redirect message, no table fabricated
- [ ] Ask AI chat: "what is the P&L for this month?" → expects redirect to Reports → P&L
- [ ] Ask AI chat: "give me the balance sheet" → expects redirect to Reports → Balance Sheet
- [ ] Ask AI chat: "show account statement for 1200" → expects redirect to Reports → Account Statement
- [ ] Ask AI chat: "refresh the views" → expects redirect to report page Refresh button
- [ ] Ask AI chat: "what is the balance of account 1000?" → expects inline answer (tool still present)
- [ ] Ask AI chat: "record a payment of 5000 from customer X" → expects journal entry flow (unaffected)
- [ ] Open Reports → Trial Balance → confirm page still loads correctly (service layer untouched)
- [ ] Open Reports → P&L → confirm page still loads correctly
- [ ] `go build ./...` — must compile clean (no dead code errors; Go does not error on unused
  functions but the JSON helper functions should be removed to keep the codebase clean)
- [ ] `go test ./internal/core -v` — all 70 tests must still pass (no service layer changes)

---

## Files Changed (summary)

| File | Change Type | Description |
|---|---|---|
| `internal/app/app_service.go` | Delete | Remove 4 tool definitions from `buildToolRegistry()` |
| `internal/app/app_service.go` | Delete | Remove 3 dead JSON-adapter helper functions |
| `internal/ai/agent.go` | Edit | Add STANDARD REPORTS redirect block to system prompt |

No database changes. No migration required. No interface changes. No test changes.

---

## Notes

- The `ApplicationService` interface methods (`GetProfitAndLoss`, `GetBalanceSheet`,
  `GetAccountStatement`, `RefreshViews`) are **not removed** — they are still called by the web
  report handlers. Only the AI tool wrappers around them are removed.
- `get_trial_balance` was never registered as an AI tool, so no removal needed there — only the
  system prompt addition covers it.
- `get_account_balance` (single-account balance lookup) is a different tool from
  `get_account_statement` (full ledger history) and is intentionally kept.
