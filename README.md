# Agentic Accounting Core

A proof-of-concept AI-powered accounting system built with Go, PostgreSQL, and OpenAI's Responses API.

## Overview

This application demonstrates how to integrate an AI agent into a core business domain (accounting journal entries). It uses a "Human-in-the-Loop" workflow where the AI proposes structured journal entries based on natural language events, and a human operator reviews and commits them to the ledger.

## Features

- **Double-Entry Ledger**: Core logic ensures debits equal credits and enforces account code validity.
- **AI Agent**: specific implementation using OpenAI `responses.New` (GPT-4o) to generate structured `Proposal` objects.
- **CLI & REPL**: Interactive command-line interface for proposing and committing transactions.
- **PostgreSQL Persistence**: Robust data checks and ACID transactions.

## Project Structure

- `cmd/app`: Main entry point for the CLI/REPL application.
- `cmd/verify-agent`: Verification tool to test AI integration in isolation.
- `internal/core`: Domain logic (Ledger, Models).
- `internal/ai`: AI Agent implementation using OpenAI SDK.
- `internal/db`: Database connection pooling.
- `migrations`: SQL schemas.
- `logs`: Application logs and audit trails.
- `tests`: Integration tests and golden datasets.

## Setup

1.  **Prerequisites**:
    - Go 1.25+
    - PostgreSQL
    - OpenAI API Key

2.  **Environment**:
    Create a `.env` file:
    ```env
    DATABASE_URL=postgres://user:pass@localhost:5432/appdb
    OPENAI_API_KEY=sk-...
    ```

3.  **Database**:
    Initialize the schema:

    Initialize the schema:
    ```bash
    psql "$DATABASE_URL" -f migrations/001_init.sql
    ```
    *Alternatively, if you don't have `psql` installed locally but have Go:*
    (You can create a small Go script to execute the SQL file content using the `pgx` driver, or use a GUI tool like pgAdmin/DBeaver to run `migrations/001_init.sql`)

## Usage

### Build
```powershell
# Windows
go build -o app.exe ./cmd/app

# Linux/Mac
go build -o app ./cmd/app
```

### Interactive REPL
```powershell
./app.exe
# > Received $500 from client for consulting
```

### CLI Commands
```powershell
# Propose a transaction (outputs JSON)
./app.exe propose "Paid $120 for software subscription"

# Validate a JSON proposal (PowerShell)
Get-Content proposal.json | ./app.exe validate

# Commit a JSON proposal (PowerShell)
Get-Content proposal.json | ./app.exe commit

# Check Account Balances
./app.exe balances
```

## Architecture Notes

- **Separation of Concerns**: The AI agent (`internal/ai`) is decoupled from the core ledger (`internal/core`).
- **Structured Outputs**: We use OpenAI's JSON Schema feature to guarantee that the AI returns valid JSON matching our `Proposal` struct.
- **Verification**: `go run ./cmd/verify-agent/main.go` runs a standalone test of the AI integration.
