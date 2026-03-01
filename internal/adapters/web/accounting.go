package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"accounting-agent/internal/app"
	"accounting-agent/internal/core"
	"accounting-agent/web/templates/pages"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ── Browser page handlers ─────────────────────────────────────────────────────

// trialBalancePage handles GET /reports/trial-balance.
func (h *Handler) trialBalancePage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Trial Balance", "trial-balance")

	if d.CompanyCode == "" {
		http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
		return
	}

	result, err := h.svc.GetTrialBalance(r.Context(), d.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load trial balance: " + err.Error()
		d.FlashKind = "error"
		result = &app.TrialBalanceResult{CompanyCode: d.CompanyCode}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.TrialBalance(d, result).Render(r.Context(), w)
}

// plReportPage handles GET /reports/pl.
func (h *Handler) plReportPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "P&L Report", "pl")

	if d.CompanyCode == "" {
		http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
		return
	}

	now := time.Now()
	year := now.Year()
	month := int(now.Month())

	if y := r.URL.Query().Get("year"); y != "" {
		if parsed, err := strconv.Atoi(y); err == nil {
			year = parsed
		}
	}
	if m := r.URL.Query().Get("month"); m != "" {
		if parsed, err := strconv.Atoi(m); err == nil {
			month = parsed
		}
	}

	report, err := h.svc.GetProfitAndLoss(r.Context(), d.CompanyCode, year, month)
	if err != nil {
		d.FlashMsg = "Failed to load P&L: " + err.Error()
		d.FlashKind = "error"
		report = &core.PLReport{CompanyCode: d.CompanyCode, Year: year, Month: month}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.PLReport(d, report, year, month).Render(r.Context(), w)
}

// balanceSheetPage handles GET /reports/balance-sheet.
func (h *Handler) balanceSheetPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Balance Sheet", "balance-sheet")

	if d.CompanyCode == "" {
		http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
		return
	}

	asOfDate := r.URL.Query().Get("date")
	if asOfDate == "" {
		asOfDate = time.Now().Format("2006-01-02")
	}

	report, err := h.svc.GetBalanceSheet(r.Context(), d.CompanyCode, asOfDate)
	if err != nil {
		d.FlashMsg = "Failed to load balance sheet: " + err.Error()
		d.FlashKind = "error"
		report = &core.BSReport{CompanyCode: d.CompanyCode, AsOfDate: asOfDate}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.BalanceSheet(d, report, asOfDate).Render(r.Context(), w)
}

// accountStatementPage handles GET /reports/statement.
// When format=csv, streams CSV instead of HTML.
func (h *Handler) accountStatementPage(w http.ResponseWriter, r *http.Request) {
	accountCode := r.URL.Query().Get("account")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	format := r.URL.Query().Get("format")

	d := h.buildAppLayoutData(r, "Account Statement", "statement")

	if d.CompanyCode == "" {
		http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
		return
	}

	if accountCode == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.AccountStatement(d, nil, "", from, to).Render(r.Context(), w)
		return
	}

	stmtResult, err := h.svc.GetAccountStatement(r.Context(), d.CompanyCode, accountCode, from, to)
	if err != nil {
		d.FlashMsg = "Failed to load statement: " + err.Error()
		d.FlashKind = "error"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.AccountStatement(d, nil, accountCode, from, to).Render(r.Context(), w)
		return
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="statement-`+accountCode+`.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Date", "Narration", "Reference", "Debit", "Credit", "Balance"})
		for _, line := range stmtResult.Lines {
			_ = cw.Write([]string{
				line.PostingDate,
				csvSafe(line.Narration),
				csvSafe(line.Reference),
				line.Debit.StringFixed(2),
				line.Credit.StringFixed(2),
				line.RunningBalance.StringFixed(2),
			})
		}
		cw.Flush()
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.AccountStatement(d, stmtResult, accountCode, from, to).Render(r.Context(), w)
}

