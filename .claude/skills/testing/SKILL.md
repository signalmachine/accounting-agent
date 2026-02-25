---
name: testing
description: Testing strategy, patterns, and database isolation rules for this project. Use when writing tests, setting up test databases, adding test coverage, or understanding test conventions.
---

# Testing Strategy

## Go Test File Convention

In Go, test files are **co-located with the source files they test** — not in a separate `tests/` folder.

```
internal/core/
├── ledger.go
├── ledger_integration_test.go   ← lives here, not in tests/
├── proposal_logic.go
└── proposal_test.go             ← lives here, not in tests/
```

> **CAUTION:** Never move test files to a `tests/` folder. `go test ./internal/core` automatically discovers all `_test.go` files in that directory. A separate folder breaks discovery.

## 1. Two Categories of Tests

### Unit Tests (`proposal_test.go`)
- No DB required — pure Go logic.
- Run anytime: `go test ./internal/core -run TestProposal`

### Integration Tests (`ledger_integration_test.go`)
- Connects to a real PostgreSQL instance.
- Requires `TEST_DATABASE_URL` env var.
- Run with: `go test ./internal/core -v`

## 2. Database Isolation Rule (CRITICAL)

> Integration tests call `TRUNCATE TABLE ... CASCADE` on startup. They **will destroy all data** in the database they connect to.

**Always use a dedicated test database. Never point `TEST_DATABASE_URL` at the live `DATABASE_URL`.**

### Setup
1. Create a test database in PostgreSQL:
   ```sql
   CREATE DATABASE appdb_test;
   ```
2. Run migrations on the test DB:
   ```bash
   DATABASE_URL="postgres://user:pass@localhost:5432/appdb_test?sslmode=disable" go run ./cmd/verify-db
   ```
3. Add to `.env`:
   ```env
   TEST_DATABASE_URL=postgres://user:pass@localhost:5432/appdb_test?sslmode=disable
   ```
4. If `TEST_DATABASE_URL` is not set, all integration tests automatically **skip**.

## 3. What `setupTestDB` Does

The `setupTestDB(t)` helper:
1. Reads `TEST_DATABASE_URL` from environment (skips if unset).
2. Connects to the test database.
3. **Truncates**: `journal_lines`, `journal_entries`, `accounts`, `companies`, `documents`, `document_sequences`, `document_types` (with CASCADE).
4. **Seeds**: One company (`1000` / INR), two accounts (`1000` asset, `4000` revenue), and all document types.

## 4. Test Naming Conventions

| Test | File | What it tests |
|---|---|---|
| `TestProposal_NormalizationAndValidation` | `proposal_test.go` | Table-driven: happy path, foreign currency, invalid amounts, imbalance |
| `TestDocumentService_ConcurrentPosting` | `document_integration_test.go` | 10 concurrent PostDocument calls produce 10 unique gapless numbers |
| `TestLedger_Idempotency` | `ledger_integration_test.go` | Duplicate submission returns explicit error |
| `TestLedger_Reversal` | `ledger_integration_test.go` | Reversal creates mirror entry; double-reversal blocked |
| `TestLedger_CrossCompanyScoping` | `ledger_integration_test.go` | Account not found in requesting company → error |
| `TestLedger_GetBalances` | `ledger_integration_test.go` | Balances correctly reflect a committed transaction |

## 5. Running Tests

```bash
# All tests (unit + integration — skips integration if TEST_DATABASE_URL not set)
go test ./...

# Specific package, verbose
go test ./internal/core -v

# Unit tests only (no DB needed)
go test ./internal/core -run TestProposal -v

# Integration tests only
go test ./internal/core -run TestLedger -v
```

## 6. What to Test (Checklist for New Features)

- [ ] **Happy path** — valid input commits successfully
- [ ] **Imbalance** — debits ≠ credits in base currency → error
- [ ] **Zero amount** — amount = 0 → error
- [ ] **Negative amount** → error
- [ ] **Unknown account code** — account not in DB → error
- [ ] **Wrong company account** — account belongs to different company → error
- [ ] **Idempotency** — same key submitted twice → explicit duplicate error
- [ ] **Reversal** — entry is reversed correctly; second reversal is blocked
- [ ] **Missing company code** → error
- [ ] **Missing transaction currency** → error

## 7. Adding New Integration Tests

```go
func TestLedger_YourFeature(t *testing.T) {
    pool := setupTestDB(t) // skips if TEST_DATABASE_URL not set
    defer pool.Close()

    docService := core.NewDocumentService(pool)
    ledger := core.NewLedger(pool, docService)
    ctx := context.Background()

    proposal := core.Proposal{
        DocumentTypeCode:    "JE",   // REQUIRED
        CompanyCode:         "1000",
        IdempotencyKey:      uuid.NewString(),
        TransactionCurrency: "INR",
        ExchangeRate:        "1.0",
        Summary:             "Test description",
        Reasoning:           "Test reasoning",
        Lines: []core.ProposalLine{
            {AccountCode: "1000", IsDebit: true,  Amount: "100.00"},
            {AccountCode: "4000", IsDebit: false, Amount: "100.00"},
        },
    }

    err := ledger.Commit(ctx, proposal)
    if err != nil {
        t.Fatalf("expected commit to succeed: %v", err)
    }
}
```

Key rules:
- `DocumentTypeCode` is mandatory — always set it to `"JE"`, `"SI"`, or `"PI"`.
- `TransactionCurrency` and `ExchangeRate` are header-level on `Proposal` (not per-line).
- All amounts must balance: `sum(Amount × ExchangeRate)` debits = credits.
- Only use account codes seeded by `setupTestDB`: `"1000"` (asset) and `"4000"` (revenue).

## 8. Verify Tools (Manual Smoke Tests)

```bash
# Verify DB schema and seed data
go run ./cmd/verify-db

# Verify AI agent end-to-end (requires OPENAI_API_KEY)
go run ./cmd/verify-agent

# Verify entire build compiles
go build ./...

# Restore live DB seed data if accidentally wiped
go run ./cmd/restore-seed
```
