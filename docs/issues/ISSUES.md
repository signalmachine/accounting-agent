## Code base issues

- internal/adapters/web/chat.go:133 and the REST handlers such
  as internal/adapters/web/orders.go:295, internal/adapters/web/
  orders.go:354, internal/adapters/web/orders.go:402 never compare the
  authenticated user’s CompanyID from AuthClaims with the company_code
  supplied in the body or URL. Any logged-in operator can therefore read
  or mutate data for every company simply by changing the company code
  in the request, which breaks the multi-company isolation model. Each
  handler needs to extract authFromContext, verify that the request’s
  company matches, and return 403 when it doesn’t.
- Write-tool execution paths trust only the numeric PO ID
  and never verify company ownership. For example internal/app/
  app_service.go:668 (ApprovePurchaseOrder) forwards poID straight
  to purchase_order_service without checking that the PO belongs
  to companyCode, and internal/core/purchase_order_service.go:143,
  internal/core/purchase_order_service.go:225, internal/
  core/purchase_order_service.go:300, and internal/core/
  purchase_order_service.go:352 operate solely on the ID. A malicious
  user (or compromised agent) who learns another company’s PO ID can
  approve, receive, invoice, or pay it. These service methods must
  accept a company identifier and include it in their WHERE clauses (or
  prefetch + compare) so cross-company IDs cannot be acted on.
- internal/core/inventory_service.go:155-248 commits the inventory/
  warehouse updates and only afterwards calls ledger.Commit. If
  the ledger write fails (network hiccup, validation error, etc.),
  the goods receipt stays in inventory and movements tables, but no
  compensating journal entry is posted. The same function is called
  from ReceivePurchaseOrder, so PO receipts can leave inventory and
  accounting out of sync. Run both the stock mutation and ledger insert
  in the same database transaction (e.g., use Ledger.CommitInTx) or roll
  back the inventory transaction when the ledger write fails.
- internal/core/purchase_order_service.go:225-306 allows any positive
  QtyReceived per call but never compares it against what was ordered or
  previously received. Because no qty_received accumulator exists per PO
  line, repeated calls can over-receive a line indefinitely, inflating
  inventory and double-booking accruals. Persist cumulative receipt
  quantities per line (or sum inventory_movements by po_line_id) and
  enforce received ≤ ordered.
- Authentication controls are weak: internal/adapters/web/auth.go:129-
  141 sets auth_token without Secure: true, so cookies travel over plain
  HTTP if TLS termination is misconfigured, and cmd/server/main.go:45-
  51 starts the server with the hard-coded insecure-default-change-me
  secret whenever JWT_SECRET isn’t provided (only a log warning). In
  production this makes session tokens predictable/stealable. Require
  JWT_SECRET, fail fast if absent, and set both Secure and SameSite on
  the cookie.

## Web UI issues

- web/templates/pages/chat_home.templ:164 reads the active
  company code from document.body.dataset.companyCode, which is
  populated in web/templates/layouts/app_layout.templ:19 from
  layouts.AppLayoutData.CompanyCode. However, internal/adapters/web/
  pages.go:115-123 fills that field by calling LoadDefaultCompany
  instead of using the authenticated user’s company from AuthClaims. In
  any multi-company database where COMPANY_CODE isn’t forced via env,
  LoadDefaultCompany returns an error (internal/app/app_service.go:1555-
  1582), leaving CompanyCode empty. The browser therefore posts
  {company_code:""} and /chat immediately rejects the request (internal/
  adapters/web/chat.go:138-140). Since this happens before the SSE
  stream is opened, the frontend never receives an event and the user
  sees “nothing”, even though the REPL—where you explicitly pass a
  company code—works. Root cause: the web UI never derives the user’s
  company and cannot satisfy /chat’s required company_code parameter.
- The chat UI has no fallback when the POST fails. In
  chat_home.templ:250-313 the code always assumes resp.body is an
  SSE stream and silently discards any non-event: payload. When /chat
  returns JSON errors (e.g., missing company_code or 401), the reader
  loop just exhausts the body without emitting an error message, so the
  operator gets no visual feedback about why their message vanished.

Why the chat interface stalls

1. Operator logs into the browser; JWT identifies CompanyID, but the
   layout code never uses it.
2. buildAppLayoutData calls LoadDefaultCompany, which errors because
   multiple companies exist; the error is ignored and CompanyCode stays
   blank.
3. The Alpine component reads that blank value and sends /chat a
   request with company_code: "".
4. chatMessage rejects the payload (text and company_code are
   required) and returns a JSON error response instead of an SSE stream.
5. The frontend keeps waiting for SSE events, never surfaces the
   error, and remains stuck on the previous state. Meanwhile the REPL
   continues to work because it is provided an explicit company code and
   satisfies the ledger layer’s requirements.

To fix this, the UI needs to pull the company code from the
authenticated session (available via AuthClaims.CompanyID/CompanyID
→ company_code lookup) and the JS should detect non-200 responses to
show the error to the user.
