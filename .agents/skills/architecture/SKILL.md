---
name: System Architecture
description: Core philosophy, architectural rules, domain model, and constraints for the Agentic Accounting Engine.
---
# Agentic Accounting Engine - System Documentation

## 1. Overview
The Agentic Accounting Engine is a proof-of-concept headless accounting core designed to demonstrate a strictly enforced, double-entry ledger system mediated by an AI agent. It is NOT a UI application but a backend core with two terminal interaction surfaces: a stateless CLI and a stateful REPL.

**Core Philosophy:**
> AI Proposal → Human Approval → Strict Ledger Enforcement

The AI acts as an interpreter of natural language business events, but it **never** has write access to the ledger. Only the Go core, enforcing strict mathematical and logical invariants, can commit transactions to the database.

## 2. Directory Structure
- **`cmd/`**: Entry points (Main App, Verification Tool).
- **`internal/`**: Private application code (Core Domain, AI integration, DB).
- **`migrations/`**: Database schema SQL files.
- **`logs/`**: Runtime logs and audit trails.
- **`tests/`**: Integration tests and golden datasets for AI evaluation.

## 3. Architectural Rules (Hard Constraints)
*Derived from System Prompt*

1.  **AI NEVER WRITES TO DATABASE.** The AI's role is strictly limited to generating a `Proposal` struct.
2.  **Code is Law.** Only Go services validate balances and commit transactions to the database.
3.  **Strict Precision.** Monetary values are never parsed into `float64` or `float32`. They are handled as exact strings passed directly to PostgreSQL's `NUMERIC` type or handled via `decimal` libraries to prevent floating-point errors.
4.  **Transaction Isolation.** Validation reads (e.g., checking account existence) and insertion writes occur within the same database transaction context to prevent race conditions and N+1 queries.
5.  **Interface-Driven.** The `Ledger` and `Agent` services are defined by Go interfaces, ensuring they are decoupled and testable.
6.  **Trust but Verify.** All AI output is normalized and strictly validated before touching the business logic.

## 3. Tech Stack
- **Language**: Go 1.22+
- **Database**: PostgreSQL (`appdb`)
- **Driver**: `github.com/jackc/pgx/v5` (using `pgxpool`)
- **AI**: OpenAI Go SDK (Responses API for structured JSON)
- **Migrations**: Plain SQL files (Raw SQL only, no ORMs)

## 4. Domain Model
The system mirrors a traditional strict double-entry ledger.

### Database Schema

#### `accounts`
Represents the Chart of Accounts.
- **id**: Serial Primary Key
- **code**: Text Unique (e.g., "1000", "5000")
- **name**: Text (e.g., "Cash", "Expenses")
- **type**: Enum (`asset`, `liability`, `equity`, `revenue`, `expense`)

#### `journal_entries`
Represents a distinct business event.
- **id**: Serial Primary Key
- **created_at**: Timestamp
- **narration**: Description of the event
- **reasoning**: The AI's explanation for why this entry was proposed
- **reversed_entry_id**: Reference to another entry if this is a reversal (Immutability: we never delete, only reverse).

#### `journal_lines`
The individual debit/credit legs of a transaction.
- **entry_id**: FK to `journal_entries`
- **account_id**: FK to `accounts`
- **debit**: `NUMERIC(14,2)`
- **credit**: `NUMERIC(14,2)`

### Invariants
1.  **Balance**: `Sum(debit) == Sum(credit)` for every single entry. Enforced in Go before commit.
2.  **Immutability**: Rows are never updated. Corrections are made via new reversing entries.
3.  **Existence**: An entry must have at least two lines.

### Data Validation Layer
To ensure data integrity despite non-deterministic AI inputs, we implement a multi-stage validation pipeline:

1.  **Normalization**: The `Proposal.Normalize()` method cleans raw inputs (e.g., converting empty strings or "null" literals to "0.00").
2.  **Strict Business Rules**: The `Proposal.Validate()` method enforces accounting invariants:
    - Non-negative amounts only.
    - Mutually exclusive Debit/Credit (a line cannot have both).
    - Balanced Entry (Total Debits == Total Credits).
3.  **Referential Integrity**: The Ledger checks that all Account Codes exist in the DB.

## 5. System Architecture
The application compiles to a single binary (`app`) with two distinct modes of operation.

