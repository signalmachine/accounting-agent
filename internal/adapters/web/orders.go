package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"accounting-agent/internal/app"
	"accounting-agent/web/templates/pages"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
)

// ── Browser page handlers ─────────────────────────────────────────────────────

// customersListPage handles GET /sales/customers.
func (h *Handler) customersListPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Customers", "customers")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	result, err := h.svc.ListCustomers(r.Context(), company.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load customers: " + err.Error()
		d.FlashKind = "error"
		result = &app.CustomerListResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.CustomersList(d, result).Render(r.Context(), w)
}

// productsListPage handles GET /inventory/products.
func (h *Handler) productsListPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Products", "products")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	products, err := h.svc.ListProducts(r.Context(), company.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load products: " + err.Error()
		d.FlashKind = "error"
		products = &app.ProductListResult{}
	}

	stock, _ := h.svc.GetStockLevels(r.Context(), company.CompanyCode)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.ProductsList(d, products, stock).Render(r.Context(), w)
}

// stockPage handles GET /inventory/stock.
func (h *Handler) stockPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Stock Levels", "stock")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	result, err := h.svc.GetStockLevels(r.Context(), company.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load stock levels: " + err.Error()
		d.FlashKind = "error"
		result = &app.StockResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.StockLevels(d, result).Render(r.Context(), w)
}

// ordersListPage handles GET /sales/orders.
func (h *Handler) ordersListPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Sales Orders", "orders")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	var statusPtr *string
	if statusFilter != "" {
		statusPtr = &statusFilter
	}

	// Surface flash errors passed via query param from lifecycle redirects.
	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	result, err := h.svc.ListOrders(r.Context(), company.CompanyCode, statusPtr)
	if err != nil {
		d.FlashMsg = "Failed to load orders: " + err.Error()
		d.FlashKind = "error"
		result = &app.OrderListResult{CompanyCode: company.CompanyCode}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.OrdersList(d, result, statusFilter).Render(r.Context(), w)
}

// orderDetailPage handles GET /sales/orders/{ref}.
func (h *Handler) orderDetailPage(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	d := h.buildAppLayoutData(r, "Order", "orders")

	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	result, err := h.svc.GetOrder(r.Context(), ref, company.CompanyCode)
	if err != nil {
		d.FlashMsg = "Order not found: " + err.Error()
		d.FlashKind = "error"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.OrderDetail(d, nil, company.CompanyCode).Render(r.Context(), w)
		return
	}

	if result.Order != nil {
		d.Title = "Order " + result.Order.OrderNumber
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.OrderDetail(d, result.Order, company.CompanyCode).Render(r.Context(), w)
}

// orderWizardPage handles GET /sales/orders/new.
func (h *Handler) orderWizardPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "New Sales Order", "orders")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	if fe := r.URL.Query().Get("error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	customers, err := h.svc.ListCustomers(r.Context(), company.CompanyCode)
	if err != nil {
		customers = &app.CustomerListResult{}
	}

	products, err := h.svc.ListProducts(r.Context(), company.CompanyCode)
	if err != nil {
		products = &app.ProductListResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.OrderWizard(d, customers, products, company.CompanyCode).Render(r.Context(), w)
}

// orderCreateAction handles POST /sales/orders/new — HTML form submission.
func (h *Handler) orderCreateAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/sales/orders/new?error=invalid+form", http.StatusSeeOther)
		return
	}

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Redirect(w, r, "/sales/orders?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	req := app.CreateOrderRequest{
		CompanyCode:  company.CompanyCode,
		CustomerCode: r.FormValue("customer_code"),
		OrderDate:    r.FormValue("order_date"),
		Currency:     r.FormValue("currency"),
		Notes:        r.FormValue("notes"),
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}
	if req.OrderDate == "" {
		req.OrderDate = time.Now().Format("2006-01-02")
	}

	// Parse dynamic line items: line_product_code[0], line_quantity[0], line_unit_price[0]
	for i := 0; ; i++ {
		pc := r.FormValue(fmt.Sprintf("line_product_code[%d]", i))
		if pc == "" {
			break
		}
		qtyStr := r.FormValue(fmt.Sprintf("line_quantity[%d]", i))
		priceStr := r.FormValue(fmt.Sprintf("line_unit_price[%d]", i))

		qty, qErr := decimal.NewFromString(qtyStr)
		price, _ := decimal.NewFromString(priceStr)

		if qErr != nil || qty.IsZero() {
			continue
		}
		req.Lines = append(req.Lines, app.OrderLineInput{
			ProductCode: pc,
			Quantity:    qty,
			UnitPrice:   price,
		})
	}

	if len(req.Lines) == 0 {
		http.Redirect(w, r, "/sales/orders/new?error=at+least+one+line+required", http.StatusSeeOther)
		return
	}

	result, err := h.svc.CreateOrder(r.Context(), req)
	if err != nil {
		http.Redirect(w, r, "/sales/orders/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/sales/orders/%d", result.Order.ID), http.StatusSeeOther)
}

