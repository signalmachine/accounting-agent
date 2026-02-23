---
name: Database Skills & Migrations
description: Core rules for interacting with the database schema and executing PostgreSQL migrations using the verify-db migration runner.
---
# Database Skills & Migrations

The database for the Agentic Accounting Engine is exclusively managed via a strict, custom-built, production-grade migration runner located at `cmd/verify-db/main.go`. This document outlines the constraints, rules, and procedures for interacting with the database.

## 1. The Migration Authority Rule

> [!IMPORTANT]
> **Never manually create, drop, or alter tables or execute schema changes directly against the database.** 

The `verify-db` runner is the **single source of truth** for all schema evolution.
Any changes to the schema must be done by writing a new `.sql` migration file in the `migrations/` directory and running `go run ./cmd/verify-db`.

## 2. Security and Concurrency

The `verify-db` runner enforces strict constraints to prevent race conditions and schema corruption:
- **Fast Failures:** Database constraints ping the DB immediately. If the DB is unreachable, the program exits forcefully.
- **Concurrency Safety:** The runner attempts to acquire a PostgreSQL advisory lock (`SELECT pg_try_advisory_lock(...)`). If another instance is running, the lock fails, and the runner terminates instantly to prevent overlap.
- **Integrity Cheksums:** Every executed migration is hashed with SHA-256 and stored in the lazily generated `schema_migrations` table alongside an execution timestamp.

## 3. Migration Immutability & Safety Rules

> [!CAUTION]
> **Once a migration is written, committed, and applied to the database, you MUST NEVER modify its contents.**

Modifying an already-applied migration file will trigger a checksum mismatch in the runner (`[ERROR] Checksum mismatch for <filename>...`) and entirely block all future migrations from running.

If you need to change a schema or fix a bug in a past migration:
1. Create a **new** migration file (e.g. `007_fix_column.sql`) in the `migrations/` directory.
2. Write the corrective `ALTER TABLE`, `DROP`, or `UPDATE` statements within that new file.

## 4. Writing a Migration

All migrations reside in the `migrations/` directory and must meet the following criteria:
- **File Naming:** Must follow the format `NNN_description.sql` (e.g. `007_add_status.sql`). The script determines the sequence by extracting the zero-padded numerical prefix (`007`) before the first underscore.
- **No Dependencies:** Do not depend on ORMs or Go drivers. Keep the file as raw SQL (`CREATE`, `ALTER`, `INSERT`).
- **Idempotency is Handled:** The runner automatically skips already applied migrations by referencing `schema_migrations`.

## 5. Execution Logic & Tracking

Under the hood, `verify-db` applies migrations transactionally:

1. **Discovery:** Reads all `.sql` files, filters, and sorts them lexicographically.
2. **Transaction Scope:** Each file executes inside its own dedicated SQL transaction (`BEGIN ... COMMIT`).
3. **Execution & Insertion:** The raw SQL represents the first half of the transaction; the insertion of the metadata into `schema_migrations` represents the second half. This guarantees that a migration is only recorded if it executes flawlessly.

## 6. How to Run Migrations

```powershell
# Run the migration system against the default DATABASE_URL
go run ./cmd/verify-db
```

### Understanding the Logs

You will see structured logs. Example healthy output for a fully patched DB:

```text
2026/02/23 [CONNECT] success
2026/02/23 [LOCK] success
2026/02/23 [SKIP] 001_init.sql
2026/02/23 [SKIP] 002_sap_currency.sql
2026/02/23 [SKIP] 003_seed_data.sql
2026/02/23 [SKIP] 004_date_semantics.sql
2026/02/23 [SKIP] 005_document_types_and_numbering.sql
2026/02/23 [SKIP] 006_fix_documents_unique_index.sql
2026/02/23 [DONE] All migrations processed.
```

If you add a new file (`007_new_feature.sql`), the log will report `[APPLY] 007_new_feature.sql`.

## 7. Seed Data Recovery

The migration runner handles **schema only**. Seed data (companies, accounts, document types) is seeded by `003_seed_data.sql` on first run. If this data is ever accidentally wiped (e.g. by running integration tests against the live database), run:

```powershell
go run ./cmd/restore-seed
```

This tool idempotently restores:
- Company `1000` ("Local Operations India", INR base currency)
- Full chart of accounts (15 accounts: Cash, Bank, AR, AP, Inventory, etc.)
- Document types: `JE` (Journal Entry), `SI` (Sales Invoice), `PI` (Purchase Invoice)

> [!CAUTION]
> `restore-seed` deletes all existing journal entries and lines for company `1000` before re-seeding accounts. **Only use this as a recovery tool**, not as part of regular workflow.

> [!WARNING]
> **Never run integration tests with `TEST_DATABASE_URL` pointing at the same database as `DATABASE_URL`.** Integration tests truncate tables on startup. See `.agents/skills/testing/SKILL.md` for the correct test database isolation setup.
