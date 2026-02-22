---
description: how to use the interactive REPL and composable CLI
---
# Agentic Accounting - User Guide

This guide explains how to interact with the Agentic Accounting Engine via its two interfaces: the **Interactive REPL** and the **Composable CLI**.

## 1. Interactive REPL (The Shell)
The REPL (Read-Eval-Print Loop) is the primary way for humans to interact with the accounting agent. It allows you to describe events in natural language, review the AI's proposal, and approve it for the ledger.

### Starting the REPL
Run the application without any arguments:
```powershell
./app.exe
```

### Workflow
1.  **Enter Event**: At the `> ` prompt, type a business event.
    *Example*: `> Received $500 cash from customer for consulting services`
2.  **Review Proposal**: The Agent will think and print a structured proposal.
    *   **Summary**: What the AI thinks happened.
    *   **Reasoning**: Why it chose specific accounts.
    *   **Entries**: The Debit/Credit lines.
3.  **Approve/Reject**:
    *   Type `y` or `yes` to commit the transaction to the database.
    *   Type `n` or anything else to discard the draft.

### Commands
- `balances`: Print the current Chart of Accounts and their balances.
- `exit` or `quit`: Close the application.

---

## 2. Composable CLI (The Plumbing)
The CLI allows for batch processing and automation. It follows the Unix philosophy of small tools joining via pipes.

### Commands

#### `propose`
Generates a JSON proposal from a text string. Does NOT write to DB.
```powershell
./app.exe propose "Bought office supplies for $50" > proposal.json
```

#### `validate`
Reads a JSON proposal from `stdin` and checks regular/business logic. Exits with 0 (pass) or 1 (fail).
```powershell
Get-Content proposal.json | ./app.exe validate
```

#### `commit`
Reads a JSON proposal from `stdin`, runs validation, and commits to the DB.
```powershell
Get-Content proposal.json | ./app.exe commit
```

### Example Workflow (PowerShell)
```powershell
# 1. Generate a proposal
./app.exe propose "Paid internet bill 80 cash" | Out-File -Encoding ASCII step1.json

# 2. (Optional) Manual Review of step1.json

# 3. Commit
Get-Content step1.json | ./app.exe commit
```

---

## 3. Troubleshooting

### "Low Confidence" Warning
If the AI is unsure (Confidence < 0.6), the REPL will show a warning.
- **Cause**: Ambiguous input or missing account codes.
- **Fix**: Rephrase your input. E.g., change "Paid for stuff" to "Paid for office supplies using Cache".

### "Credits do not equal debits"
- **Cause**: The AI proposed an unbalanced entry.
- **Fix**: This is caught by the Validator. Retry the request; the AI is non-deterministic and may fix it on the second try.

### "Account code not found"
- **Cause**: The AI Hallucinated a code that doesn't exist in the database.
- **Fix**: Run `balances` to see valid codes.
