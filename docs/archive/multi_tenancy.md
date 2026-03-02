# Multi-Tenancy Implementation Plan

> **Archived 2026-03-02. All three phases complete.**

## Overview

Add proper SaaS multi-tenancy so each company (tenant) is fully isolated, users are bound to a company at registration, and the per-company admin can manage users within their own company. The data isolation layer already existed (`company_id` on every business table). This plan covered only what was missing.

---

## Implementation Order

| Phase | Deliverable | Status | Completed |
|---|---|---|---|
| MT-1 | User-to-company binding — JWT carries `CompanyCode` + `Username`, `LoadDefaultCompany()` removed from web path | ✅ Done | 2026-03-02 |
| MT-2 | Self-service company registration (`GET /register`, `POST /register`) | ✅ Done | 2026-03-02 |
| MT-3 | Per-company user management by ADMIN (role change, deactivate/activate) | ✅ Done | 2026-03-02 |

---

## What Was Built

### Phase MT-1 — User-to-Company Binding

**Migrations:** `016_users.sql` — `company_id` FK on `users`; `017_seed_admin_user.sql` — admin user assigned to seed company.

**Changes:**
- JWT claims extended: `UserID`, `CompanyID`, `CompanyCode`, `Username`, `Role`.
- `RequireAuth` / `RequireAuthBrowser` middleware injects all claims into `context.Context`.
- `buildAppLayoutData` reads username/role/companyCode from JWT — zero DB calls per page render.
- `requireCompanyAccess` compares `claims.CompanyCode` directly — zero DB calls per API request.
- All `LoadDefaultCompany()` calls removed from web handlers; retained for health endpoint and REPL/CLI path only.

**Outcome:** Every authenticated web request is automatically scoped to the correct company without any env var.

---

### Phase MT-2 — Company Registration (Sign-Up Flow)

**Migration:** `027_company_unique_name.sql` — UNIQUE constraint on `companies.name`.

**New files:**
- `web/templates/pages/register.templ` — Registration form (Company Name, Username, Email, Password).

**Changed files:**
- `internal/app/request_types.go` — `RegisterCompanyRequest`.
- `internal/app/service.go` — `RegisterCompany` added to `ApplicationService`.
- `internal/app/app_service.go` — `RegisterCompany` implementation (atomic TX), `generateUniqueCompanyCode`, `validateRegistrationPassword`.
- `internal/adapters/web/pages.go` — `registerPage` + `registerFormSubmit`.
- `internal/adapters/web/handlers.go` — Public routes `GET /register`, `POST /register`.
- `web/templates/pages/login.templ` — "Don't have an account? Create one" link added.

**Key details:**
- Company code auto-generated from company name (first 4 uppercase alphanumeric chars; numeric suffix on collision).
- Password policy enforced in app layer: minimum 8 characters, at least one uppercase letter, at least one digit.
- Company creation and admin user creation are a single atomic DB transaction.
- On success: JWT issued, user redirected to `/`.

**Outcome:** Self-service onboarding. Each sign-up creates a fully isolated tenant.

---

### Phase MT-3 — Per-Company User Management

**Changes:**
- `internal/core/user_model.go` — `UpdateUserRole` + `SetUserActive` added to `UserService` interface.
- `internal/core/user_service.go` — Both implemented; all queries scoped by `company_id`.
- `internal/app/request_types.go` — `UpdateUserRoleRequest`.
- `internal/app/service.go` — `UpdateUserRole` + `SetUserActive` added to `ApplicationService`.
- `internal/app/app_service.go` — Both implemented (resolve companyID, delegate to UserService).
- `web/templates/pages/users_list.templ` — Actions column: inline role-change dropdown + Activate/Deactivate button. ADMIN now included in create-user role dropdown. Logged-in user shown as "You" with no action buttons (self-protection).
- `internal/adapters/web/users.go` — `usersUpdateRoleAction`, `usersToggleActiveAction` handlers added. ADMIN restriction removed from user creation (all roles now creatable via UI).
- `internal/adapters/web/handlers.go` — `POST /settings/users/{id}/role`, `POST /settings/users/{id}/active` (both ADMIN-only).

**Safety guards:**
- A user cannot change their own role (would require a re-login to take effect and could cause confusion).
- A user cannot deactivate their own account (self-lock-out prevention).

**Outcome:** Each company ADMIN self-manages their team with full role control. No platform intervention needed.

---

## Final JWT Claims

```json
{
  "user_id": 1,
  "username": "alice",
  "role": "ADMIN",
  "company_id": 42,
  "company_code": "ACME"
}
```

---

## What Did Not Change

- `company_id` filtering on all business tables — already correct before MT-1.
- All domain services (`LedgerService`, `OrderService`, `InventoryService`, etc.) — untouched.
- The AI agent — untouched.
- The REPL/CLI path — still uses `COMPANY_CODE` env var for multi-company setups.

---

## Out of Scope (deferred)

- Platform-level superuser who can access all tenants.
- Cross-company reporting or data migration tools.
- Company deletion or tenant offboarding.
- Email-based invitations.
- SSO / OAuth login.
- Token revocation (JWT is stateless; deferred to post-MVP).
