# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all migrations against the live database
go run ./cmd/verify-db

# Build the application
go build -o app.exe ./cmd/app

# Run all tests (integration tests require TEST_DATABASE_URL in .env)
go test ./internal/core -v

# Run a single test
go test ./internal/core -v -run TestLedger_Idempotency

# Run unit tests only (no DB required)
go test ./internal/core -v -run TestProposal

# Verify AI agent integration
go run ./cmd/verify-agent

# Run the interactive REPL
./app.exe

# CLI one-shot commands
./app.exe propose "event description"
./app.exe balances
Get-Content proposal.json | ./app.exe validate
Get-Content proposal.json | ./app.exe commit
```

## Architecture

### Layering (strictly enforced — no exceptions)

```
cmd/ (REPL/CLI)
  → internal/core/ (domain: Ledger, OrderService, InventoryService, DocumentService, Proposal)
    → internal/db/ (pgx pool)
  → internal/ai/ (OpenAI agent — advisory only, never touches DB)
```

Forbidden: domain importing AI, ledger importing presentation, any layer importing upward. OrderService may call LedgerService. InventoryService may call `Ledger.CommitInTx` (but not the interface — only the concrete `*Ledger`). Neither domain knows journal schema internals.

### Key Design Decisions

**No ORM.** All database access uses hand-written SQL with `pgx/v5`. The PostgreSQL schema is the source of truth. Never use struct tags or reflection to generate SQL.

**Immutable ledger.** `journal_entries` and `journal_lines` are append-only. Business corrections use compensating entries, never UPDATEs. Only `internal/core/ledger.go` may write to these tables.

**AI is advisory only.** `internal/ai/agent.go` calls GPT-4o and returns a `core.Proposal`. The proposal must pass `Proposal.Validate()` before `Ledger.Commit()` is called. The AI never writes to the database.

**One transaction currency per journal entry (SAP model).** A single `TransactionCurrency` and `ExchangeRate` apply to all lines of an entry. Mixed-currency entries within one posting are forbidden. Line amounts are stored in transaction currency; `debit_base`/`credit_base` store the computed base-currency equivalent.

**Company scoping everywhere.** Every query touching business data must filter by `company_id`. There are no global reads of business data.

**Monetary precision.** Use `github.com/shopspring/decimal` for all monetary values. Database columns are `NUMERIC(14,2)` or `NUMERIC(15,6)`. Never use `float64` for money.

### REPL Input Classification

All commands use a `/` prefix. Input without a `/` prefix is routed to the AI agent.

Slash commands (deterministic, no AI):
- **Ledger**: `/bal`, `/balances`
- **Master data**: `/customers`, `/products`
- **Orders**: `/orders`, `/new-order`, `/confirm`, `/ship`, `/invoice`, `/payment`
- **Inventory**: `/warehouses`, `/stock`, `/receive`
- **Session**: `/help`, `/exit`, `/quit`

Multi-word input (no `/` prefix) → sent to GPT-4o as a business event description.

### OpenAI Integration

- Model: GPT-4o via Responses API (`openai-go` SDK)
- Strict JSON schema mode: all schema properties must appear in `required`. No `omitempty` on structs used for schema generation.
- The `$schema` key must be stripped before submission (OpenAI strict mode rejects it).
- Schema is dynamically generated from Go structs via `invopop/jsonschema`.
- Nullable fields use `anyOf: [{schema}, {type: "null"}]` manually — not Go pointers with omitempty.

### Document Flow

Business event → `DRAFT` Document → `POSTED` Document (gapless number assigned) → Journal Entry committed atomically.

Document types: `JE` (journal entry), `SI` (sales invoice, affects AR), `PI` (purchase invoice, affects AP), `SO` (sales order, gapless order numbering), `GR` (goods receipt, DR Inventory / CR AP), `GI` (goods issue / COGS, DR COGS / CR Inventory).

Gapless document numbers use PostgreSQL row-level locks on `document_sequences` (`FOR UPDATE`). Never compute the next sequence number in application memory.

### Inventory Design Rules

- `InventoryService` exposes two method categories: **standalone** (manage their own TX) and **TX-scoped** (accept a `pgx.Tx` from the caller).
- `ShipStockTx`, `ReserveStockTx`, `ReleaseReservationTx` are TX-scoped — called inside `OrderService` transactions to ensure atomicity.
- COGS booking uses `Ledger.CommitInTx(ctx, tx, proposal)` — committed inside the same TX as inventory deduction and order state update.
- Products without an `inventory_item` record are service products — silently skipped in all inventory operations (no stock check, no COGS).
- Inventory running totals (`qty_on_hand`, `qty_reserved`) are maintained under `SELECT ... FOR UPDATE` row-level locks. Never update inventory outside a locked row.

### Migrations

- Files live in `migrations/` and are named `NNN_description.sql` (lexicographic order).
- All migrations must be idempotent: use `IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, and `DO $$ ... EXCEPTION ... END $$` guards.
- Never edit a previously applied migration — always add a new numbered file.
- The migration runner tracks applied migrations via the `schema_migrations` table and acquires a PostgreSQL advisory lock before running.

### Testing

- Integration tests in `internal/core/*_integration_test.go` require `TEST_DATABASE_URL` and truncate that database. Never point `TEST_DATABASE_URL` at the live database.
- Tests auto-skip if `TEST_DATABASE_URL` is not set.
- After adding new migrations, apply them to the test DB before running integration tests:
  `DATABASE_URL=$TEST_DATABASE_URL go run ./cmd/verify-db`
- Ledger and proposal unit tests must not require OpenAI.
- Required test coverage: ledger commit success/rejection, cross-company isolation, concurrency for document numbering, balance calculation regression, inventory stock levels and COGS.
- Current test count: **27 tests** across ledger, document, order, and inventory suites.

## Pending Roadmap

- **Phases 0.5, 2, 3**: ✅ Complete. See `docs/Pending Implementation Phases.md`.
- **Phase 6** (recommended next): Reporting — materialized views, `/pl` (P&L) and `/bs` (Balance Sheet) REPL commands. Non-breaking, additive read layer.
- **Phase 4**: Policy & Rule Engine — replace hardcoded account mappings (1200 AR, 1400 Inventory, 5000 COGS) with a configurable rule registry.
- **Phases 5, 7, 8**: Approvals, AI Expansion, External Integrations — all deferred.

When implementing future phases: New domains call `Ledger.Commit()` or `Ledger.CommitInTx()` — they never construct `journal_lines` directly. Follow the established TX-scoped service method pattern from `InventoryService` for any operations that must be atomic with order state transitions.
