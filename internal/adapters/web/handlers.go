package web

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"

	"accounting-agent/internal/app"
	webui "accounting-agent/web"

	"github.com/go-chi/chi/v5"
)

// Handler holds the ApplicationService, the chi router, and the pending action store.
type Handler struct {
	svc        app.ApplicationService
	router     chi.Router
	pending    *pendingStore
	jwtSecret  string
	fileServer http.Handler
	uploadDir  string // directory for temporary attachment uploads
}

// NewHandler creates and wires the chi router with all routes.
func NewHandler(svc app.ApplicationService, allowedOrigins, jwtSecret string) http.Handler {
	staticFS, err := fs.Sub(webui.Static, "static")
	if err != nil {
		panic("web/static embed sub-FS failed: " + err.Error())
	}

	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = os.TempDir()
	}

	h := &Handler{
		svc:        svc,
		pending:    newPendingStore(),
		jwtSecret:  jwtSecret,
		fileServer: http.FileServer(http.FS(staticFS)),
		uploadDir:  uploadDir,
	}

	// Start background maintenance goroutines.
	h.pending.startPurge(context.Background())
	h.startUploadCleanup()

	r := chi.NewRouter()
	r.Use(RequestID)
	r.Use(Logger)
	r.Use(Recoverer)
	r.Use(CORS(allowedOrigins))

	// ── Health (public) ───────────────────────────────────────────────────────
	r.Get("/api/health", h.health)

	// ── Auth (public API) ─────────────────────────────────────────────────────
	r.Post("/api/auth/login", h.login)
	r.Post("/api/auth/logout", h.logout)

	// ── Static files served at /static/* ─────────────────────────────────────
	r.Get("/static/*", func(w http.ResponseWriter, req *http.Request) {
		http.StripPrefix("/static", h.fileServer).ServeHTTP(w, req)
	})

	// ── Browser login/logout (public HTML) ───────────────────────────────────
	r.Get("/login", h.loginPage)
	r.Post("/login", h.loginFormSubmit)
	r.Post("/logout", h.logoutPage)

	// ── Protected browser routes (redirect to /login if unauthenticated) ─────
	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuthBrowser)
		r.Get("/", h.chatHome) // WF5: chat home is the primary interface
		r.Get("/dashboard", h.dashboardPage)
		// WF4 accounting screens
		r.Get("/reports/trial-balance", h.trialBalancePage)
		r.Get("/reports/pl", h.plReportPage)
		r.Get("/reports/balance-sheet", h.balanceSheetPage)
		r.Get("/reports/statement", h.accountStatementPage)
		r.Get("/accounting/journal-entry", h.journalEntryPage)
		// WD0 — Sales / Inventory pages
		r.Get("/sales/customers", h.customersListPage)
		r.Get("/sales/customers/{code}", notImplementedPage) // detail — WD0 follow-on
		r.Get("/sales/orders", h.ordersListPage)
		r.Get("/sales/orders/new", h.orderWizardPage)
		r.Post("/sales/orders/new", h.orderCreateAction)
		r.Get("/sales/orders/{ref}", h.orderDetailPage)
		r.Get("/inventory/products", h.productsListPage)
		r.Get("/inventory/stock", h.stockPage)
		// WD1 — Purchases pages
		r.Get("/purchases/vendors", h.vendorsListPage)
		r.Get("/purchases/vendors/new", h.vendorCreatePage)
		r.Post("/purchases/vendors/new", h.vendorCreateAction)
		r.Get("/purchases/orders", h.purchaseOrdersListPage)
		r.Get("/purchases/orders/new", h.poWizardPage)
		r.Post("/purchases/orders/new", h.poCreateAction)
		r.Get("/purchases/orders/{id}", h.poDetailPage)
		r.Get("/settings/users", notImplementedPage)
		r.Get("/settings/rules", notImplementedPage)
	})

	// ── Protected API routes (return 401 JSON if unauthenticated) ────────────
	r.Group(func(r chi.Router) {
		r.Use(h.RequireAuth)

		// File upload: body limit is managed inside the handler (multipart, up to 50 MB).
		r.Post("/chat/upload", h.chatUpload)

		// All other protected endpoints: 1 MB body limit to prevent unbounded request abuse.
		r.Group(func(r chi.Router) {
			r.Use(RequestBodyLimit(1 << 20)) // 1 MB

			// Auth
			r.Get("/api/auth/me", h.me)

			// Chat — primary endpoints (WF5)
			r.Post("/chat", h.chatMessage)
			r.Post("/chat/confirm", h.chatConfirm)
			r.Post("/chat/clear", h.chatClear)

			// Chat — legacy endpoints (kept for backward compat with old static frontend)
			r.Post("/api/chat/message", h.chatMessage)
			r.Post("/api/chat/confirm", h.chatConfirm)

			// ── Accounting (WF4) ──────────────────────────────────────────────────
			r.Get("/api/companies/{code}/trial-balance", h.apiTrialBalance)
			r.Get("/api/companies/{code}/accounts/{accountCode}/statement", h.apiAccountStatement)
			r.Get("/api/companies/{code}/reports/pl", h.apiProfitAndLoss)
			r.Get("/api/companies/{code}/reports/balance-sheet", h.apiBalanceSheet)
			r.Post("/api/companies/{code}/reports/refresh", h.apiRefreshViews)
			r.Post("/api/companies/{code}/journal-entries", h.apiPostJournalEntry)
			r.Post("/api/companies/{code}/journal-entries/validate", h.apiValidateJournalEntry)

			// ── Sales (WD0) ───────────────────────────────────────────────────────
			r.Get("/api/companies/{code}/customers", h.apiListCustomers)
			r.Get("/api/companies/{code}/orders", h.apiListOrders)
			r.Post("/api/companies/{code}/orders", h.apiCreateOrder)
			r.Get("/api/companies/{code}/orders/{ref}", h.apiGetOrder)
			r.Post("/api/companies/{code}/orders/{ref}/confirm", h.apiConfirmOrder)
			r.Post("/api/companies/{code}/orders/{ref}/ship", h.apiShipOrder)
			r.Post("/api/companies/{code}/orders/{ref}/invoice", h.apiInvoiceOrder)
			r.Post("/api/companies/{code}/orders/{ref}/payment", h.apiPaymentOrder)

			// ── Inventory (WD0) ───────────────────────────────────────────────────
			r.Get("/api/companies/{code}/products", h.apiListProducts)
			r.Get("/api/companies/{code}/warehouses", notImplemented)
			r.Get("/api/companies/{code}/stock", notImplemented)
			r.Post("/api/companies/{code}/stock/receive", notImplemented)

			// ── Purchases (WD1) ──────────────────────────────────────────────────
			r.Get("/api/companies/{code}/vendors", h.apiListVendors)
			r.Post("/api/companies/{code}/vendors", h.apiCreateVendor)
			r.Get("/api/companies/{code}/vendors/{vendorCode}", h.apiGetVendor)
			r.Get("/api/companies/{code}/purchase-orders", h.apiListPurchaseOrders)
			r.Post("/api/companies/{code}/purchase-orders", h.apiCreatePurchaseOrder)
			r.Get("/api/companies/{code}/purchase-orders/{id}", h.apiGetPurchaseOrder)
			r.Post("/api/companies/{code}/purchase-orders/{id}/approve", h.apiApprovePO)
			r.Post("/api/companies/{code}/purchase-orders/{id}/receive", h.apiReceivePO)
			r.Post("/api/companies/{code}/purchase-orders/{id}/invoice", h.apiInvoicePO)
			r.Post("/api/companies/{code}/purchase-orders/{id}/pay", h.apiPayPO)

			// ── AI (legacy admin endpoints — company-scoped) ──────────────────────
			r.Post("/api/companies/{code}/ai/interpret", notImplemented)
			r.Post("/api/companies/{code}/ai/validate", notImplemented)
			r.Post("/api/companies/{code}/ai/commit", notImplemented)
		})
	})

	h.router = r
	return r
}

// health returns service status and the loaded company code.
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	company, err := h.svc.LoadDefaultCompany(r.Context())
	companyCode := ""
	if err == nil && company != nil {
		companyCode = company.CompanyCode
	}

	type response struct {
		Status  string `json:"status"`
		Company string `json:"company"`
	}

	writeJSON(w, response{Status: "ok", Company: companyCode})
}

// companyCode extracts the {code} URL parameter.
func companyCode(r *http.Request) string {
	return chi.URLParam(r, "code")
}

// decodeJSON decodes the request body into v and returns false + writes an appropriate
// error response on failure. Returns HTTP 413 when the body exceeds the size limit set
// by RequestBodyLimit middleware; HTTP 400 for all other decode errors.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, r, "request body too large", "REQUEST_TOO_LARGE", http.StatusRequestEntityTooLarge)
			return false
		}
		writeError(w, r, "invalid JSON body: "+err.Error(), "BAD_REQUEST", http.StatusBadRequest)
		return false
	}
	return true
}
