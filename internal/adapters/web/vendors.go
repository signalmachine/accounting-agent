package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"accounting-agent/internal/app"
	"accounting-agent/web/templates/pages"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
)

// ── Browser page handlers ─────────────────────────────────────────────────────

// vendorsListPage handles GET /purchases/vendors.
func (h *Handler) vendorsListPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Vendors", "vendors")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}
	if fs := r.URL.Query().Get("flash_success"); fs != "" {
		d.FlashMsg = fs
		d.FlashKind = "success"
	}

	result, err := h.svc.ListVendors(r.Context(), company.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load vendors: " + err.Error()
		d.FlashKind = "error"
		result = &app.VendorsResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.VendorsList(d, result).Render(r.Context(), w)
}

// vendorCreatePage handles GET /purchases/vendors/new.
func (h *Handler) vendorCreatePage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "New Vendor", "vendors")

	if fe := r.URL.Query().Get("error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.VendorForm(d).Render(r.Context(), w)
}

// vendorCreateAction handles POST /purchases/vendors/new — HTML form submission.
func (h *Handler) vendorCreateAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/purchases/vendors/new?error=invalid+form", http.StatusSeeOther)
		return
	}

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Redirect(w, r, "/purchases/vendors?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	paymentTerms := 30
	if pt := r.FormValue("payment_terms_days"); pt != "" {
		if n, err := strconv.Atoi(pt); err == nil && n > 0 {
			paymentTerms = n
		}
	}

	apAccountCode := r.FormValue("ap_account_code")
	if apAccountCode == "" {
		apAccountCode = "2000"
	}

	req := app.CreateVendorRequest{
		CompanyCode:               company.CompanyCode,
		Code:                      r.FormValue("code"),
		Name:                      r.FormValue("name"),
		ContactPerson:             r.FormValue("contact_person"),
		Email:                     r.FormValue("email"),
		Phone:                     r.FormValue("phone"),
		Address:                   r.FormValue("address"),
		PaymentTermsDays:          paymentTerms,
		APAccountCode:             apAccountCode,
		DefaultExpenseAccountCode: r.FormValue("default_expense_account_code"),
	}

	if req.Code == "" || req.Name == "" {
		http.Redirect(w, r, "/purchases/vendors/new?error=code+and+name+are+required", http.StatusSeeOther)
		return
	}

	_, err = h.svc.CreateVendor(r.Context(), req)
	if err != nil {
		http.Redirect(w, r, "/purchases/vendors/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/purchases/vendors?flash_success=Vendor+"+url.QueryEscape(req.Code)+"+created", http.StatusSeeOther)
}

// purchaseOrdersListPage handles GET /purchases/orders.
func (h *Handler) purchaseOrdersListPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Purchase Orders", "purchase-orders")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	statusFilter := r.URL.Query().Get("status")

	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	result, err := h.svc.ListPurchaseOrders(r.Context(), company.CompanyCode, statusFilter)
	if err != nil {
		d.FlashMsg = "Failed to load purchase orders: " + err.Error()
		d.FlashKind = "error"
		result = &app.PurchaseOrdersResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.POList(d, result, statusFilter).Render(r.Context(), w)
}

// poWizardPage handles GET /purchases/orders/new.
func (h *Handler) poWizardPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "New Purchase Order", "purchase-orders")

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	if fe := r.URL.Query().Get("error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	vendors, err := h.svc.ListVendors(r.Context(), company.CompanyCode)
	if err != nil {
		vendors = &app.VendorsResult{}
	}

	products, err := h.svc.ListProducts(r.Context(), company.CompanyCode)
	if err != nil {
		products = &app.ProductListResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.POWizard(d, vendors, products, company.CompanyCode).Render(r.Context(), w)
}

// poCreateAction handles POST /purchases/orders/new — HTML form submission.
func (h *Handler) poCreateAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/purchases/orders/new?error=invalid+form", http.StatusSeeOther)
		return
	}

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Redirect(w, r, "/purchases/orders?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	poDate := r.FormValue("po_date")
	if poDate == "" {
		poDate = time.Now().Format("2006-01-02")
	}

	req := app.CreatePurchaseOrderRequest{
		CompanyCode: company.CompanyCode,
		VendorCode:  r.FormValue("vendor_code"),
		PODate:      poDate,
		Notes:       r.FormValue("notes"),
	}

	if req.VendorCode == "" {
		http.Redirect(w, r, "/purchases/orders/new?error=vendor+is+required", http.StatusSeeOther)
		return
	}

	// Parse dynamic line items
	for i := 0; ; i++ {
		lineType := r.FormValue(fmt.Sprintf("line_type[%d]", i))
		if lineType == "" {
			break
		}
		qtyStr := r.FormValue(fmt.Sprintf("line_quantity[%d]", i))
		costStr := r.FormValue(fmt.Sprintf("line_unit_cost[%d]", i))

		qty, qErr := decimal.NewFromString(qtyStr)
		cost, _ := decimal.NewFromString(costStr)

		if qErr != nil || qty.IsZero() {
			continue
		}

		line := app.POLineInput{
			Quantity: qty,
			UnitCost: cost,
		}

		if lineType == "goods" {
			line.ProductCode = r.FormValue(fmt.Sprintf("line_product_code[%d]", i))
			line.Description = r.FormValue(fmt.Sprintf("line_description[%d]", i))
			if line.ProductCode == "" {
				continue
			}
		} else {
			// service/expense line
			line.Description = r.FormValue(fmt.Sprintf("line_description[%d]", i))
			line.ExpenseAccountCode = r.FormValue(fmt.Sprintf("line_expense_account[%d]", i))
			if line.Description == "" {
				continue
			}
		}

		req.Lines = append(req.Lines, line)
	}

	if len(req.Lines) == 0 {
		http.Redirect(w, r, "/purchases/orders/new?error=at+least+one+line+required", http.StatusSeeOther)
		return
	}

	result, err := h.svc.CreatePurchaseOrder(r.Context(), req)
	if err != nil {
		http.Redirect(w, r, "/purchases/orders/new?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/purchases/orders/%d", result.PurchaseOrder.ID), http.StatusSeeOther)
}

