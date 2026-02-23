---
name: System Architecture
description: Core philosophy, architectural rules, domain model, and constraints for the Agentic Accounting Engine.
---
# Agentic Accounting Engine - System Documentation

## 1. Overview
The Agentic Accounting Engine is an enterprise-grade headless accounting core with a strict double-entry ledger mediated by an AI agent. It supports multi-company, multi-currency (SAP-style) transactions. It is NOT a UI application — it is a backend core with two terminal surfaces: a stateless CLI and a stateful REPL.

**Core Philosophy:**
> AI Proposal → Human Approval → Strict Ledger Enforcement

The AI acts as an interpreter of natural language business events, but it **never** has write access to the ledger. Only the Go core, enforcing strict mathematical and logical invariants, can commit transactions to the database.

## 2. Directory Structure
- **`cmd/app/`**: Main entry point — stateless CLI and conversational REPL.
- **`cmd/verify-db/`**: Migration runner — applies and checksums schema migrations.
- **`cmd/verify-agent/`**: AI agent smoke test.
- **`cmd/restore-seed/`**: Recovery tool — restores company, accounts, and document types if wiped.
- **`internal/`**: Private application code (Core Domain, AI integration, DB). Test files (`_test.go`) live alongside source files in each package directory — this is idiomatic Go.
- **`migrations/`**: Database schema SQL files (run in sequence).
- **`.agents/skills/`**: Agent skill documentation for AI context.

## 3. Architectural Rules (Hard Constraints)

1.  **AI NEVER WRITES TO DATABASE.** The AI's role is strictly limited to generating a `Proposal` struct.
2.  **Code is Law.** Only Go services validate balances and commit transactions.
3.  **Strict Precision.** Monetary values are stored as `TEXT` and processed via the `shopspring/decimal` library. Never use `float64` for money.
4.  **Transaction Isolation.** Account existence checks and insertions occur within the same DB transaction to prevent race conditions.
5.  **Interface-Driven.** `LedgerService` and `AgentService` are Go interfaces — decoupled and independently testable.
6.  **Trust but Verify.** All AI output is normalized (`Proposal.Normalize()`) then strictly validated (`Proposal.Validate()`) before touching business logic.
7.  **Test Isolation.** Integration tests must use `TEST_DATABASE_URL` (a separate DB). They must never connect to the live `DATABASE_URL`.

## 4. Domain Model

### SAP-Inspired Multi-Company, Multi-Currency Design

Each company has a **Base Currency** (local currency). Transactions are recorded in a single **Transaction Currency** per entry, with an **Exchange Rate** to convert to the base currency.

> **No mixed-currency entries.** Every journal entry uses exactly one `TransactionCurrency`. All lines share the same currency and exchange rate.

### Database Schema

#### `companies`
| Column | Type | Notes |
|---|---|---|
| `id` | `SERIAL` | Primary key |
| `company_code` | `VARCHAR(10)` | Unique identifier (e.g., `1000`) |
| `name` | `TEXT` | Display name |
| `base_currency` | `CHAR(3)` | ISO code (e.g., `INR`, `USD`) |

#### `accounts`
| Column | Notes |
|---|---|
| `company_id` | FK to `companies` — accounts are company-scoped |
| `code` | Account code (unique per company) |
| `name` | Account name |
| `type` | Enum: `asset`, `liability`, `equity`, `revenue`, `expense` |

#### `document_types`
| Column | Notes |
|---|---|
| `code` | Primary key (e.g., `JE`, `SI`, `PI`) |
| `numbering_strategy` | Enum: `global`, `per_fy`, `per_branch` |
| `affects_*` | Boolean flags indicating impacts on different sub-ledgers |

#### `documents`
| Column | Notes |
|---|---|
| `company_id` | FK to `companies` |
| `type_code` | FK to `document_types` |
| `status` | State: `DRAFT`, `POSTED`, `CANCELLED` |
| `document_number` | Highly controlled, gapless business ID assigned at `POST` |

#### `journal_entries`
| Column | Notes |
|---|---|
| `company_id` | FK to `companies` |
| `posting_date` | Date controlling the accounting period for reporting |
| `document_date` | Real-world transaction date (e.g., invoice date) |
| `created_at` | Strict system-generated timestamp |
| `narration` | AI-generated summary of the business event |
| `reference_type` | System referencing (e.g. `DOCUMENT`) |
| `reference_id` | Link to the business identifier (e.g. `document_number`) |
| `reasoning` | AI explanation for the proposed entry |
| `idempotency_key` | UUID — prevents duplicate entries |
| `reversed_entry_id` | Self-referencing FK for audit-safe reversals |

#### `journal_lines`
| Column | Notes |
|---|---|
| `account_id` | FK to `accounts` |
| `transaction_currency` | ISO code shared by all lines in the entry |
| `exchange_rate` | Rate: `TransactionCurrency → BaseCurrency` |
| `amount_transaction` | Amount in `TransactionCurrency` |
| `debit_base` | `amount_transaction × exchange_rate` (debit side in base currency) |
| `credit_base` | `amount_transaction × exchange_rate` (credit side in base currency) |

### Invariants
1.  **Balance**: `Sum(debit_base) == Sum(credit_base)` for every entry. Enforced in Go before commit.
2.  **Single Currency**: All lines in one entry share the same `transaction_currency` and `exchange_rate`.
3.  **Immutability**: Rows are never updated. Corrections are made via new reversing entries (`Ledger.Reverse()`).
4.  **Existence**: An entry must have at least two lines.
5.  **Company Scoping**: Account lookups are scoped to `company_id` — cross-company account use is prevented.

