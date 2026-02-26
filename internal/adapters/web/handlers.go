package web

import (
	"encoding/json"
	"net/http"

	"accounting-agent/internal/app"

	"github.com/go-chi/chi/v5"
)

// Handler holds the ApplicationService and the chi router.
type Handler struct {
	svc    app.ApplicationService
	router chi.Router
}

// NewHandler creates and wires the chi router with all routes.
func NewHandler(svc app.ApplicationService, allowedOrigins string) http.Handler {
	h := &Handler{svc: svc}

	r := chi.NewRouter()
	r.Use(RequestID)
	r.Use(Logger)
	r.Use(Recoverer)
	r.Use(CORS(allowedOrigins))

	// ── Health ────────────────────────────────────────────────────────────────
	r.Get("/api/health", h.health)

	// ── Accounting ────────────────────────────────────────────────────────────
	r.Get("/api/companies/{code}/trial-balance", notImplemented)
	r.Get("/api/companies/{code}/accounts/{accountCode}/statement", notImplemented)
	r.Get("/api/companies/{code}/reports/pl", notImplemented)
	r.Get("/api/companies/{code}/reports/balance-sheet", notImplemented)
	r.Post("/api/companies/{code}/reports/refresh", notImplemented)

	// ── Sales ─────────────────────────────────────────────────────────────────
	r.Get("/api/companies/{code}/customers", notImplemented)
	r.Get("/api/companies/{code}/orders", notImplemented)
	r.Post("/api/companies/{code}/orders", notImplemented)
	r.Get("/api/companies/{code}/orders/{ref}", notImplemented)
	r.Post("/api/companies/{code}/orders/{ref}/confirm", notImplemented)
	r.Post("/api/companies/{code}/orders/{ref}/ship", notImplemented)
	r.Post("/api/companies/{code}/orders/{ref}/invoice", notImplemented)
	r.Post("/api/companies/{code}/orders/{ref}/payment", notImplemented)

	// ── Inventory ─────────────────────────────────────────────────────────────
	r.Get("/api/companies/{code}/products", notImplemented)
	r.Get("/api/companies/{code}/warehouses", notImplemented)
	r.Get("/api/companies/{code}/stock", notImplemented)
	r.Post("/api/companies/{code}/stock/receive", notImplemented)

	// ── AI ────────────────────────────────────────────────────────────────────
	r.Post("/api/companies/{code}/ai/interpret", notImplemented)
	r.Post("/api/companies/{code}/ai/validate", notImplemented)
	r.Post("/api/companies/{code}/ai/commit", notImplemented)

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

// writeJSON (already defined in errors.go — kept here as reminder of usage).
// All handlers follow: parse → validate → call svc → format JSON.

// companyCode extracts the {code} URL parameter.
func companyCode(r *http.Request) string {
	return chi.URLParam(r, "code")
}

// decodeJSON decodes the request body into v and returns false + writes 400 on error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, r, "invalid JSON body: "+err.Error(), "BAD_REQUEST", http.StatusBadRequest)
		return false
	}
	return true
}