// poDetailPage handles GET /purchases/orders/{id}.
func (h *Handler) poDetailPage(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid purchase order ID", http.StatusBadRequest)
		return
	}

	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "Failed to load company", http.StatusInternalServerError)
		return
	}

	d := h.buildAppLayoutData(r, "Purchase Order", "purchase-orders")

	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}

	result, err := h.svc.GetPurchaseOrder(r.Context(), company.CompanyCode, poID)
	if err != nil {
		d.FlashMsg = "Purchase order not found: " + err.Error()
		d.FlashKind = "error"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.PODetail(d, nil, company.CompanyCode).Render(r.Context(), w)
		return
	}

	if result.PurchaseOrder != nil && result.PurchaseOrder.PONumber != nil {
		d.Title = "PO " + *result.PurchaseOrder.PONumber
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.PODetail(d, result.PurchaseOrder, company.CompanyCode).Render(r.Context(), w)
}

// ── API handlers ──────────────────────────────────────────────────────────────

// apiListVendors handles GET /api/companies/{code}/vendors.
func (h *Handler) apiListVendors(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	result, err := h.svc.ListVendors(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Vendors)
}

// apiCreateVendor handles POST /api/companies/{code}/vendors.
// Body: { code, name, contact_person?, email?, phone?, address?, payment_terms_days?, ap_account_code?, default_expense_account_code? }
func (h *Handler) apiCreateVendor(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)

	var body struct {
		Code                      string `json:"code"`
		Name                      string `json:"name"`
		ContactPerson             string `json:"contact_person"`
		Email                     string `json:"email"`
		Phone                     string `json:"phone"`
		Address                   string `json:"address"`
		PaymentTermsDays          int    `json:"payment_terms_days"`
		APAccountCode             string `json:"ap_account_code"`
		DefaultExpenseAccountCode string `json:"default_expense_account_code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.Code == "" {
		writeError(w, r, "code is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		writeError(w, r, "name is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	result, err := h.svc.CreateVendor(r.Context(), app.CreateVendorRequest{
		CompanyCode:               code,
		Code:                      body.Code,
		Name:                      body.Name,
		ContactPerson:             body.ContactPerson,
		Email:                     body.Email,
		Phone:                     body.Phone,
		Address:                   body.Address,
		PaymentTermsDays:          body.PaymentTermsDays,
		APAccountCode:             body.APAccountCode,
		DefaultExpenseAccountCode: body.DefaultExpenseAccountCode,
	})
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, result.Vendor)
}

// apiGetVendor handles GET /api/companies/{code}/vendors/{vendorCode}.
func (h *Handler) apiGetVendor(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	vendorCode := chi.URLParam(r, "vendorCode")

	result, err := h.svc.ListVendors(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	for _, v := range result.Vendors {
		if v.Code == vendorCode {
			writeJSON(w, v)
			return
		}
	}
	writeError(w, r, "vendor "+vendorCode+" not found", "NOT_FOUND", http.StatusNotFound)
}

// apiListPurchaseOrders handles GET /api/companies/{code}/purchase-orders.
func (h *Handler) apiListPurchaseOrders(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	statusFilter := r.URL.Query().Get("status")
	result, err := h.svc.ListPurchaseOrders(r.Context(), code, statusFilter)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.Orders)
}

// apiCreatePurchaseOrder handles POST /api/companies/{code}/purchase-orders.
// Body: { vendor_code, po_date?, notes?, lines: [{product_code?, description, quantity, unit_cost, expense_account_code?}] }
func (h *Handler) apiCreatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)

	var body struct {
		VendorCode string `json:"vendor_code"`
		PODate     string `json:"po_date"`
		Notes      string `json:"notes"`
		Lines      []struct {
			ProductCode        string `json:"product_code"`
			Description        string `json:"description"`
			Quantity           string `json:"quantity"`
			UnitCost           string `json:"unit_cost"`
			ExpenseAccountCode string `json:"expense_account_code"`
		} `json:"lines"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.VendorCode == "" {
		writeError(w, r, "vendor_code is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if len(body.Lines) == 0 {
		writeError(w, r, "at least one line is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	req := app.CreatePurchaseOrderRequest{
		CompanyCode: code,
		VendorCode:  body.VendorCode,
		PODate:      body.PODate,
		Notes:       body.Notes,
	}

	for i, l := range body.Lines {
		qty, err := decimal.NewFromString(l.Quantity)
		if err != nil || qty.IsZero() {
			writeError(w, r, fmt.Sprintf("line %d: invalid quantity", i+1), "BAD_REQUEST", http.StatusBadRequest)
			return
		}
		cost, _ := decimal.NewFromString(l.UnitCost)
		req.Lines = append(req.Lines, app.POLineInput{
			ProductCode:        l.ProductCode,
			Description:        l.Description,
			Quantity:           qty,
			UnitCost:           cost,
			ExpenseAccountCode: l.ExpenseAccountCode,
		})
	}

	result, err := h.svc.CreatePurchaseOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, result.PurchaseOrder)
}

// apiGetPurchaseOrder handles GET /api/companies/{code}/purchase-orders/{id}.
func (h *Handler) apiGetPurchaseOrder(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, "invalid purchase order ID", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	result, err := h.svc.GetPurchaseOrder(r.Context(), code, poID)
	if err != nil {
		writeError(w, r, err.Error(), "NOT_FOUND", http.StatusNotFound)
		return
	}
	writeJSON(w, result.PurchaseOrder)
}

// apiApprovePO handles POST /api/companies/{code}/purchase-orders/{id}/approve.
func (h *Handler) apiApprovePO(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, "invalid purchase order ID", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	result, err := h.svc.ApprovePurchaseOrder(r.Context(), code, poID)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.PurchaseOrder)
}

