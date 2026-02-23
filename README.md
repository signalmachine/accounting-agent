# Agentic Accounting Core

An AI-powered, enterprise-grade double-entry accounting system built with Go, PostgreSQL, and OpenAI's Responses API. Modeled after SAP's multi-company, multi-currency architecture.

## Overview

This system integrates a GPT-4o AI agent into a rigorous double-entry ledger. The agent interprets natural language business events and proposes structured journal entries. A human operator reviews and commits them via CLI or interactive REPL.

The system is built for multi-company, multi-currency scenarios where each company has a **base currency (local currency)**, and transactions may occur in any **transaction currency** with an explicit exchange rate.

---

## Features

| Feature | Description |
|---|---|
| **Double-Entry Ledger** | Strict enforcement of debit = credit balance in base currency |
| **Multi-Company** | Each transaction is scoped to a `Company Code` (like SAP's Company Code) |
| **Multi-Currency** | Captures `Transaction Currency`, `Exchange Rate`, and computes `Base Currency` amounts |
| **AI Agent** | GPT-4o via OpenAI Responses API generates structured `Proposal` objects |
| **Idempotency** | UUID-keyed idempotency prevents duplicate journal entries |
| **Reversals** | Atomic, auditable reversal of prior entries |
| **Document Types** | SAP-style document classification (`SI`, `PI`, `JE`) mapping intent to effects |
| **Gapless Numbering** | High-concurrency sequence generation using PostgreSQL row-level locks |
| **Date Semantics** | Separation of accounting period (`posting_date`) from transaction date (`document_date`) |
| **CLI & REPL** | Interactive command-line interface for proposing and committing transactions |
| **PostgreSQL** | ACID-compliant persistence with constraint enforcement |
| **Structured Outputs** | Strict JSON Schema via `invopop/jsonschema` guarantees schema-safe AI responses |

---

## Database Schema

### `companies`
| Column | Type | Notes |
|---|---|---|
| `id` | `SERIAL` | Primary key |
| `company_code` | `VARCHAR(10)` | Unique identifier (e.g., `1000`) |
| `name` | `TEXT` | Display name |
| `base_currency` | `VARCHAR(3)` | ISO currency code (e.g., `INR`, `USD`) |

### `accounts`
Accounts are scoped to a company via `company_id`.

### `document_types`
| Column | Notes |
|---|---|
| `code` | PK, e.g., `JE`, `SI`, `PI` |
| `affects_inventory` | Boolean flag for inventory impact |
| `numbering_strategy` | Strategy string (`global`, `per_fy`, etc) |

### `documents`
| Column | Notes |
|---|---|
| `company_id` | Foreign key to `companies` |
| `type_code` | Foreign key to `document_types` |
| `status` | State: `DRAFT`, `POSTED`, `CANCELLED` |
| `document_number` | Gapless business ID assigned at `POST` |

### `documents` vs `journal_entries`
A `document` represents the business event (like a Sales Invoice or a Purchase Invoice) and is assigned a rigorous, gapless `document_number` based on its `type_code` when posted. 
A `journal_entry` represents the accounting impact of that event. The `journal_entry` links back to the `document` via `reference_type = 'DOCUMENT'` and `reference_id = document_number`. This provides a clean separation between business operations and ledger mechanics.

### `journal_entries`
| Column | Notes |
|---|---|
| `company_id` | Foreign key to `companies` |
| `posting_date` | Date controlling the accounting period for reporting |
| `document_date` | Real-world transaction date (e.g., invoice date) |
| `created_at` | Strict system-generated timestamp |
| `narration` | AI-generated summary |
| `reference_type` | Type of linked entity (e.g., `DOCUMENT`) |
| `reference_id` | Business identifier of linked entity (e.g., `document_number`) |
| `reasoning` | Explanation from the AI |
| `idempotency_key` | UUID for deduplication |
| `reversed_entry_id` | Self-referencing FK for audit-safe reversals |

### `journal_lines`
| Column | Notes |
|---|---|
| `transaction_currency` | ISO code of the source currency (e.g., `USD`) |
| `exchange_rate` | Rate to convert to the company base currency |
| `amount_transaction` | Amount in transaction currency |
| `debit_base` | Computed debit in base currency |
| `credit_base` | Computed credit in base currency |

---

## Project Structure

```
.
├── cmd/
│   ├── app/            # Main CLI/REPL entry point
│   ├── verify-agent/   # Standalone AI integration test
│   └── verify-db/      # Runs all SQL migrations against the DB
├── internal/
│   ├── ai/             # OpenAI Responses API agent
│   ├── core/           # Domain models, ledger, proposal logic
│   │   ├── ledger.go
│   │   ├── ledger_integration_test.go  # Integration tests (requires TEST_DATABASE_URL)
│   │   ├── proposal_logic.go
│   │   └── proposal_test.go            # Unit tests
│   └── db/             # Database connection pool
├── migrations/
│   ├── 001_init.sql          # Base schema (accounts, journal_entries, journal_lines)
│   ├── 002_sap_currency.sql  # Multi-company & multi-currency upgrade
│   ├── 003_seed_data.sql     # Default company + full chart of accounts (idempotent)
│   ├── 004_date_semantics.sql # Date support
│   ├── 005_document_types_and_numbering.sql # Document types, records, and sequence locks
│   └── 006_fix_documents_unique_index.sql   # Bug fix for draft coalescing
└── .agents/
    └── skills/         # AI agent skill documentation
```

> [!NOTE]
> **Go test file convention**: Test files (`_test.go`) live *alongside the source files they test*, not in a separate `tests/` folder. This is idiomatic Go — the `go test` toolchain discovers tests by scanning each package directory for `_test.go` files. Moving them elsewhere would break automatic test discovery.

---

## Setup

### Prerequisites
- Go 1.21+
- PostgreSQL (running locally or remote)
- OpenAI API Key

### Environment
Create a `.env` file in the project root:
```env
DATABASE_URL=postgres://user:pass@localhost:5432/appdb?sslmode=disable
OPENAI_API_KEY=sk-...

# Required only for running integration tests (keeps live DB safe)
TEST_DATABASE_URL=postgres://user:pass@localhost:5432/appdb_test?sslmode=disable
```

### Database Initialization
The database schema and continuous evolution are exclusively managed via the built-in, production-grade migration runner. 
Initialize or patch your database safely:

```powershell
go run ./cmd/verify-db
```

This runner automatically enforces database authority by:
1. Dynamically scanning the `migrations/` directory for lexicographically sorted `.sql` scripts.
2. Acquiring PostgreSQL advisory locks to prevent concurrent executions.
3. Transactionally running raw SQL files and matching SHA-256 Checksums.
4. Keeping execution history securely within the `schema_migrations` tracking table so previous runs are gracefully skipped.

> [!NOTE]
> Ensure you have an active database named `appdb` available locally before running the migrator.

Alternatively, you could run specific un-tracked tests manually with `psql` (not recommended for production).
```bash
psql "$DATABASE_URL" -f migrations/001_init.sql
psql "$DATABASE_URL" -f migrations/002_sap_currency.sql
```

---

## Usage

### Build
```powershell
go build -o app.exe ./cmd/app
```

### Interactive REPL
```powershell
./app.exe
```

REPL command routing rules:
- **Single-word known commands** (e.g., `balances`, `help`, `exit`): Executed locally, never sent to AI.
- **Unknown single-word inputs**: Rejected immediately with an error.
- **Multi-word inputs**: Treated as business events and routed to GPT-4o for proposal generation.

```
> balances
Account 1000 | Cash/Bank     | ASSET   | Dr 41250.00 INR | Cr 0.00 INR
Account 4000 | Revenue       | REVENUE | Dr 0.00 INR     | Cr 41250.00 INR

> Received $500 from client for consulting
[AI generates a multi-currency proposal...]
Commit? (y/n): y
✓ Committed.
```

### CLI Commands
```powershell
# Propose a transaction (outputs JSON)
./app.exe propose "Paid $120 for software subscription"

# Validate a JSON proposal from stdin
Get-Content proposal.json | ./app.exe validate

# Commit a JSON proposal from stdin
Get-Content proposal.json | ./app.exe commit

# Show account balances
./app.exe balances
```

### Verify AI Agent
```powershell
go run ./cmd/verify-agent
```

### Running Integration Tests

> [!IMPORTANT]
> Integration tests truncate the database they connect to. Always use a dedicated test database — **never point `TEST_DATABASE_URL` at your live `appdb`**.

```powershell
# Set TEST_DATABASE_URL in .env to a separate test database, then:
go test ./internal/core -v
```

If `TEST_DATABASE_URL` is not set, integration tests are automatically skipped to protect the live database.

---

## Multi-Currency Workflow

Transactions follow the SAP model — **one currency per journal entry**:

> [!IMPORTANT]
> **No mixed-currency entries.** Every journal entry uses exactly one `TransactionCurrency`. If the event happened in USD, every line records an amount in USD. The line amounts are converted to the company's `BaseCurrency` using the single header-level `ExchangeRate`. Mixing USD debit with EUR credit in one entry is **forbidden**.

### Transaction Flow

1. **Event** (e.g., "Received $500 from a client")
2. **AI Proposal**: GPT-4o identifies:
   - `TransactionCurrency`: `USD` (header-level, applies to all lines)
   - `ExchangeRate`: e.g., `82.50` (USD → INR, header-level)
   - Each line has only `AccountCode`, `IsDebit`, `Amount` (in USD)
3. **Validation**: `Proposal.Validate()` verifies:
   - `TransactionCurrency` is set and `ExchangeRate > 0`
   - All line amounts are positive
   - `sum(Amount × ExchangeRate)` for debits = `sum(Amount × ExchangeRate)` for credits (balanced in base currency)
4. **Commit**: `journal_lines` stores:
   - `transaction_currency` — the common currency (e.g., `USD`)
   - `exchange_rate` — same for all lines
   - `amount_transaction` — line amount in `USD`
   - `debit_base` / `credit_base` — computed: `amount_transaction × exchange_rate` in `INR`

### Examples

| Scenario | TransactionCurrency | ExchangeRate | Notes |
|---|---|---|---|
| Domestic INR transaction | `INR` | `1.0` | No conversion needed |
| Foreign USD invoice | `USD` | `82.50` | All lines in USD, INR stored computed |
| EUR payment | `EUR` | `89.10` | All lines in EUR, INR stored computed |

---

## Architecture Notes

- **Separation of Concerns**: The AI agent (`internal/ai`) is fully decoupled from the core ledger (`internal/core`). The ledger does not know about AI.
- **Strict Structured Outputs**: JSON Schema is dynamically generated from the `Proposal` Go struct via `invopop/jsonschema`. The `$schema` key is stripped before submission since OpenAI strict mode does not accept it.
- **No `omitempty` in Schema Structs**: OpenAI's `Strict: true` mode requires all schema properties in the `required` array. `omitempty` causes exclusion and must not be used on structs submitted for AI schema generation.
- **Base Currency Balancing**: Ledger balance is enforced in the company's base currency. The `decimal` library is used throughout for exact precision.
- **Idempotency**: Each committed proposal carries a UUID idempotency key that prevents re-processing duplicate events.
