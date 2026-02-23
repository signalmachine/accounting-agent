---
name: Testing Strategy
description: Rules, patterns, and workflows for unit tests and integration tests in this project. Covers test database setup, what to test, and how to avoid corrupting the live database.
---
# Testing Strategy

## ⚠️ Go Test File Convention (Important)

In Go, test files are **co-located with the source files they test** — not in a separate `tests/` folder.

```
internal/core/
├── ledger.go
├── ledger_integration_test.go   ← lives here, not in tests/
├── proposal_logic.go
└── proposal_test.go             ← lives here, not in tests/
```

This is the **idiomatic Go standard** followed by the Go standard library itself. A separate `tests/` folder is the convention in Python, JavaScript, and Java — but **not in Go**.

**Why co-location?**
- `go test ./internal/core` automatically discovers all `_test.go` files in that package directory.
- Moving test files to a `tests/` folder breaks this discovery — `go test` would not find them.
- Co-location keeps tests immediately adjacent to the code they verify, reducing navigation friction.
- Black-box tests use `package core_test` (note the `_test` suffix on the package name) while still residing in the same directory.

> [!CAUTION]
> **Never move test files to a `tests/` folder.** Always create `_test.go` files in the same directory as the source file being tested.

---

## 1. Two Categories of Tests

### Unit Tests (`proposal_test.go`)
- **Location**: Same package as production code, in `_test.go` files.
- **No DB required**: Pure Go logic — normalize and validate `Proposal` structs.
- **Run anytime**: `go test ./internal/core -run TestProposal`

### Integration Tests (`ledger_integration_test.go`)
- **Location**: `internal/core/ledger_integration_test.go`
- **Requires DB**: Connects to a real PostgreSQL instance.
- **Requires `TEST_DATABASE_URL`**: See section 2.
- **Run with**: `go test ./internal/core -v`

---

## 2. Database Isolation Rule (CRITICAL)

> [!CAUTION]
> Integration tests call `TRUNCATE TABLE ... CASCADE` on startup. They **will destroy all data** in the database they connect to.

**Always use a dedicated test database. Never point `TEST_DATABASE_URL` at the live `DATABASE_URL`.**

> [!WARNING]
> **Do NOT run** `$env:TEST_DATABASE_URL = $env:DATABASE_URL` before running tests. This will **wipe your live company, accounts, and journal entries**. If this happens, run `go run ./cmd/restore-seed` to recover.

### Setup
1. Create a test database in PostgreSQL:
   ```sql
   CREATE DATABASE appdb_test;
   ```
2. Run migrations on the test DB:
   ```powershell
   $env:DATABASE_URL = "postgres://user:pass@localhost:5432/appdb_test?sslmode=disable"
   go run ./cmd/verify-db
   ```
3. Add to `.env`:
   ```env
   TEST_DATABASE_URL=postgres://user:pass@localhost:5432/appdb_test?sslmode=disable
   ```
4. If `TEST_DATABASE_URL` is not set, all integration tests automatically **skip** — this protects the live database.

---

## 3. What `setupTestDB` Does

The `setupTestDB(t)` helper in `ledger_integration_test.go`:
1. Reads `TEST_DATABASE_URL` from environment (skips if unset).
2. Connects to the test database.
3. **Truncates**: `journal_lines`, `journal_entries`, `accounts`, `companies`, `documents`, `document_sequences`, `document_types` (with CASCADE).
4. **Seeds**: One company (`1000` / INR), two accounts (`1000` asset, `4000` revenue), and all three document types (`JE`, `SI`, `PI`).

This ensures every test starts from a known clean state.

---

## 4. Test Naming Conventions

| Test | File | What it tests |
|---|---|---|
| `TestProposal_Validate_Reproduction` | `proposal_test.go` | Edge case: blank amount after normalize |
| `TestProposal_NormalizationAndValidation` | `proposal_test.go` | Table-driven: happy path, foreign currency, invalid amounts, imbalance, missing company |
| `TestDocumentService_ConcurrentPosting` | `document_integration_test.go` | 10 concurrent PostDocument calls produce 10 unique gapless numbers |
| `TestLedger_Idempotency` | `ledger_integration_test.go` | Duplicate submission returns explicit error |
| `TestLedger_Reversal` | `ledger_integration_test.go` | Reversal creates mirror entry; double-reversal blocked |
| `TestLedger_CrossCompanyScoping` | `ledger_integration_test.go` | Account code not found in the requesting company → error |
| `TestLedger_GetBalances` | `ledger_integration_test.go` | Balances correctly reflect a committed transaction |

---

## 5. Running Tests

```powershell
# All tests (unit + integration — skips integration if TEST_DATABASE_URL not set)
go test ./...

# Specific package, verbose
go test ./internal/core -v

# Unit tests only (no DB needed)
go test ./internal/core -run TestProposal -v

# Integration tests only
go test ./internal/core -run TestLedger -v
```

---

## 6. What to Test (Checklist for New Features)

When adding new ledger or proposal logic, ensure tests cover:

- [ ] **Happy path** — valid input commits successfully
- [ ] **Imbalance** — debits ≠ credits in base currency → error
- [ ] **Zero amount** — amount = 0 → error
- [ ] **Negative amount** — negative → error
- [ ] **Unknown account code** — account not in DB → error
- [ ] **Wrong company account** — account exists but belongs to different company → error
- [ ] **Idempotency** — same idempotency key submitted twice → explicit duplicate error
- [ ] **Reversal** — entry is reversed correctly; second reversal is blocked
- [ ] **Missing company code** — empty `CompanyCode` → error
- [ ] **Missing transaction currency** — empty `TransactionCurrency` → error

---

## 7. Adding New Integration Tests

Follow this pattern:

```go
func TestLedger_YourFeature(t *testing.T) {
    pool := setupTestDB(t) // skips if TEST_DATABASE_URL not set
    defer pool.Close()

    docService := core.NewDocumentService(pool)
    ledger := core.NewLedger(pool, docService)
    ctx := context.Background()

    proposal := core.Proposal{
        DocumentTypeCode:    "JE",   // REQUIRED — must be a valid type seeded by setupTestDB
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

**Key rules for proposals in tests:**
- `DocumentTypeCode` is **mandatory** — always set it to `"JE"`, `"SI"`, or `"PI"`.
- `TransactionCurrency` and `ExchangeRate` are **header-level** on `Proposal` (not per-line).
- All amounts must balance: `sum(Amount × ExchangeRate)` debits = credits.
- Only use account codes seeded by `setupTestDB`: `"1000"` (asset) and `"4000"` (revenue).

---

## 8. Verify Tools (Manual Smoke Tests)

These are not automated tests but useful for manual verification:

```powershell
# Verify DB schema and seed data are correctly applied
go run ./cmd/verify-db

# Verify AI agent end-to-end (requires OPENAI_API_KEY)
go run ./cmd/verify-agent

# Verify entire build compiles with no errors
go build ./...

# Restore live DB seed data if accidentally wiped by integration tests
go run ./cmd/restore-seed
```

> [!WARNING]
> `go run ./cmd/restore-seed` clears all journal entries for company `1000` and re-seeds accounts and document types. Only run this if the live DB was accidentally contaminated by test data.
