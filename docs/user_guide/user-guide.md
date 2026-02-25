# Agentic Accounting - User Guide

## 1. Interactive REPL (The Shell)

The REPL is the primary interface. It lets you describe business events in natural language, review the AI's proposal, and approve it for the ledger.

### Starting the REPL
```powershell
./app.exe
```

### Workflow
1. **Enter Event**: At the `> ` prompt, type a business event in natural language.
   ```
   > Received $500 cash from customer for consulting services
   ```
2. **Review Proposal**: The Agent prints a structured proposal:
   ```
   SUMMARY:    Receipt from client for consulting services
   COMPANY:    1000
   CURRENCY:   USD @ rate 82.50
   REASONING:  Cash increases (asset debit), revenue is recognized (credit)
   CONFIDENCE: 0.95
   ENTRIES:
     [DR] Account 1000      500.00 USD
     [CR] Account 4100      500.00 USD
   ```
3. **Approve/Reject**:
   - Type `y` or `yes` to commit the transaction to the database.
   - Type `n` or anything else to discard the draft.

### REPL Commands (use `/` prefix)
- `/bal` or `/balances` — Print Chart of Accounts with running balances (base currency).
- `/customers` — List customers.
- `/products` — List products.
- `/orders` — List sales orders.
- `/new-order` — Create a new sales order.
- `/confirm` — Confirm a sales order (reserves stock, assigns order number).
- `/ship` — Ship a confirmed order (deducts inventory, books COGS).
- `/invoice` — Create sales invoice for a shipped order.
- `/payment` — Record payment against an invoice.
- `/warehouses` — List warehouses.
- `/stock` — View inventory stock levels.
- `/receive` — Record a goods receipt (adds stock, books DR Inventory / CR AP).
- `/help` — List all available commands.
- `/exit` or `/quit` — Close the application.

> **Note:** Only multi-word inputs without `/` prefix are sent to the AI. Single-word inputs are treated as commands.

---

## 2. Composable CLI (The Plumbing)

The CLI follows the Unix philosophy for batch processing and automation.

### `propose`
Generates a JSON proposal from a text string. Does NOT write to DB.
```powershell
./app.exe propose "Bought office supplies for $50" > proposal.json
```

### `validate`
Reads a JSON proposal from `stdin` and checks business logic. Exits with 0 (pass) or 1 (fail).
```powershell
Get-Content proposal.json | ./app.exe validate
```

### `commit`
Reads a JSON proposal from `stdin`, validates, and commits to the DB.
```powershell
Get-Content proposal.json | ./app.exe commit
```

### `balances`
Prints current account balances.
```powershell
./app.exe balances
```

### Example Workflow (PowerShell)
```powershell
# 1. Generate a proposal
./app.exe propose "Paid internet bill 80 cash" | Out-File -Encoding ASCII step1.json

# 2. (Optional) Manual review of step1.json

# 3. Commit
Get-Content step1.json | ./app.exe commit
```

---

## 3. Troubleshooting

### "Low Confidence" Warning
- **Cause**: Ambiguous input or missing account codes.
- **Fix**: Rephrase. E.g., change "Paid for stuff" to "Paid for office supplies using cash".

### "Credits do not equal debits" / "Base currency imbalance"
- **Cause**: The AI proposed an unbalanced entry.
- **Fix**: Caught by the Validator before any DB write. Retry — the AI is non-deterministic and typically self-corrects.

### "Account code not found for company"
- **Cause**: The AI used an account code that doesn't exist in the database.
- **Fix**: Run `/balances` to see valid account codes. Rephrase your input referencing a known account type.
