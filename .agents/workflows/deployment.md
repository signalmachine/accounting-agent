---
description: how to deploy the application
---
# Agentic Accounting - Deployment Guide

This guide covers building, configuring, and deploying the Agentic Accounting Engine.

## 1. Prerequisites
- **Go 1.25+**: To compile the application.
- **PostgreSQL 14+**: The persistent data store.
- **OpenAI API Key**: Required for the `internal/ai` module.

## 2. Configuration (Environment Variables)
The application follows the **12-Factor App** methodology and uses environment variables for configuration.

| Variable | Required | Example | Description |
| :--- | :--- | :--- | :--- |
| `DATABASE_URL` | Yes | `postgres://user:pass@host:5432/db` | Connection string for PostgreSQL |
| `OPENAI_API_KEY` | Yes | `sk-proj-...` | Your OpenAI API Key |

Create a `.env` file in the root directory for local development.

## 3. Database Setup
The database schema is defined in SQL migration files.

### Running Migrations (Manual)
```bash
psql "$DATABASE_URL" -f migrations/001_init.sql
```
This creates the `accounts`, `journal_entries`, and `journal_lines` tables and seeds the initial Chart of Accounts.

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
- **Never commit `.env` or keys to version control.**
- Use a robust secrets manager (e.g., AWS Secrets Manager, HashiCorp Vault) in production.

### Database access
- The application requires `SELECT`, `INSERT`, and `UPDATE` permissions on the `public` schema.
- **Network Security**: Ensure the database is not exposed to the public internet. Use private subnets or strict firewall rules.

### PII Handling
- Be mindful of what you type into the REPL. While we scrub PII where possible, avoid entering highly sensitive personal data (SSNs, medical info) into narration fields.

## 6. Verification
After deployment, run the verification tool to ensure the AI and DB connections are healthy:
```bash
go run ./cmd/verify-agent/main.go
# Output should show a successful AI response.
```