### Proposal Struct (AI Output)

```go
type Proposal struct {
    DocumentTypeCode    string         // REQUIRED: 'JE', 'SI', or 'PI'
    CompanyCode         string         // Header: which company
    IdempotencyKey      string         // Dedup key
    TransactionCurrency string         // Header: currency for ALL lines
    ExchangeRate        string         // Header: rate to base currency
    PostingDate         string         // YYYY-MM-DD: accounting period
    DocumentDate        string         // YYYY-MM-DD: real-world doc date
    Summary             string
    Confidence          float64
    Reasoning           string
    Lines               []ProposalLine
}

type ProposalLine struct {
    AccountCode string  // Must exist in the company's chart of accounts
    IsDebit     bool
    Amount      string  // In TransactionCurrency (always positive)
}
```

### AgentResponse (AI Output Wrapper)

The AI always returns an `AgentResponse`, which routes to either a confirmed proposal or a clarification request:

```go
type AgentResponse struct {
    IsClarificationRequest bool                  // true if AI needs more info
    Clarification          *ClarificationRequest // populated when IsClarificationRequest=true
    Proposal               *Proposal             // populated when IsClarificationRequest=false
}

type ClarificationRequest struct {
    Message string // Question posed back to the user
}
```

The REPL handles multi-turn conversations: if `IsClarificationRequest` is true, the user's answer is appended to the conversation context and the AI is called again.

### Data Validation Pipeline
1.  **`Proposal.Normalize()`** — Sanitizes AI output (empty strings → `"0.00"`, missing exchange rate → `"1.0"`, uppercases currency codes).
2.  **`Proposal.Validate()`** — Enforces business rules (company code present, currency present, rate > 0, amounts > 0, base-currency debits = credits).
3.  **`Ledger.execute()`** — Referential integrity check: all account codes exist in DB for the given company.

## 5. System Architecture

### Surface #1: Stateless Composable CLI
```
app propose "Paid 1000 for supplies"   → outputs JSON proposal
app validate < proposal.json           → validates logic + DB rules
app commit < proposal.json             → validates and commits to DB
app balances                           → prints current account balances
```

### Surface #2: Conversational REPL
- **Flow**: User Input → AI Interpretation → Clarification (if needed) → Logic Validation → Human Review → Commit
- **Clarification Loop**: If the AI returns `IsClarificationRequest: true`, the REPL asks the user the AI's question, appends the answer to the conversation context, and re-submits. This repeats until a confident `Proposal` is formed.
- **Routing**: Single-word known commands execute locally. Multi-word inputs → AI.
- **State**: Draft proposal held in memory until approved or rejected.

## 6. Data Flow (Event Lifecycle)

1.  **Input**: User types `"Received $500 USD from a client"` (REPL or CLI).
2.  **Context**: System fetches Chart of Accounts and Company from DB.
3.  **Interpretation**: `Agent.InterpretEvent` sends prompt + CoA + Company to OpenAI.
4.  **Proposal**: OpenAI returns structured JSON:
    ```json
    {
      "company_code": "1000",
      "transaction_currency": "USD",
      "exchange_rate": "82.50",
      "summary": "Receipt from client for services rendered",
      "confidence": 0.95,
      "reasoning": "Cash increases (debit) and revenue is recognized (credit)",
      "lines": [
        {"account_code": "1000", "is_debit": true,  "amount": "500.00"},
        {"account_code": "4100", "is_debit": false, "amount": "500.00"}
      ]
    }
    ```
5.  **Normalize + Validate**: Go validates balance in base currency: `500 × 82.50 = 41,250 INR` debit = `41,250 INR` credit ✓
6.  **Review (REPL only)**: User sees proposal with document type, currency, amounts, and reasoning. AI may first ask clarifying questions (multi-turn).
7.  **Commit**: `Ledger.Commit()` runs everything in a single atomic DB transaction:
    - `INSERT INTO documents` (DRAFT status)
    - `UPDATE documents SET status=POSTED` — assigns gapless `document_number` via `PostDocumentTx`
    - `INSERT INTO journal_entries` — linked via `reference_type='DOCUMENT'`, `reference_id=document_number`
    - `INSERT INTO journal_lines`
    - All four steps commit or roll back together. No orphaned POSTED documents on failure.

## 7. Migrations

The database is exclusively managed via a production-grade migration runner (`go run ./cmd/verify-db`). 
- **Strict Authority:** The runner asserts total schema ownership, automatically handling Postgres advisory locks to prevent concurrent executions.
- **Idempotency & Integrity:** Each migration executes inside its own dedicated transaction (`BEGIN ... COMMIT`). The script enforces SHA-256 checksumming and immutability by storing logs within a lazily generated `schema_migrations` table alongside an execution timestamp.

See `.agents/skills/database/SKILL.md` for strict guidance and operational rules regarding adding or verifying database schema updates.

## 8. Future Roadmap

1.  **Expanded Core Accounting**: Cost centers, report generation, period closing.
2.  **Inventory Management**: Stock tracking, valuation, movement journaling.
3.  **Order Management**: Purchase Orders, Sales Orders, Invoice processing.
4.  **Web Interface**: REST API layer (`cmd/api`) wrapping the existing `LedgerService`/`AgentService` interfaces — zero change to core logic.
5.  **Cloud Deployment**: Stateless binary, 12-factor config, PostgreSQL — ready for Docker/Cloud Run.