// ── API handlers ──────────────────────────────────────────────────────────────

// apiListCustomers handles GET /api/companies/{code}/customers.
func (h *Handler) apiListCustomers(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	result, err := h.svc.ListCustomers(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Customers)
}

// apiListProducts handles GET /api/companies/{code}/products.
func (h *Handler) apiListProducts(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	result, err := h.svc.ListProducts(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Products)
}

// apiListOrders handles GET /api/companies/{code}/orders.
func (h *Handler) apiListOrders(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	statusFilter := r.URL.Query().Get("status")
	var statusPtr *string
	if statusFilter != "" {
		statusPtr = &statusFilter
	}
	result, err := h.svc.ListOrders(r.Context(), code, statusPtr)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Orders)
}

// apiGetOrder handles GET /api/companies/{code}/orders/{ref}.
func (h *Handler) apiGetOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	ref := chi.URLParam(r, "ref")
	result, err := h.svc.GetOrder(r.Context(), ref, code)
	if err != nil {
		writeError(w, r, err.Error(), "NOT_FOUND", http.StatusNotFound)
		return
	}
	writeJSON(w, result.Order)
}

// apiCreateOrder handles POST /api/companies/{code}/orders.
// Body: { customer_code, order_date?, currency?, notes?, lines: [{product_code, quantity, unit_price?}] }
func (h *Handler) apiCreateOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)

	var body struct {
		CustomerCode string `json:"customer_code"`
		OrderDate    string `json:"order_date"`
		Currency     string `json:"currency"`
		Notes        string `json:"notes"`
		Lines        []struct {
			ProductCode string `json:"product_code"`
			Quantity    string `json:"quantity"`
			UnitPrice   string `json:"unit_price"`
		} `json:"lines"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.CustomerCode == "" {
		writeError(w, r, "customer_code is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if len(body.Lines) == 0 {
		writeError(w, r, "at least one line is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	req := app.CreateOrderRequest{
		CompanyCode:  code,
		CustomerCode: body.CustomerCode,
		Currency:     body.Currency,
		OrderDate:    body.OrderDate,
		Notes:        body.Notes,
	}

	for i, l := range body.Lines {
		qty, err := decimal.NewFromString(l.Quantity)
		if err != nil || qty.IsZero() {
			writeError(w, r, fmt.Sprintf("line %d: invalid quantity", i+1), "BAD_REQUEST", http.StatusBadRequest)
			return
		}
		price, _ := decimal.NewFromString(l.UnitPrice)
		req.Lines = append(req.Lines, app.OrderLineInput{
			ProductCode: l.ProductCode,
			Quantity:    qty,
			UnitPrice:   price,
		})
	}

	result, err := h.svc.CreateOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, result.Order)
}

// apiConfirmOrder handles POST /api/companies/{code}/orders/{ref}/confirm.
func (h *Handler) apiConfirmOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	ref := chi.URLParam(r, "ref")
	result, err := h.svc.ConfirmOrder(r.Context(), ref, code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Order)
}

// apiShipOrder handles POST /api/companies/{code}/orders/{ref}/ship.
func (h *Handler) apiShipOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	ref := chi.URLParam(r, "ref")
	result, err := h.svc.ShipOrder(r.Context(), ref, code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Order)
}

// apiInvoiceOrder handles POST /api/companies/{code}/orders/{ref}/invoice.
func (h *Handler) apiInvoiceOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	ref := chi.URLParam(r, "ref")
	result, err := h.svc.InvoiceOrder(r.Context(), ref, code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Order)
}

// apiPaymentOrder handles POST /api/companies/{code}/orders/{ref}/payment.
// Body: { bank_account_code? } (optional; defaults to BANK_DEFAULT rule).
func (h *Handler) apiPaymentOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	ref := chi.URLParam(r, "ref")

	var body struct {
		BankAccountCode string `json:"bank_account_code"`
	}
	// Best-effort decode; bank_account_code is optional.
	_ = json.NewDecoder(r.Body).Decode(&body)

	result, err := h.svc.RecordPayment(r.Context(), ref, body.BankAccountCode, code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Order)
}