// journalEntryPage handles GET /accounting/journal-entry.
func (h *Handler) journalEntryPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Journal Entry", "")

	if d.CompanyCode == "" {
		http.Error(w, "Company not resolved — please log in again", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.JournalEntry(d, d.CompanyCode).Render(r.Context(), w)
}

// csvSafe prevents CSV formula injection by prefixing cells that begin with a
// formula-triggering character with a single quote.
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

// ── API handlers ──────────────────────────────────────────────────────────────

// apiTrialBalance handles GET /api/companies/{code}/trial-balance.
func (h *Handler) apiTrialBalance(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}
	result, err := h.svc.GetTrialBalance(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// apiAccountStatement handles GET /api/companies/{code}/accounts/{accountCode}/statement.
func (h *Handler) apiAccountStatement(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}
	result, err := h.svc.GetAccountStatement(r.Context(),
		code,
		chi.URLParam(r, "accountCode"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
	)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// apiProfitAndLoss handles GET /api/companies/{code}/reports/pl.
func (h *Handler) apiProfitAndLoss(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}
	now := time.Now()
	year, month := now.Year(), int(now.Month())

	if y := r.URL.Query().Get("year"); y != "" {
		if parsed, err := strconv.Atoi(y); err == nil {
			year = parsed
		}
	}
	if m := r.URL.Query().Get("month"); m != "" {
		if parsed, err := strconv.Atoi(m); err == nil {
			month = parsed
		}
	}

	result, err := h.svc.GetProfitAndLoss(r.Context(), code, year, month)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// apiBalanceSheet handles GET /api/companies/{code}/reports/balance-sheet.
func (h *Handler) apiBalanceSheet(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}
	result, err := h.svc.GetBalanceSheet(r.Context(), code, r.URL.Query().Get("date"))
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// apiRefreshViews handles POST /api/companies/{code}/reports/refresh.
func (h *Handler) apiRefreshViews(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}
	if err := h.svc.RefreshViews(r.Context()); err != nil {
		writeError(w, r, err.Error(), "INTERNAL", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "refreshed"})
}

// ── Journal Entry API ─────────────────────────────────────────────────────────

// journalEntryRequest is the JSON body for manual journal entry creation/validation.
type journalEntryRequest struct {
	Narration    string `json:"narration"`
	PostingDate  string `json:"posting_date"`
	DocumentDate string `json:"document_date"`
	Currency     string `json:"currency"`
	ExchangeRate string `json:"exchange_rate"`
	Lines        []struct {
		AccountCode string `json:"account_code"`
		Debit       string `json:"debit"`
		Credit      string `json:"credit"`
	} `json:"lines"`
}

// apiPostJournalEntry handles POST /api/companies/{code}/journal-entries.
func (h *Handler) apiPostJournalEntry(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}

	var req journalEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	proposal, err := buildProposal(code, req)
	if err != nil {
		writeError(w, r, err.Error(), "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	if err := h.svc.CommitProposal(r.Context(), proposal); err != nil {
		writeError(w, r, err.Error(), "COMMIT_FAILED", http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, map[string]string{"status": "posted", "idempotency_key": proposal.IdempotencyKey})
}

// apiValidateJournalEntry handles POST /api/companies/{code}/journal-entries/validate.
func (h *Handler) apiValidateJournalEntry(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}

	var req journalEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	proposal, err := buildProposal(code, req)
	if err != nil {
		writeError(w, r, err.Error(), "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	if err := h.svc.ValidateProposal(r.Context(), proposal); err != nil {
		writeError(w, r, err.Error(), "VALIDATION_FAILED", http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, map[string]string{"status": "valid"})
}

// buildProposal converts a journalEntryRequest into a core.Proposal.
func buildProposal(code string, req journalEntryRequest) (core.Proposal, error) {
	if req.Narration == "" {
		return core.Proposal{}, fmt.Errorf("narration is required")
	}
	if req.PostingDate == "" {
		return core.Proposal{}, fmt.Errorf("posting_date is required")
	}

	currency := req.Currency
	if currency == "" {
		currency = "INR"
	}
	exchangeRate := req.ExchangeRate
	if exchangeRate == "" {
		exchangeRate = "1.0"
	}
	docDate := req.DocumentDate
	if docDate == "" {
		docDate = req.PostingDate
	}

	var lines []core.ProposalLine
	for _, l := range req.Lines {
		if l.AccountCode == "" {
			continue
		}

		debitAmt := decimal.Zero
		creditAmt := decimal.Zero

		if l.Debit != "" && l.Debit != "0" {
			d, err := decimal.NewFromString(l.Debit)
			if err != nil {
				return core.Proposal{}, fmt.Errorf("invalid debit amount %q: %w", l.Debit, err)
			}
			debitAmt = d
		}
		if l.Credit != "" && l.Credit != "0" {
			c, err := decimal.NewFromString(l.Credit)
			if err != nil {
				return core.Proposal{}, fmt.Errorf("invalid credit amount %q: %w", l.Credit, err)
			}
			creditAmt = c
		}

		if debitAmt.IsPositive() {
			lines = append(lines, core.ProposalLine{
				AccountCode: l.AccountCode,
				IsDebit:     true,
				Amount:      debitAmt.StringFixed(2),
			})
		}
		if creditAmt.IsPositive() {
			lines = append(lines, core.ProposalLine{
				AccountCode: l.AccountCode,
				IsDebit:     false,
				Amount:      creditAmt.StringFixed(2),
			})
		}
	}

	if len(lines) < 2 {
		return core.Proposal{}, fmt.Errorf("at least two non-zero journal lines are required")
	}

	return core.Proposal{
		DocumentTypeCode:    "JE",
		CompanyCode:         code,
		IdempotencyKey:      uuid.New().String(),
		TransactionCurrency: currency,
		ExchangeRate:        exchangeRate,
		Summary:             req.Narration,
		PostingDate:         req.PostingDate,
		DocumentDate:        docDate,
		Confidence:          1.0,
		Reasoning:           "Manual journal entry submitted via web UI",
		Lines:               lines,
	}, nil
}
