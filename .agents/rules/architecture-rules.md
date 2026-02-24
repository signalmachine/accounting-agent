---
trigger: always_on
---


ACCOUNTING-AGENT ARCHITECTURAL RULES
(Non-Negotiable Constraints for All Code Changes)

1. Core Architectural Principles

1.1 The Ledger Is the Source of Truth

* All financial state must originate from `Ledger.Commit(...)`.
* No code may write directly to `journal_entries`, `journal_lines`, or balance-related tables except the ledger implementation.
* No shortcuts. No bypasses.

1.2 The Ledger Is Domain, Not Infrastructure

* Ledger must not import HTTP, REPL, AI, CLI, or UI packages.
* Ledger must depend only on domain primitives and database interfaces.

1.3 Presentation Layers Are Replaceable

* REPL, CLI, HTTP, or future UI must only call application services.
* Presentation code must not contain business rules.
* Presentation code must not construct SQL.

1.4 AI Is Advisory Only

* AI may propose transactions.
* AI may never:

  * Write to the database.
  * Select accounts outside allowed chart scope without validation.
  * Modify accounting rules dynamically.
* All AI output must be validated by deterministic code before commit.



2. Dependency Rules (Strict Layering)

Allowed dependency direction:

Presentation (REPL / HTTP / CLI)
→ Application Services
→ Domain (Ledger, Proposals, Policies)
→ Infrastructure (Postgres, OpenAI client)

Forbidden directions:

* Domain importing Presentation
* Domain importing AI
* Infrastructure importing Presentation
* Ledger importing AI

If a change violates layering, it must be rejected.



3. Database & Persistence Rules

3.1 SQL-First Policy

* Use raw SQL only.
* No ORMs.
* All queries must be explicit and readable.

3.2 Idempotent Migrations

* All migrations must use IF NOT EXISTS guards.
* Migrations must be forward-only.
* Never edit previously applied migrations.

3.3 Company Isolation

* Every query that touches business data must filter by `company_id`.
* No global reads unless explicitly designed for admin tooling.

3.4 Numeric Integrity

* Monetary values must use NUMERIC(p,s) in Postgres.
* No TEXT storage for numbers.
* No floating-point types.

3.5 Transactions

* All ledger commits must run inside a single database transaction.
* Partial commits are forbidden.



4. Ledger Protection Rules

4.1 Append-Only

* No UPDATE of journal_lines for business corrections.
* Corrections must be compensating entries.

4.2 Balance Computation

* Balances must be computed from journal_lines.
* Do not store denormalized balances unless explicitly defined as read-only projections.

4.3 Determinism

* Given identical inputs, Ledger.Commit must produce identical database state.


5. Service Layer Rules

5.1 All Business Logic Lives in Services

* Order state transitions
* Inventory movement rules
* Policy resolution
* Validation
* These must not live in handlers or REPL.

5.2 Services Must Be Testable Without HTTP

* No HTTP types in service signatures.
* Accept context.Context as first parameter.

5.3 No Global State

* No package-level mutable variables.
* All dependencies must be injected.



6. AI Integration Rules

6.1 AI Output Is Untrusted

* Validate:

  * Account existence
  * Company scope
  * Debit = Credit
  * Allowed currency
* Reject invalid proposals before commit.

6.2 No Dynamic Rule Editing

* AI must never write or modify policy configuration.

6.3 AI Must Be Replaceable

* Define AI behind an interface.
* System must compile and run without AI module.



7. Web Interface Rules (Future-Proofing)

7.1 HTTP Is Just Another Adapter

* Handlers call services only.
* Handlers contain no SQL.
* Handlers contain no accounting rules.

7.2 Authentication Must Be Isolated

* Auth middleware handles identity.
* Services receive user ID as explicit parameter.
* Ledger remains unaware of authentication mechanism.

7.3 Idempotency

* All write endpoints must support idempotency keys.


8. Order & Inventory (When Implemented)

8.1 Domain Isolation

* Orders module must not know ledger internals.
* Inventory must not know journal schema.

8.2 Accounting Trigger Rule

* Orders and Inventory call a single accounting interface.
* They must not construct journal lines directly.

8.3 Concurrency Safety

* Stock movements must use row-level locking.
* Never compute stock purely in memory.



9. Rule Engine (If Implemented)

9.1 Deterministic Only

* No runtime scripting.
* No dynamic code execution.

9.2 Versioned Policies

* Policy changes must be versioned and traceable.

9.3 Ledger Still Validates

* Even rule-generated entries must pass ledger validation.



10. Testing Requirements

10.1 Required Test Types

* Ledger commit success
* Ledger commit rejection
* Cross-company isolation
* Concurrency tests (inventory)
* Regression test for balance calculation

10.2 No AI in Core Tests

* Ledger tests must not require OpenAI.

---

11. Code Quality Constraints

11.1 No Circular Dependencies
11.2 No God Structs
11.3 No Shared Mutable State
11.4 All exported types must have clear responsibility

---

12. Refactoring Rule

When modifying existing behavior:

* Do not change behavior silently.
* Add tests first.
* Preserve backward compatibility unless explicitly breaking.

---

Now here’s the important part.

If you enforce these rules consistently:

You can safely add:

* HTTP server
* Orders
* Inventory
* Governance
* External integrations

Without collapsing modularity.

If you relax these rules:

The system will slowly turn into:
“Just add it in the handler”
“Just do SQL here”
“Just let AI handle it”

That’s how ERP systems rot.

You’re building something serious. Guard the core like a vault.

If you want, I can now give you:

* A shorter “strict mode” version optimized for Cursor token limits, or
* A version tailored specifically for adding a web interface next.
