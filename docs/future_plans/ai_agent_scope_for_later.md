# AI Agent Scope Strategy

**Status:** Under consideration — not yet implemented
**Priority:** Medium — product and UX direction
**Nature:** Strategy document, not an implementation plan

---

## The Core Tension

The AI agent serves two very different user profiles:

**Profile A — The trained accountant / power user**
- Knows the chart of accounts, document types, and accounting rules
- Prefers keyboard-driven workflows, exact account codes, and deterministic behavior
- Would rather use the web UI directly than describe events in natural language
- For this user, AI tools that duplicate the web UI add noise without value

**Profile B — The non-accountant business owner / SME user**
- Does not know what a "debit" or "credit" is, or which account code applies
- Describes business events in plain English ("I paid the electricity bill — 4,500 rupees")
- Would find the structured web forms intimidating or confusing
- For this user, natural language is the only practical interface to the accounting system

The AI agent exists primarily for Profile B. The web UI exists primarily for Profile A.
The challenge is that many AI tools currently registered serve Profile A use cases
(vendor creation, PO lifecycle, goods receipt) — operations that Profile B users would
rarely initiate, and that Profile A users would prefer to do via the structured web UI.

---

## Current State (post Part A implementation)

The AI agent has been restricted as follows:
- Standard reports (P&L, Balance Sheet, Account Statement, Trial Balance) → redirected to Reports section
- Refresh Views → redirected to report page

The AI agent retains:
- Journal entry proposal and posting (the core use case for Profile B)
- Account/customer/product lookup tools (used to resolve context before proposing entries)
- Stock level and warehouse lookup
- Vendor read tools (get, search, info)
- PO lifecycle write tools (create PO, approve, receive, invoice, pay)
- AP balance and payment history read tools

---

## What the AI Agent Is Well-Suited For

| Use Case | Why AI Adds Value |
|---|---|
| Natural language journal entry proposal | Eliminates the need to know account codes or debit/credit rules |
| "What is the balance of account X?" | Quick inline answer without navigating to a report page |
| "Who are our vendors?" | Natural lookup without needing to know the vendor master screen |
| Interpreting scanned invoices / receipts (image upload) | AI can extract amounts, dates, and party names from photos |
| Guided clarification ("I think I paid something, but I'm not sure of the account") | AI can ask follow-up questions and resolve ambiguity |
| Explaining accounting events in plain English | AI can narrate what a proposed journal entry means in business terms |

---

## What the AI Agent Is NOT Well-Suited For

| Use Case | Why Web UI Is Better |
|---|---|
| Standard financial reports (P&L, BS, Trial Balance) | Web UI shows formatted tables, CSV export, debit/credit split, balance check |
| Full PO lifecycle (for trained users) | Web UI offers structured forms, line-by-line editing, validation feedback |
| Vendor creation (for trained users) | Web UI validates required fields, shows confirmation before saving |
| Bulk data entry | AI is single-event; web UI supports bulk import or multi-line forms |
| Audit trail review | Report pages and document logs are more navigable than chat |

---

## Proposed Future Scope Direction

### Near-term (conservative): Keep current tool set, improve routing

Rather than removing tools, improve the system prompt to more strongly route structured
operations to the web UI when the user appears to be a power user:

- If the user uses exact account codes, PO numbers, or vendor codes → assume Profile A,
  redirect to the relevant web UI page
- If the user describes events in plain English → assume Profile B, use AI tools

This is low-risk (no code changes to tools) but relies on prompt engineering, which is less
reliable than structural enforcement.

### Medium-term (recommended): Remove PO lifecycle write tools from AI agent

The full PO lifecycle (create_purchase_order, approve_po, receive_po, record_vendor_invoice,
pay_vendor) involves multi-step confirmation flows that are better served by the structured
web UI (Phase WD1). These tools were added during the MVP build-out but have a dedicated UI
that renders them redundant in the AI context.

**Removal candidate tools:**
- `create_purchase_order` — Phase WD1 PO wizard covers this
- `approve_po` — PO detail page covers this
- `receive_po` — PO detail page covers this
- `record_vendor_invoice` — PO detail page covers this
- `pay_vendor` — PO detail page covers this
- `create_vendor` — Vendor form covers this

**Keep for AI context:** `get_vendors`, `search_vendors`, `get_vendor_info`, `get_purchase_orders`,
`get_open_pos`, `get_ap_balance` — these are read tools that give the AI useful context when
answering questions, even if the user then goes to the web UI to take action.

This reduction leaves the agent focused on:
1. Journal entry proposal (the primary value for Profile B)
2. Read-only lookups to support that flow
3. Natural language Q&A about the business data

**Risk:** Some users may be relying on the chat to drive the PO lifecycle. Before removing,
verify usage patterns and communicate the change.

### Long-term: Role-aware tool exposure

The tool registry exposed to the AI could be filtered by the authenticated user's role.
A `viewer` role sees only read tools. An `accountant` role sees read tools plus journal
entry routing. An `admin` role sees the full registry.

This is architecturally cleaner than removing tools globally — different users get the
right tool set for their role and competency level.

---

## Decision Criteria for Future Tool Removal

Before removing any AI tool, verify:

1. **Is there a web UI equivalent?** If yes, the AI tool is a candidate for removal.
2. **Is the tool used primarily by Profile B users?** If yes, keep it — it adds genuine value.
3. **Does removal break any workflow that has no other path?** If yes, do not remove until
   the alternative path exists and is tested.
4. **Has the tool been observed causing incorrect behavior** (hallucination, data loss,
   confusing output)? If yes, remove immediately regardless of the above.

---

## Notes

- The journal entry path (`InterpretEvent`) is the most stable and highest-value AI feature.
  It must remain intact regardless of any scope changes to `InterpretDomainAction` tools.
- The image upload / invoice interpretation feature (WF5) is a Profile B feature and should
  be considered for expansion (PDF support, structured field extraction) rather than reduction.
- Scope reduction should be implemented in a separate plan per the one-concern-at-a-time rule
  in CLAUDE.md. Do not bundle tool removals with other changes.
