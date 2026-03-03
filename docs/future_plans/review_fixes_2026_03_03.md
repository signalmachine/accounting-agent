# Code Review — Fix Plan (2026-03-03)

Findings from a full application review covering architecture, SQL correctness,
security, and UI behaviour. Issues are ordered by severity.

---

## Issue 1 — Upload cleanup deletes from `os.TempDir()` [HIGH]

**File:** `internal/adapters/web/ai.go:130–153`
**File:** `internal/adapters/web/handlers.go:34–36`

### Problem

`startUploadCleanup` deletes every file older than 30 minutes from `h.uploadDir`.
When `UPLOAD_DIR` is not set in `.env`, `h.uploadDir` defaults to `os.TempDir()`
(e.g. `C:\Users\vinod\AppData\Local\Temp`). The goroutine then deletes arbitrary
temp files owned by other processes running on the same machine.

```go
// handlers.go:34–36 — unsafe default
uploadDir := os.Getenv("UPLOAD_DIR")
if uploadDir == "" {
    uploadDir = os.TempDir()   // ← sweeps the entire system temp dir
}
```

### Fix

1. Change the default to a dedicated subdirectory inside `os.TempDir()`:

   ```go
   uploadDir := os.Getenv("UPLOAD_DIR")
   if uploadDir == "" {
       uploadDir = filepath.Join(os.TempDir(), "accounting-agent-uploads")
   }
   if err := os.MkdirAll(uploadDir, 0700); err != nil {
       log.Fatalf("cannot create upload dir: %v", err)
   }
   ```

2. Document `UPLOAD_DIR` in `.env.example` with the recommended dedicated path.

---

## Issue 2 — Path-building inconsistency in upload handling [MEDIUM]

**File:** `internal/adapters/web/ai.go:106` vs `internal/adapters/web/chat.go:300`

### Problem

`ai.go` (write path) uses `filepath.Join`; `chat.go` (read path) uses string
concatenation with a hard-coded forward slash:

```go
// ai.go:106 — correct
destPath := filepath.Join(h.uploadDir, attachmentID)

// chat.go:300 — inconsistent, fragile on Windows
path := h.uploadDir + "/" + id
```

### Fix

Change `chat.go:300` to use `filepath.Join`:

```go
path := filepath.Join(h.uploadDir, id)
```

Add `"path/filepath"` to the import block in `chat.go` if not already present.

---

## Issue 3 — Hardcoded bank account code `"1000"` [MEDIUM]

**File:** `internal/adapters/web/vendors.go:637–640`

### Problem

When the caller of `POST /api/companies/{code}/purchase-orders/{id}/pay` omits
`bank_account_code`, the handler silently defaults to `"1000"`. Any company whose
bank account has a different code gets a cryptic ledger error at commit time with
no actionable feedback.

```go
bankCode := body.BankAccountCode
if bankCode == "" {
    bankCode = "1000"   // ← breaks companies without this account code
}
```

### Fix

Two-part fix:

1. **Remove the silent default.** Require the field explicitly, or look it up from
   `account_rules` using `RuleType = 'BANK_DEFAULT'` (which is already seeded):

   ```go
   bankCode := body.BankAccountCode
   if bankCode == "" {
       // Resolve from account_rules (BANK_DEFAULT already exists for seeded data)
       resolved, err := h.svc.ResolveAccountRule(r.Context(), code, "BANK_DEFAULT")
       if err != nil || resolved == "" {
           writeError(w, r, "bank_account_code is required", "BAD_REQUEST", http.StatusBadRequest)
           return
       }
       bankCode = resolved
   }
   ```

   Alternatively, make `bank_account_code` a required field in the request body
   and return 400 when absent. This is simpler and avoids a new service method.

2. **Add `ResolveAccountRule` to `ApplicationService`** if the lookup approach is
   chosen, delegating to `RuleEngine.ResolveAccount`.

**Simpler near-term fix:** return HTTP 400 when `bank_account_code` is empty
rather than substituting `"1000"`. The PO detail page already sends the field from
the vendor's `ap_account_code`.

---

## Issue 4 — Hardcoded `"INR"` currency default [MEDIUM]

**Files:**
- `internal/adapters/web/accounting.go:347–350` (journal entry handler)
- `internal/adapters/web/orders.go:187–189` (sales order wizard)

### Problem

Both handlers default to `"INR"` when the currency field is blank:

```go
currency := req.Currency
if currency == "" {
    currency = "INR"   // ← wrong for non-INR companies
}
```

This silently overrides the company's actual base currency, which is stored in
`companies.base_currency`.

### Fix

1. **Add `GetCompanyCurrency` to `ApplicationService`** (or expose
   `Company.BaseCurrency` through an existing method like `LoadDefaultCompany`).