### Surface #1: Stateless Composable CLI (The Plumbing)
Command-line pipeline tools for batch processing and automation.
- `app propose "Paid 1000 for supplies"`: Outputs JSON proposal.
- `app validate < proposal.json`: Validates JSON against logic/DB rules.
- `app commit < proposal.json`: Validates and commits to DB.
- `app balances`: Prints current account balances.

**Characteristics**: Deterministic, pipeline-friendly, no persistent state.

### Surface #2: Conversational REPL (The Shell)
Interactive loop for human-in-the-loop workflows.
- **Flow**: User Input → AI Interpretation → Logic Validation → Human Review → Commit.
- **State**: Holds the "draft" proposal in memory until approved.

## 6. Internal Components

### `cmd/app/main.go`
Entry point. Routes logic based on `os.Args`. Initializes the `pgxpool`, `Ledger`, and `Agent` services.

### `internal/core/ledger.go` (Ledger Service)
The "Kernel" of the application.
- **Responsibility**: Validation and Database Persistence.
- **Key Method**: `Commit(ctx, proposal)`
    - Validates inputs (min 2 lines, debits=credits).
    - Starts DB transaction.
    - Resolves Account Codes to IDs.
    - Inserts Entry and Lines atomicially.
    - Commits transaction.

### `internal/ai/agent.go` (Agent Service)
The "Brain" of the application.
- **Responsibility**: Translating natural language into structured `Proposal` objects.
- **Implementation**: Uses OpenAI Chat Completions with 'Structured Outputs' (`json_schema`).
- **Prompting**: Injects the current Chart of Accounts into the system prompt to ensure the AI uses valid codes.

## 7. Data Flow (Event Lifecycle)

1.  **Input**: User types "Bought a laptop for 1200 cash" (REPL or CLI).
2.  **Context**: System fetches current Chart of Accounts from DB.
3.  **Interpretation**: `Agent.InterpretEvent` sends prompt + CoA to OpenAI.
4.  **Proposal**: OpenAI returns structured JSON:
    ```json
    {
      "summary": "Purchase of office equipment",
      "lines": [
        {"account_code": "1200", "debit": "1200.00", "credit": "0.00"},
        {"account_code": "1000", "debit": "0.00", "credit": "1200.00"}
      ]
    }
    ```
5.  **Review (REPL only)**: User sees the proposal and reasoning.
6.  **Validation**: `Ledger.Validate` checks:
    - Do accounts 1200 and 1000 exist?
    - Does 1200.00 == 1200.00?
    - Are amounts valid numbers?

## 8. Future Roadmap
The current application is a foundational core. The future vision includes significant expansion:

1.  **Expanded Core Accounting**: Advanced capabilities (Cost centers, Multi-currency, Consolidation).
2.  **New Components**:
    - **Inventory Management**: Tracking stock levels, valuation, and movements.
    - **Order Management**: Sales Organizations, Purchase Orders, and Invoice processing.
3.  **Web Interface**: Moving beyond the CLI/REPL to a rich web-based UI for dashboarding, complex entry, and management.
4.  **Cloud Architecture**:
    - The main application will be hosted in the cloud (e.g., AWS/GCP).
    - Users will access the system via "Thin Clients" (Web Browsers or lightweight Desktop wrappers) that communicate with the Cloud API.

## 9. Architectural Readiness Analysis
The current architecture is specifically designed to support this expansion roadmap with minimal refactoring.

| Future Requirement | Current Architecture Support |
| :--- | :--- |
| **Inventory & Order Mgmt** | **High.** The `LedgerService` is a decoupled "engine." New modules (e.g., `InventoryService`) can be built alongside it. When stock moves, the Inventory logic calculates values and simply calls `Ledger.Commit()` to record the financial financial impact. The shared database transaction model allows atomic updates across modules. |
| **Web Interface** | **High.** The "Core" (`internal/`) is completely decoupled from the "Shell" (`cmd/app`). To add a Web UI, we effectively just add a new "Shell" (`cmd/api`) that wraps the *exact same* `Ledger` and `Agent` interfaces in HTTP handlers/REST endpoints. |
| **Cloud Deployment** | **High.** The application is a single binary, uses environment variables for config (12-factor friendly), and uses standard PostgreSQL. It is stateless (state is only in DB), making it ready for containerization (Docker) and horizontal scaling. |
| **Thin Client** | **High.** Since the core logic resides on the server (in Go), the client (Web/Desktop) can be very thin, responsible only for rendering UI and sending commands to the Cloud API. |

