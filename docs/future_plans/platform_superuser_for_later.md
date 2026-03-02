# Platform Superuser — Deferred Feature

## What This Is

A platform-level superuser is a special account operated by **Signal Machine** (the service provider) that can access and manage any tenant's data across all companies. This is distinct from a per-company ADMIN, who can only manage their own company.

**Status: Deliberately deferred.** Do not implement until multi-tenancy (see `multi_tenancy.md`) is stable in production and there is a clear operational need.

---

## Why It Is Deferred

| Concern | Detail |
|---|---|
| Security risk | Bypasses `company_id` filters — a bug here exposes all tenant data |
| Audit requirements | Every cross-tenant action must be logged with who, what, when, and why |
| Complexity | Requires a separate auth path, separate role, and careful middleware design |
| Not needed to launch | Direct database access (read-only) is sufficient for support during early production |
| Trust implications | Customers must be informed (via terms of service) that this role exists |

---

## When to Revisit

Consider implementing when **all** of the following are true:

- Multi-tenancy (MT-1 through MT-3) is stable and has been running in production.
- The support burden (e.g. debugging customer data issues) justifies the complexity.
- An audit log is in place for all write operations.
- Legal/terms of service have been reviewed to cover platform access.

---

## Proposed Design (for future reference)

### Role
A new role `PLATFORM_ADMIN` stored outside the per-company `users` table — either a separate `platform_users` table or a flag on `users` with `company_id = NULL`.

### Auth
- Separate login endpoint (`/platform/login`) or a flag in the existing login flow.
- JWT carries `role: PLATFORM_ADMIN` and no `company_id`.
- Middleware allows `PLATFORM_ADMIN` to pass a `company_id` header/query param to scope requests to a specific tenant.

### Audit Log
Every action taken by a `PLATFORM_ADMIN` is written to a `platform_audit_log` table:
```
platform_audit_log(id, platform_user_id, company_id, action, detail, created_at)
```
No exceptions — this table is the accountability trail.

### Access Controls
- Read access to all tenant data: permitted for support/debugging.
- Write access: only via explicit break-glass procedures, never from the standard UI.
- Tenant data export/deletion: only via a purpose-built admin tool, not the main application.

### UI
A separate `/platform` admin panel, entirely distinct from the main application UI. Never mixed into the per-company screens.

---

## Interim Workaround (until this is built)

For support and debugging during early production:
- Use **direct read-only database access** via `psql` or a DB GUI tool.
- Keep a separate `DATABASE_READONLY_URL` connection string for this purpose.
- All access must be manually logged in an internal ops log.

This is sufficient until scale or support volume demands a proper solution.