2. In both handlers, resolve the company's base currency from the JWT claim
   and use that as the fallback:

   ```go
   currency := req.Currency
   if currency == "" {
       company, err := h.svc.GetCompanyByCode(r.Context(), claims.CompanyCode)
       if err == nil {
           currency = company.BaseCurrency
       }
   }
   ```

   `GetCompanyByCode` (or `fetchCompany` exposed as a service method) already
   exists internally in `appService` — it just needs to be surfaced on the
   interface.

3. **Update the order wizard template** (`order_wizard.templ`) so the currency
   `<select>` pre-selects the company's base currency instead of a hard-coded
   default.

---

## Issue 5 — `GetPO` missing `company_id` SQL filter [MEDIUM]

**File:** `internal/core/purchase_order_service.go:547–581`

### Problem

`GetPO` fetches by `id` only:

```sql
WHERE po.id = $1   -- no AND po.company_id = $X
```

The app layer (`app_service.go:614`) re-checks `po.CompanyID != company.ID` after
the fact, so no current exploit path exists. However, the `PurchaseOrderService`
interface exposes `GetPO(ctx, poID int)` with no company parameter — any future
caller that skips the app-layer check silently gains cross-company read access.

### Fix

1. **Add `company_id` to the SQL filter in `GetPO`:**

   Change the signature to `GetPO(ctx context.Context, companyID, poID int)` and
   update the query:

   ```sql
   WHERE po.id = $1 AND po.company_id = $2
   ```

2. **Update all callers:**
   - `app_service.go`: pass `company.ID` — already resolved at that call site.
   - Internal callers inside `purchase_order_service.go` (`ApprovePO:138`,
     `ReceivePO:543`): both already validate company ownership immediately before
     calling `GetPO`, so they can pass the validated `companyID`.

3. Update the `PurchaseOrderService` interface in
   `internal/core/purchase_order_model.go`.

---

## Issue 6 — No loading indicator during confirm/cancel [LOW]

**File:** `web/templates/pages/chat_home.templ:106–118`, `176–192`

### Problem

JavaScript sets `msg.status = 'confirming'` or `'cancelling'` while the async
fetch is in-flight, but the card template has no matching `x-show` branch:

```html
<!-- action_card -->
<div x-show="msg.status === undefined || msg.status === 'pending'">buttons</div>
<div x-show="msg.status === 'confirmed'">...</div>
<div x-show="msg.status === 'cancelled'">...</div>
<div x-show="msg.status === 'error'">...</div>
<!-- 'confirming' and 'cancelling' → all divs hidden → blank card -->
```

### Fix

Add a loading state div to both the `action_card` and `proposal` card blocks:

```html
<!-- action_card — add after the buttons div -->
<div x-show="msg.status === 'confirming' || msg.status === 'cancelling'"
     class="text-sm text-slate-500 flex items-center gap-1.5">
    <svg class="animate-spin h-4 w-4 text-slate-400" .../>
    Processing…
</div>
```

Apply the same pattern to the proposal card's action section.

---

## Issue 7 — Pending action deleted before execution (no retry on failure) [LOW]

**File:** `internal/adapters/web/chat.go:339`

### Problem

The token is deleted from the pending store *before* `CommitProposal` or
`ExecuteWriteTool` runs. If execution fails, the user cannot retry — they must
restart the full AI interaction.

```go
h.pending.delete(req.Token)   // ← deleted here
switch action.Kind {
case pendingKindJournalEntry:
    if err := h.svc.CommitProposal(...); err != nil {
        writeError(...)   // token already gone — no retry possible
        return
    }
```

### Fix Options

**Option A — Restore token on failure (preferred for UX):**
Only delete after successful execution. On failure, leave the token in the store
so the user can retry. Add an explicit note in the code explaining this is
intentional (i.e. not a double-submit vulnerability because the token is consumed
on first success).

```go
switch action.Kind {
case pendingKindJournalEntry:
    if err := h.svc.CommitProposal(r.Context(), *action.Proposal); err != nil {
        // Re-insert so the user can retry.
        h.pending.put(req.Token, action)
        writeError(w, r, "commit failed: "+err.Error(), "COMMIT_ERROR", http.StatusUnprocessableEntity)
        return
    }
    h.pending.delete(req.Token)
```

**Option B — Document current behaviour:**
Add a comment explaining why the token is deleted first and improve the error
message to explicitly tell the user to send a new message.

---

## Issue 8 — `float64` precision for monetary values in `ExecuteWriteTool` [LOW]

**File:** `internal/app/app_service.go:426–455` (`create_purchase_order`)

### Problem

JSON args are unmarshalled into `float64` struct fields then converted:

```go
type lineIn struct {
    Quantity float64 `json:"quantity"`
    UnitCost float64 `json:"unit_cost"`
}
// ...
Quantity: decimal.NewFromFloat(l.Quantity),   // loses precision for e.g. 0.1
UnitCost: decimal.NewFromFloat(l.UnitCost),
```

`decimal.NewFromFloat(0.1)` produces `0.1000000000000000055511151231257827021181583404541015625`.

### Fix

Change the struct fields to `string` and use `decimal.NewFromString`:

```go
type lineIn struct {
    ProductCode        string `json:"product_code"`
    Description        string `json:"description"`
    Quantity           string `json:"quantity"`
    UnitCost           string `json:"unit_cost"`
    ExpenseAccountCode string `json:"expense_account_code"`
}
// ...
qty, err := decimal.NewFromString(l.Quantity)
// handle parse error
```

Also update the AI tool schema in `internal/ai/tools.go` to mark `quantity` and
`unit_cost` as `"type": "string"` so the model serialises them as strings.
(Requires invoking `/openai-integration` skill before touching `tools.go`.)

---

## Issue 9 — Upload cleanup goroutine not cancellable on shutdown [LOW]

**File:** `internal/adapters/web/ai.go:130–153`

### Problem

`startUploadCleanup` launches an infinite goroutine with no cancellation:

```go
func (h *Handler) startUploadCleanup() {
    go func() {
        ticker := time.NewTicker(10 * time.Minute)
        defer ticker.Stop()
        for range ticker.C {   // no context — runs forever
```

`startPurge` (pendingStore) already accepts a `context.Context` for graceful
shutdown. The upload cleanup should follow the same pattern.

### Fix

Add a `ctx context.Context` parameter (or accept a `chan struct{}` done channel)
mirroring `startPurge`:

```go
func (h *Handler) startUploadCleanup(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(10 * time.Minute)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                // existing cleanup logic
            }
        }
    }()
}
```

Pass the server's root context from `NewHandler` or from `main`.

---

## Issue 10 — No CSRF protection on HTML form routes [LOW]

**File:** `internal/adapters/web/auth.go`, `handlers.go`

### Problem

`POST /login`, `POST /register`, and `POST /logout` are HTML form submissions.
The JWT is stored in an `httpOnly` cookie, which is automatically sent by the
browser on every same-origin request — making these endpoints CSRF targets if
`SameSite` is not enforced.

### Fix (two-layer defence)

1. **Set `SameSite=Strict` (or `Lax`) on the auth cookie** (`auth.go`):

   ```go
   http.SetCookie(w, &http.Cookie{
       Name:     "auth_token",
       Value:    tokenStr,
       HttpOnly: true,
       Secure:   true,         // require HTTPS in production
       SameSite: http.SameSiteStrictMode,
       Path:     "/",
   })
   ```

   `SameSite=Strict` prevents the cookie from being sent on cross-origin requests,
   which blocks most practical CSRF attacks without token-based mitigations.

2. **(Optional — belt and suspenders)** Add a double-submit cookie or synchroniser
   token to the login and register forms if SameSite alone is insufficient for your
   deployment threat model.

---

## Issue 11 — Adapter layer imports `internal/core` directly [INFO]

**Files:** `chat.go:13`, `accounting.go:11`, `cli.go:12`, `display.go:8`

### Problem

CLAUDE.md states: "Adapters must not import `internal/core` directly — they call
`app.ApplicationService` only." Several adapters import `internal/core` for model
types (`core.Proposal`, `core.BSReport`, etc.).

### Fix

Move all display/transport types that adapters need into `internal/app/` result
types or create a dedicated `internal/app/model/` package for shared value types.
This is a larger refactor and lower urgency than the other items — defer until a
natural seam presents itself (e.g. when adding a new domain).

---

## Uncommitted Changes — Commit These

Two files have correct, reviewed changes sitting unstaged:

| File | Change |
|------|--------|
| `internal/adapters/web/chat.go` | Fallback message when `result.Answer` is empty |
| `web/templates/pages/chat_home.templ` | `anyResponse` fallback; `data.message \|\| result.message` fix |
| `web/templates/pages/chat_home_templ.go` | Regenerated from above |

Run `make generate` to verify the generated file is current, then commit all three.

---

## Fix Priority Order

| Priority | Issues | Rationale |
|----------|--------|-----------|
| **Do first** | 1, 2 | Data safety — wrong default `UPLOAD_DIR` can corrupt other processes |
| **Do next** | 3, 4 | Functional correctness — silent wrong defaults break real workflows |
| **Do next** | 5 | Defense-in-depth — SQL should enforce company isolation, not just app layer |
| **Do soon** | 6, 9 | UX and clean shutdown — low effort, visible improvement |
| **Later** | 7, 8, 10 | Low severity; fix 7 and 8 when touching those code paths next |
| **Deferred** | 11 | Architectural refactor — plan separately |
