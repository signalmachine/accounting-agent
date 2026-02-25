# Agentic Accounting - Deployment Guide

## 1. Prerequisites
- **Go 1.21+**: To compile the application.
- **PostgreSQL 14+**: The persistent data store.
- **OpenAI API Key**: Required for the `internal/ai` module.

## 2. Configuration (Environment Variables)

The application follows the **12-Factor App** methodology.

| Variable | Required | Example | Description |
| :--- | :--- | :--- | :--- |
| `DATABASE_URL` | Yes | `postgres://user:pass@host:5432/appdb` | Connection string for the live PostgreSQL database |
| `OPENAI_API_KEY` | Yes | `sk-proj-...` | Your OpenAI API Key |
| `TEST_DATABASE_URL` | Dev only | `postgres://user:pass@host:5432/appdb_test` | Separate database for integration tests — **never point at the live DB** |

Create a `.env` file in the root directory for local development (already in `.gitignore`).

## 3. Database Setup

### Running Migrations (Recommended)
```bash
go run ./cmd/verify-db
```

This executes all migration files in order. Current migrations (10 files):

| File | Purpose |
|---|---|
| `001_init.sql` | Base schema |
| `002_sap_currency.sql` | Multi-company & multi-currency |
| `003_seed_data.sql` | Default company (INR) + chart of accounts |
| `004_date_semantics.sql` | posting_date vs document_date |
| `005_document_types_and_numbering.sql` | Documents & gapless numbering |
| `006_fix_documents_unique_index.sql` | Fix unique index for drafts |
| `007_sales_orders.sql` | Sales orders domain |
| `008_seed_customers_products.sql` | Seed customers and products |
| `009_inventory_engine.sql` | Inventory engine |
| `010_seed_inventory.sql` | Seed warehouse and inventory items |

## 4. Building the Application

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
- **Never commit `.env` or keys to version control.**
- Use a secrets manager (GCP Secret Manager, AWS Secrets Manager, HashiCorp Vault) in production.

### Database Access
- The application requires `SELECT`, `INSERT` permissions on business tables.
- Ensure the database is not exposed to the public internet. Use private subnets or VPC peering.

### PII Handling
- Avoid entering highly sensitive personal data (SSNs, medical info) into narration fields — these are sent to OpenAI.

## 6. Verification

After deployment, run the built-in verification tools:

```bash
# Verify AI agent end-to-end
go run ./cmd/verify-agent
# Expected output: a structured proposal with currency, lines, and confidence score.

# Verify database schema
go run ./cmd/verify-db
# Expected output: "All migrations processed." with [SKIP] entries for each file.
```