// apiReceivePO handles POST /api/companies/{code}/purchase-orders/{id}/receive.
// Body: { warehouse_code?, lines: [{po_line_id, qty_received}] }
func (h *Handler) apiReceivePO(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, "invalid purchase order ID", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	var body struct {
		WarehouseCode string `json:"warehouse_code"`
		Lines         []struct {
			POLineID    int    `json:"po_line_id"`
			QtyReceived string `json:"qty_received"`
		} `json:"lines"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if len(body.Lines) == 0 {
		writeError(w, r, "at least one line is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	warehouseCode := body.WarehouseCode
	if warehouseCode == "" {
		warehouseCode = "MAIN"
	}

	req := app.ReceivePORequest{
		CompanyCode:   code,
		POID:          poID,
		WarehouseCode: warehouseCode,
	}

	for i, l := range body.Lines {
		qty, err := decimal.NewFromString(l.QtyReceived)
		if err != nil || qty.IsZero() {
			writeError(w, r, fmt.Sprintf("line %d: invalid qty_received", i+1), "BAD_REQUEST", http.StatusBadRequest)
			return
		}
		req.Lines = append(req.Lines, app.ReceivedLineInput{
			POLineID:    l.POLineID,
			QtyReceived: qty,
		})
	}

	result, err := h.svc.ReceivePurchaseOrder(r.Context(), req)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.PurchaseOrder)
}

// apiInvoicePO handles POST /api/companies/{code}/purchase-orders/{id}/invoice.
// Body: { invoice_number, invoice_date, invoice_amount }
func (h *Handler) apiInvoicePO(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, "invalid purchase order ID", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	var body struct {
		InvoiceNumber string `json:"invoice_number"`
		InvoiceDate   string `json:"invoice_date"`
		InvoiceAmount string `json:"invoice_amount"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	if body.InvoiceNumber == "" {
		writeError(w, r, "invoice_number is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	invoiceDate, err := time.Parse("2006-01-02", body.InvoiceDate)
	if err != nil {
		writeError(w, r, "invalid invoice_date (expected YYYY-MM-DD)", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	invoiceAmount, err := decimal.NewFromString(body.InvoiceAmount)
	if err != nil || invoiceAmount.IsZero() {
		writeError(w, r, "invalid invoice_amount", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	result, err := h.svc.RecordVendorInvoice(r.Context(), app.VendorInvoiceRequest{
		CompanyCode:   code,
		POID:          poID,
		InvoiceNumber: body.InvoiceNumber,
		InvoiceDate:   invoiceDate,
		InvoiceAmount: invoiceAmount,
	})
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	type response struct {
		PurchaseOrder any    `json:"purchase_order"`
		Warning       string `json:"warning,omitempty"`
	}
	writeJSON(w, response{PurchaseOrder: result.PurchaseOrder, Warning: result.Warning})
}

// apiPayPO handles POST /api/companies/{code}/purchase-orders/{id}/pay.
// Body: { bank_account_code?, payment_date? }
func (h *Handler) apiPayPO(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	idStr := chi.URLParam(r, "id")
	poID, err := strconv.Atoi(idStr)
	if err != nil {
		writeError(w, r, "invalid purchase order ID", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	var body struct {
		BankAccountCode string `json:"bank_account_code"`
		PaymentDate     string `json:"payment_date"`
	}
	// Best-effort decode; both fields have defaults.
	_ = json.NewDecoder(r.Body).Decode(&body)

	bankCode := body.BankAccountCode
	if bankCode == "" {
		bankCode = "1000"
	}

	paymentDate := time.Now()
	if body.PaymentDate != "" {
		if pd, err := time.Parse("2006-01-02", body.PaymentDate); err == nil {
			paymentDate = pd
		}
	}

	result, err := h.svc.PayVendor(r.Context(), app.PayVendorRequest{
		CompanyCode:     code,
		POID:            poID,
		BankAccountCode: bankCode,
		PaymentDate:     paymentDate,
	})
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result.PurchaseOrder)
}

