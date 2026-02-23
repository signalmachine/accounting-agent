---
description: how to deploy the application
---
# Agentic Accounting - Deployment Guide

This guide covers building, configuring, and deploying the Agentic Accounting Engine.

## 1. Prerequisites
- **Go 1.21+**: To compile the application.
- **PostgreSQL 14+**: The persistent data store.
- **OpenAI API Key**: Required for the `internal/ai` module.

## 2. Configuration (Environment Variables)
The application follows the **12-Factor App** methodology and uses environment variables for configuration.

| Variable | Required | Example | Description |
| :--- | :--- | :--- | :--- |
| `DATABASE_URL` | Yes | `postgres://user:pass@host:5432/appdb` | Connection string for the live PostgreSQL database |
| `OPENAI_API_KEY` | Yes | `sk-proj-...` | Your OpenAI API Key |
| `TEST_DATABASE_URL` | Dev only | `postgres://user:pass@host:5432/appdb_test` | Separate database for integration tests — **never point at the live DB** |

Create a `.env` file in the root directory for local development (already in `.gitignore`).

## 3. Database Setup
The database schema is managed via plain SQL migration files, applied in sequence.

### Running Migrations (Recommended — Go tool)
```powershell
go run ./cmd/verify-db
```
This executes all three migration files in order:

| File | Purpose |
|---|---|
| `001_init.sql` | Base schema |
| `002_sap_currency.sql` | Multi-company & multi-currency upgrade |
| `003_seed_data.sql` | Default company (INR) + 15 chart of accounts (idempotent) |

### Running Migrations (Manual — psql)
```bash
psql "$DATABASE_URL" -f migrations/001_init.sql
psql "$DATABASE_URL" -f migrations/002_sap_currency.sql
psql "$DATABASE_URL" -f migrations/003_seed_data.sql
```

## 4. Building the Application
Compile the Go source code into a single executable binary.

### Windows
```powershell
go build -o app.exe ./cmd/app
```

### Linux / Mac
```bash
go build -o app ./cmd/app
```

## 5. Security & Best Practices

### API Key Management
- **Never commit `.env` or keys to version control.** The `.gitignore` already excludes `.env`.
- Use a robust secrets manager (e.g., GCP Secret Manager, AWS Secrets Manager, HashiCorp Vault) in production.

### Database Access
- The application requires `SELECT`, `INSERT` permissions on `accounts`, `companies`, `journal_entries`, `journal_lines`.
- **Network Security**: Ensure the database is not exposed to the public internet. Use private subnets or VPC peering.

### PII Handling
- Avoid entering highly sensitive personal data (SSNs, medical info) into narration fields — these are sent to OpenAI.

## 6. Verification
After deployment, run the built-in verification tools to confirm the AI and DB connections are healthy:

```powershell
# Verify AI agent end-to-end
go run ./cmd/verify-agent
# Expected output: a structured proposal with currency, lines, and confidence score.

# Verify database schema
go run ./cmd/verify-db
# Expected output: "All migrations applied successfully."
```
