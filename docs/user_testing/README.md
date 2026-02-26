# User Testing Guides

This folder contains workflow-specific testing guides for the web UI.

Each document is named after the workflow it covers and provides step-by-step instructions for a human tester to execute the workflow end-to-end, observe results, and determine pass/fail.

## Purpose

- Give testers enough context to complete a workflow without prior system knowledge
- Define clear success and failure criteria for each step
- Cover both the happy path and key error/edge cases
- Serve as living documentation — updated whenever the UI changes

## Document Conventions

Each guide follows this structure:

```
# <Workflow Name>

## Prerequisites
What must be true before starting (seed data, user role, browser, etc.)

## Steps
Numbered steps with exact UI interactions (button labels, field names, input values)

## Expected Results
What the tester should see at each step / at the end

## Pass Criteria
Conditions that confirm the workflow works correctly

## Fail Indicators
Symptoms that indicate a bug or missing feature
```

## Planned Guides

Guides will be written as each web UI domain phase (WD0–WD3) is completed:

| File | Workflow | Web Phase |
|---|---|---|
| `login.md` | Login / logout | WF3 |
| `trial-balance.md` | View trial balance | WD0 |
| `journal-entry.md` | Manual journal entry via AI chat | WD0 |
| `sales-order.md` | Create → confirm → ship → invoice → payment | WD1 |
| `purchase-order.md` | Receive goods → auto AP posting | WD1 |
| `stock.md` | View stock levels, receive inventory | WD2 |
| `account-statement.md` | Account ledger / statement report | WD2 |
| `ai-chat.md` | AI chat panel — propose and confirm entries | WD3 |
| `document-upload.md` | Attach invoice/receipt to AI chat | WD3 |

## Status

No guides written yet. This folder is created in advance and will be populated as each web UI phase is delivered and ready for user testing.
