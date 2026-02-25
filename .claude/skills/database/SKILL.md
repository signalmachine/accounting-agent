---
name: database
description: Rules for database interaction, migrations, and the verify-db runner. Use when writing migrations, modifying the schema, working with database queries, or troubleshooting migration issues.
---

# Database Skills & Migrations

The database is exclusively managed via a strict, custom-built, production-grade migration runner at `cmd/verify-db/main.go`.

## 1. The Migration Authority Rule

> **Never manually create, drop, or alter tables or execute schema changes directly against the database.**

The `verify-db` runner is the **single source of truth** for all schema evolution. Any changes to the schema must be done by writing a new `.sql` migration file in the `migrations/` directory and running `go run ./cmd/verify-db`.

## 2. Security and Concurrency

The `verify-db` runner enforces strict constraints:
- **Fast Failures:** Database constraints ping the DB immediately. If the DB is unreachable, the program exits forcefully.
- **Concurrency Safety:** The runner acquires a PostgreSQL advisory lock (`SELECT pg_try_advisory_lock(...)`). If another instance is running, the lock fails and the runner terminates.
- **Integrity Checksums:** Every executed migration is hashed with SHA-256 and stored in `schema_migrations` alongside an execution timestamp.

## 3. Migration Immutability & Safety Rules

> **Once a migration is written, committed, and applied to the database, you MUST NEVER modify its contents.**

Modifying an already-applied migration file will trigger a checksum mismatch (`[ERROR] Checksum mismatch for <filename>...`) and block all future migrations.

If you need to fix a past migration:
1. Create a **new** migration file (e.g. `011_fix_column.sql`) in the `migrations/` directory.
2. Write the corrective `ALTER TABLE`, `DROP`, or `UPDATE` statements within that new file.

## 4. Writing a Migration

All migrations reside in `migrations/` and must meet:
- **File Naming:** `NNN_description.sql` (e.g. `011_add_status.sql`). Sequence is determined by the zero-padded numerical prefix.
- **No Dependencies:** Raw SQL only — no ORM dependencies.
- **Idempotency:** Use `IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, and `DO $$ ... EXCEPTION ... END $$` guards.
- The runner automatically skips already-applied migrations via `schema_migrations`.

## 5. Execution Logic & Tracking

Under the hood, `verify-db` applies migrations transactionally:

1. **Discovery:** Reads all `.sql` files, filters, and sorts them lexicographically.
2. **Transaction Scope:** Each file executes inside its own dedicated SQL transaction (`BEGIN ... COMMIT`).
3. **Execution & Insertion:** SQL runs first; metadata is inserted into `schema_migrations` second. A migration is only recorded if it executes flawlessly.

## 6. How to Run Migrations

```bash
# Run the migration system against the default DATABASE_URL
go run ./cmd/verify-db
```

### Understanding the Logs

```text
2026/02/23 [CONNECT] success
2026/02/23 [LOCK] success
2026/02/23 [SKIP] 001_init.sql
2026/02/23 [SKIP] 002_sap_currency.sql
...
2026/02/23 [DONE] All migrations processed.
```

If you add a new file (`011_new_feature.sql`), the log will report `[APPLY] 011_new_feature.sql`.

## 7. Seed Data Recovery

The migration runner handles **schema only**. If seed data is ever accidentally wiped (e.g. by running integration tests against the live database), run:

```bash
go run ./cmd/restore-seed
```

This idempotently restores:
- Company `1000` ("Local Operations India", INR base currency)
- Full chart of accounts (15 accounts: Cash, Bank, AR, AP, Inventory, etc.)
- Document types: `JE`, `SI`, `PI`, `SO`, `GR`, `GI`

> **CAUTION:** `restore-seed` deletes all existing journal entries and lines for company `1000` before re-seeding. Only use this as a recovery tool.

> **WARNING:** Never run integration tests with `TEST_DATABASE_URL` pointing at the same database as `DATABASE_URL`. Integration tests truncate tables on startup.
