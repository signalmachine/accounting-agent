### PROMPT — AGENTIC ACCOUNTING (CORE & SHELL ARCHITECTURE)

You are a senior backend systems engineer building a proof-of-concept agentic accounting engine.

This is NOT a UI application. This is a headless accounting core with two terminal interaction surfaces bundled into a single binary:

1. **Conversational REPL** (The Shell: stateful reasoning + human approval)
2. **Stateless Composable CLI** (The Plumbing: automation + batch execution)

The REPL and CLI are thin adapters. The domain core must be completely decoupled from both.

**SYSTEM GOAL**
A user describes a business event in natural language. An AI agent proposes a journal entry. Go validation strictly enforces double-entry accounting invariants. A human explicitly approves. Only then is it written to PostgreSQL as an immutable ledger fact.

This is not bookkeeping automation. This is AI proposal → human approval → strict ledger enforcement.

**HARD ARCHITECTURAL RULES**

- **AI NEVER WRITES TO DATABASE.** AI only returns a `Proposal` struct.
- **Code is Law.** Only Go services validate balances and commit transactions.
- **Strict Precision.** You must NEVER parse monetary values into `float64` or `float32`. Keep them as exact strings and pass them directly to PostgreSQL's `NUMERIC` type, or use integer arithmetic (cents) for internal Go validation.
- **Transaction Isolation.** Validation reads (checking if accounts exist) and insertion writes must utilize the same database connection/transaction context to prevent N+1 queries and isolation leaks.
- **Interface-Driven.** The Ledger and Agent services must be defined by interfaces so they are equally callable from the REPL, the CLI, tests, or a future HTTP API.

**TECH STACK**

- Go 1.22+
- PostgreSQL (database: `appdb`)
- `github.com/jackc/pgx/v5` for connection pooling and queries.
- OpenAI Go SDK (The new 'Responses API' for structured JSON output)
  URL:
  https://github.com/openai/openai-go

- Raw SQL only (`database/sql` or `pgx` native). Absolutely no ORMs.
- SQL migrations as plain `.sql` files.

**DOMAIN MODEL — STRICT DOUBLE ENTRY LEDGER**
Tables:

- `accounts`: id (serial pk), code (text unique), name (text), type (check in: asset, liability, equity, revenue, expense)
- `journal_entries`: id, created_at, narration, reference_type (nullable), reference_id (nullable), reasoning (AI explanation), reversed_entry_id (nullable)
- `journal_lines`: id, entry_id, account_id, debit numeric(14,2), credit numeric(14,2)

Database Rules:

- `Sum(debit) == Sum(credit)` must be enforced in Go inside the transaction block before commit.
- Reversal creates a new entry — never update an existing row.
- No "draft" or "approved" flags in the DB. The existence of a row means it is an immutable truth.

Seed accounts:
1000 Cash, 1100 Bank, 1200 Furniture, 2000 Accounts Payable, 3000 Owner Capital, 4000 Revenue, 5000 Expenses.

**AI INTERPRETATION CONTRACT**
Function: `InterpretEvent(ctx, naturalLanguage string) (Proposal, error)`

The agent must return structured JSON ONLY:

```json
{
  "summary": "...",
  "confidence": 0.0-1.0,
  "reasoning": "...",
  "lines": [
    {"account_code":"5000","debit":"1000.00","credit":"0.00"},
    {"account_code":"1000","debit":"0.00","credit":"1000.00"}
  ]
}

```

Requirements:

- Provide the chart of accounts in the system prompt context.
- Temperature: 0–0.2.
- The AI must explain its reasoning for auditability.
- Amounts MUST be strings in the JSON to prevent unmarshaling precision loss.

**VALIDATION LAYER**
Go checks prior to DB insert:

1. All `account_code`s exist.
2. Total debits equal total credits.
3. Minimum of two lines per entry.
4. If `confidence < 0.6`, flag for explicit human override in the REPL.
   _If any validation fails, abort the transaction immediately._

**THE SINGLE BINARY ARCHITECTURE (`cmd/app/main.go`)**
The application compiles to a single binary (`app`) that routes execution based on `os.Args`.

**Surface #1: Stateless Composable CLI (The Plumbing)**
Invoked via subcommands.

- `app propose "Paid 1000 for supplies"` → Writes Proposal JSON to `stdout`.
- `app validate < proposal.json` → Reads JSON from `stdin`, runs Go validation, exits 0 or 1.
- `app commit < proposal.json` → Reads JSON from `stdin`, validates, commits to Postgres.
- Rules: Deterministic exit codes. No prompts. No persistent state. Pipeline friendly.

**Surface #2: Conversational REPL (The Shell)**
Invoked by running the binary with no subcommands (`app`).

- Behavior: Uses `bufio.Scanner` to create an interactive loop.
- Flow: User types event → REPL calls Agent service → Prints formatted text block (not full screen) → User chooses to approve (commits via Ledger service), edit, or reject.
- The REPL holds the draft state in memory only.

**PROJECT STRUCTURE**

- `cmd/app/main.go` (Subcommand router, CLI setup, REPL loop)
- `internal/core/ledger.go` (DB transaction & validation logic)
- `internal/ai/agent.go` (OpenAI integration)
- `internal/db/db.go` (pgxpool setup)
- `migrations/001_init.sql`

**IMPLEMENTATION ORDER**
Produce code step-by-step. Each step must be complete and compilable before moving to the next.

1. `migrations/001_init.sql`
2. `internal/db/db.go`
3. `internal/core/ledger.go` (Interface, validation, atomic transaction)
4. `internal/ai/agent.go`
5. `cmd/app/main.go` (Implement CLI subcommands first)
6. `cmd/app/main.go` (Append the REPL loop)
