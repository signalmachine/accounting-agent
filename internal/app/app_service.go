package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"accounting-agent/internal/ai"
	"accounting-agent/internal/core"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"golang.org/x/crypto/bcrypt"
)

type appService struct {
	pool                 *pgxpool.Pool
	ledger               *core.Ledger
	docService           core.DocumentService
	orderService         core.OrderService
	inventoryService     core.InventoryService
	reportingService     core.ReportingService
	userService          core.UserService
	vendorService        core.VendorService
	purchaseOrderService core.PurchaseOrderService
	agent                *ai.Agent
}

// NewAppService constructs an appService that satisfies ApplicationService.
func NewAppService(
	pool *pgxpool.Pool,
	ledger *core.Ledger,
	docService core.DocumentService,
	orderService core.OrderService,
	inventoryService core.InventoryService,
	reportingService core.ReportingService,
	userService core.UserService,
	vendorService core.VendorService,
	purchaseOrderService core.PurchaseOrderService,
	agent *ai.Agent,
) ApplicationService {
	return &appService{
		pool:                 pool,
		ledger:               ledger,
		docService:           docService,
		orderService:         orderService,
		inventoryService:     inventoryService,
		reportingService:     reportingService,
		userService:          userService,
		vendorService:        vendorService,
		purchaseOrderService: purchaseOrderService,
		agent:                agent,
	}
}

// GetTrialBalance returns the trial balance for the given company.
// Reads from mv_trial_balance (materialized view) for performance.
// Call RefreshViews to include the latest postings.
func (s *appService) GetTrialBalance(ctx context.Context, companyCode string) (*TrialBalanceResult, error) {
	var companyID int
	var companyName, currency string
	if err := s.pool.QueryRow(ctx,
		"SELECT id, name, base_currency FROM companies WHERE company_code = $1", companyCode,
	).Scan(&companyID, &companyName, &currency); err != nil {
		return nil, fmt.Errorf("company %s not found: %w", companyCode, err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT account_code, account_name, net_balance
		FROM mv_trial_balance
		WHERE company_id = $1
		ORDER BY account_code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query trial balance view: %w", err)
	}
	defer rows.Close()

	var accounts []core.AccountBalance
	for rows.Next() {
		var b core.AccountBalance
		if err := rows.Scan(&b.Code, &b.Name, &b.Balance); err != nil {
			return nil, fmt.Errorf("failed to scan trial balance: %w", err)
		}
		accounts = append(accounts, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trial balance row iteration: %w", err)
	}

	return &TrialBalanceResult{
		CompanyCode: companyCode,
		CompanyName: companyName,
		Currency:    currency,
		Accounts:    accounts,
	}, nil
}

// ListCustomers returns all active customers for a company.
func (s *appService) ListCustomers(ctx context.Context, companyCode string) (*CustomerListResult, error) {
	customers, err := s.orderService.GetCustomers(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	return &CustomerListResult{Customers: customers}, nil
}

// ListProducts returns all active products for a company.
func (s *appService) ListProducts(ctx context.Context, companyCode string) (*ProductListResult, error) {
	products, err := s.orderService.GetProducts(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	return &ProductListResult{Products: products}, nil
}

// ListOrders returns sales orders for a company, optionally filtered by status.
func (s *appService) ListOrders(ctx context.Context, companyCode string, status *string) (*OrderListResult, error) {
	orders, err := s.orderService.GetOrders(ctx, companyCode, status)
	if err != nil {
		return nil, err
	}
	return &OrderListResult{Orders: orders, CompanyCode: companyCode}, nil
}

// GetOrder returns a single sales order by numeric ID or order number string.
func (s *appService) GetOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error) {
	order, err := s.resolveOrder(ctx, ref, companyCode)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// CreateOrder creates a new DRAFT sales order.
func (s *appService) CreateOrder(ctx context.Context, req CreateOrderRequest) (*OrderResult, error) {
	lines := make([]core.OrderLineInput, len(req.Lines))
	for i, l := range req.Lines {
		lines[i] = core.OrderLineInput{
			ProductCode: l.ProductCode,
			Quantity:    l.Quantity,
			UnitPrice:   l.UnitPrice,
		}
	}

	exchangeRate := req.ExchangeRate
	if exchangeRate.IsZero() {
		exchangeRate = decimal.NewFromFloat(1.0)
	}

	orderDate := req.OrderDate
	if orderDate == "" {
		orderDate = time.Now().Format("2006-01-02")
	}

	order, err := s.orderService.CreateOrder(ctx, req.CompanyCode, req.CustomerCode, req.Currency,
		exchangeRate, orderDate, lines, req.Notes)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// ConfirmOrder transitions a DRAFT order to CONFIRMED, assigning an order number and reserving stock.
func (s *appService) ConfirmOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error) {
	order, err := s.resolveOrder(ctx, ref, companyCode)
	if err != nil {
		return nil, err
	}
	order, err = s.orderService.ConfirmOrder(ctx, order.ID, s.docService, s.inventoryService)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// ShipOrder transitions a CONFIRMED order to SHIPPED, deducting inventory and booking COGS.
func (s *appService) ShipOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error) {
	order, err := s.resolveOrder(ctx, ref, companyCode)
	if err != nil {
		return nil, err
	}
	order, err = s.orderService.ShipOrder(ctx, order.ID, s.inventoryService, s.ledger, s.docService)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// InvoiceOrder transitions a SHIPPED order to INVOICED, posting the sales invoice journal entry.
func (s *appService) InvoiceOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error) {
	order, err := s.resolveOrder(ctx, ref, companyCode)
	if err != nil {
		return nil, err
	}
	order, err = s.orderService.InvoiceOrder(ctx, order.ID, s.ledger, s.docService)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// RecordPayment transitions an INVOICED order to PAID, posting the cash receipt journal entry.
func (s *appService) RecordPayment(ctx context.Context, ref, bankCode, companyCode string) (*OrderResult, error) {
	order, err := s.resolveOrder(ctx, ref, companyCode)
	if err != nil {
		return nil, err
	}
	if err := s.orderService.RecordPayment(ctx, order.ID, bankCode, "", s.ledger); err != nil {
		return nil, err
	}
	// Re-fetch to return the updated order with PAID status.
	order, err = s.orderService.GetOrder(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	return &OrderResult{Order: order}, nil
}

// ListWarehouses returns all active warehouses for a company.
func (s *appService) ListWarehouses(ctx context.Context, companyCode string) (*WarehouseListResult, error) {
	warehouses, err := s.inventoryService.GetWarehouses(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	return &WarehouseListResult{Warehouses: warehouses}, nil
}

// GetStockLevels returns current stock levels for all inventory items in a company.
func (s *appService) GetStockLevels(ctx context.Context, companyCode string) (*StockResult, error) {
	levels, err := s.inventoryService.GetStockLevels(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	return &StockResult{Levels: levels, CompanyCode: companyCode}, nil
}

// ReceiveStock records a goods receipt: increases qty_on_hand and books DR Inventory / CR creditAccount.
func (s *appService) ReceiveStock(ctx context.Context, req ReceiveStockRequest) error {
	warehouseCode := req.WarehouseCode
	if warehouseCode == "" {
		wh, err := s.inventoryService.GetDefaultWarehouse(ctx, req.CompanyCode)
		if err != nil {
			return fmt.Errorf("no active warehouse found: %w", err)
		}
		warehouseCode = wh.Code
	}

	creditAccount := req.CreditAccountCode
	if creditAccount == "" {
		creditAccount = "2000"
	}

	movementDate := req.MovementDate
	if movementDate == "" {
		movementDate = time.Now().Format("2006-01-02")
	}

	return s.inventoryService.ReceiveStock(ctx, req.CompanyCode, warehouseCode, req.ProductCode,
		req.Qty, req.UnitCost, movementDate, creditAccount, nil, s.ledger, s.docService)
}

// GetAccountStatement returns a chronological account statement with running balance.
func (s *appService) GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) (*AccountStatementResult, error) {
	var currency string
	if err := s.pool.QueryRow(ctx,
		"SELECT base_currency FROM companies WHERE company_code = $1", companyCode,
	).Scan(&currency); err != nil {
		return nil, fmt.Errorf("company %s not found: %w", companyCode, err)
	}

	lines, err := s.reportingService.GetAccountStatement(ctx, companyCode, accountCode, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	return &AccountStatementResult{
		CompanyCode: companyCode,
		AccountCode: accountCode,
		Currency:    currency,
		Lines:       lines,
	}, nil
}

// GetProfitAndLoss returns the P&L report for the given year and month.
func (s *appService) GetProfitAndLoss(ctx context.Context, companyCode string, year, month int) (*core.PLReport, error) {
	return s.reportingService.GetProfitAndLoss(ctx, companyCode, year, month)
}

// GetBalanceSheet returns the Balance Sheet as of the given date.
func (s *appService) GetBalanceSheet(ctx context.Context, companyCode, asOfDate string) (*core.BSReport, error) {
	return s.reportingService.GetBalanceSheet(ctx, companyCode, asOfDate)
}

// RefreshViews refreshes all materialized reporting views.
func (s *appService) RefreshViews(ctx context.Context) error {
	return s.reportingService.RefreshViews(ctx)
}

// InterpretEvent sends a natural language event description to the AI agent and returns
// either a Proposal or a clarification request.
func (s *appService) InterpretEvent(ctx context.Context, text, companyCode string) (*AIResult, error) {
	coa, err := s.fetchCoA(ctx, companyCode)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chart of accounts: %w", err)
	}

	documentTypes, err := s.fetchDocumentTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch document types: %w", err)
	}

	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch company: %w", err)
	}

	response, err := s.agent.InterpretEvent(ctx, text, coa, documentTypes, company)
	if err != nil {
		return nil, err
	}

	if response.IsClarificationRequest {
		return &AIResult{
			IsClarification:      true,
			ClarificationMessage: response.Clarification.Message,
		}, nil
	}

	return &AIResult{
		IsClarification: false,
		Proposal:        response.Proposal,
	}, nil
}

// InterpretDomainAction routes a natural language input through the agentic tool loop.
// It builds a ToolRegistry with read tools for the current domain, delegates to the agent,
// and translates the AgentDomainResult to a DomainActionResult for the adapter layer.
// attachments is variadic — REPL/CLI callers omit it.
func (s *appService) InterpretDomainAction(ctx context.Context, text, companyCode string, attachments ...Attachment) (*DomainActionResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch company: %w", err)
	}

	registry := s.buildToolRegistry(ctx, companyCode)

	// Convert app.Attachment → ai.Attachment for the agent layer.
	aiAtts := make([]ai.Attachment, len(attachments))
	for i, a := range attachments {
		aiAtts[i] = ai.Attachment{MimeType: a.MimeType, Data: a.Data}
	}

	result, err := s.agent.InterpretDomainAction(ctx, text, company, registry, aiAtts)
	if err != nil {
		return nil, err
	}

	return &DomainActionResult{
		Kind:             DomainActionKind(result.Kind),
		Answer:           result.Answer,
		Question:         result.Question,
		Context:          result.Context,
		ToolName:         result.ToolName,
		ToolArgs:         result.ToolArgs,
		EventDescription: result.EventDescription,
	}, nil
}

// ExecuteWriteTool executes a previously proposed write tool action after human confirmation.
// Args are parsed from the map stored at proposal time; returned string is a JSON-encoded summary.
func (s *appService) ExecuteWriteTool(ctx context.Context, companyCode, toolName string, args map[string]any) (string, error) {
	// Helper to safely extract a float64 → int (JSON numbers arrive as float64).
	intArg := func(key string) int {
		v, _ := args[key].(float64)
		return int(v)
	}
	strArg := func(key string) string {
		v, _ := args[key].(string)
		return v
	}

	switch toolName {

	case "approve_po":
		result, err := s.ApprovePurchaseOrder(ctx, companyCode, intArg("po_id"))
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{
			"message":   "Purchase order approved.",
			"po_number": result.PurchaseOrder.PONumber,
			"status":    result.PurchaseOrder.Status,
		})
		return string(b), nil

	case "create_vendor":
		req := CreateVendorRequest{
			CompanyCode:               companyCode,
			Code:                      strArg("code"),
			Name:                      strArg("name"),
			ContactPerson:             strArg("contact_person"),
			Email:                     strArg("email"),
			Phone:                     strArg("phone"),
			Address:                   strArg("address"),
			APAccountCode:             strArg("ap_account_code"),
			DefaultExpenseAccountCode: strArg("default_expense_account_code"),
		}
		if pt, ok := args["payment_terms_days"].(float64); ok {
			req.PaymentTermsDays = int(pt)
		}
		result, err := s.CreateVendor(ctx, req)
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{
			"message": "Vendor created.",
			"code":    result.Vendor.Code,
			"name":    result.Vendor.Name,
		})
		return string(b), nil

	case "create_purchase_order":
		// Parse nested lines via JSON round-trip.
		type lineIn struct {
			ProductCode        string  `json:"product_code"`
			Description        string  `json:"description"`
			Quantity           float64 `json:"quantity"`
			UnitCost           float64 `json:"unit_cost"`
			ExpenseAccountCode string  `json:"expense_account_code"`
		}
		type poIn struct {
			VendorCode string  `json:"vendor_code"`
			PODate     string  `json:"po_date"`
			Notes      string  `json:"notes"`
			Lines      []lineIn `json:"lines"`
		}
		raw, _ := json.Marshal(args)
		var inp poIn
		if err := json.Unmarshal(raw, &inp); err != nil {
			return "", fmt.Errorf("invalid create_purchase_order args: %w", err)
		}
		lines := make([]POLineInput, len(inp.Lines))
		for i, l := range inp.Lines {
			lines[i] = POLineInput{
				ProductCode:        l.ProductCode,
				Description:        l.Description,
				Quantity:           decimal.NewFromFloat(l.Quantity),
				UnitCost:           decimal.NewFromFloat(l.UnitCost),
				ExpenseAccountCode: l.ExpenseAccountCode,
			}
		}
		result, err := s.CreatePurchaseOrder(ctx, CreatePurchaseOrderRequest{
			CompanyCode: companyCode,
			VendorCode:  inp.VendorCode,
			PODate:      inp.PODate,
			Notes:       inp.Notes,
			Lines:       lines,
		})
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{
			"message": "Purchase order created as DRAFT.",
			"po_id":   result.PurchaseOrder.ID,
			"status":  result.PurchaseOrder.Status,
		})
		return string(b), nil

	case "receive_po":
		type lineIn struct {
			POLineID    int     `json:"po_line_id"`
			QtyReceived float64 `json:"qty_received"`
		}
		type receiveIn struct {
			POID          int      `json:"po_id"`
			WarehouseCode string   `json:"warehouse_code"`
			Lines         []lineIn `json:"lines"`
		}
		raw, _ := json.Marshal(args)
		var inp receiveIn
		if err := json.Unmarshal(raw, &inp); err != nil {
			return "", fmt.Errorf("invalid receive_po args: %w", err)
		}
		lines := make([]ReceivedLineInput, len(inp.Lines))
		for i, l := range inp.Lines {
			lines[i] = ReceivedLineInput{
				POLineID:    l.POLineID,
				QtyReceived: decimal.NewFromFloat(l.QtyReceived),
			}
		}
		result, err := s.ReceivePurchaseOrder(ctx, ReceivePORequest{
			CompanyCode:   companyCode,
			POID:          inp.POID,
			WarehouseCode: inp.WarehouseCode,
			Lines:         lines,
		})
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{
			"message":        "Goods received against PO.",
			"lines_received": result.LinesReceived,
			"status":         result.PurchaseOrder.Status,
		})
		return string(b), nil

	case "record_vendor_invoice":
		amtStr := strArg("invoice_amount")
		amt, err := decimal.NewFromString(amtStr)
		if err != nil {
			// try float fallback
			if f, ok := args["invoice_amount"].(float64); ok {
				amt = decimal.NewFromFloat(f)
			} else {
				return "", fmt.Errorf("invalid invoice_amount: %q", amtStr)
			}
		}
		invoiceDate, err := time.Parse("2006-01-02", strArg("invoice_date"))
		if err != nil {
			return "", fmt.Errorf("invalid invoice_date: %w", err)
		}
		result, err := s.RecordVendorInvoice(ctx, VendorInvoiceRequest{
			CompanyCode:   companyCode,
			POID:          intArg("po_id"),
			InvoiceNumber: strArg("invoice_number"),
			InvoiceDate:   invoiceDate,
			InvoiceAmount: amt,
		})
		if err != nil {
			return "", err
		}
		msg := "Vendor invoice recorded. PI document: " + result.PIDocumentNumber
		if result.Warning != "" {
			msg += " Warning: " + result.Warning
		}
		b, _ := json.Marshal(map[string]any{"message": msg, "status": result.PurchaseOrder.Status})
		return string(b), nil

	case "pay_vendor":
		paymentDate, err := time.Parse("2006-01-02", strArg("payment_date"))
		if err != nil {
			return "", fmt.Errorf("invalid payment_date: %w", err)
		}
		result, err := s.PayVendor(ctx, PayVendorRequest{
			CompanyCode:     companyCode,
			POID:            intArg("po_id"),
			BankAccountCode: strArg("bank_account_code"),
			PaymentDate:     paymentDate,
		})
		if err != nil {
			return "", err
		}
		b, _ := json.Marshal(map[string]any{
			"message": "Vendor payment posted.",
			"status":  result.PurchaseOrder.Status,
		})
		return string(b), nil

	default:
		return "", fmt.Errorf("unknown write tool: %q", toolName)
	}
}

// ListVendors returns all active vendors for a company.
func (s *appService) ListVendors(ctx context.Context, companyCode string) (*VendorsResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	vendors, err := s.vendorService.GetVendors(ctx, company.ID)
	if err != nil {
		return nil, err
	}
	return &VendorsResult{Vendors: vendors}, nil
}

// CreateVendor creates a new vendor record for the given company.
func (s *appService) CreateVendor(ctx context.Context, req CreateVendorRequest) (*VendorResult, error) {
	company, err := s.fetchCompany(ctx, req.CompanyCode)
	if err != nil {
		return nil, err
	}
	vendor, err := s.vendorService.CreateVendor(ctx, company.ID, core.VendorInput{
		Code:                      req.Code,
		Name:                      req.Name,
		ContactPerson:             req.ContactPerson,
		Email:                     req.Email,
		Phone:                     req.Phone,
		Address:                   req.Address,
		PaymentTermsDays:          req.PaymentTermsDays,
		APAccountCode:             req.APAccountCode,
		DefaultExpenseAccountCode: req.DefaultExpenseAccountCode,
	})
	if err != nil {
		return nil, err
	}
	return &VendorResult{Vendor: vendor}, nil
}

// GetPurchaseOrder returns a single purchase order by its internal ID, validating company ownership.
func (s *appService) GetPurchaseOrder(ctx context.Context, companyCode string, poID int) (*PurchaseOrderResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	po, err := s.purchaseOrderService.GetPO(ctx, poID)
	if err != nil {
		return nil, err
	}
	if po.CompanyID != company.ID {
		return nil, fmt.Errorf("purchase order %d not found", poID)
	}
	return &PurchaseOrderResult{PurchaseOrder: po}, nil
}

// ListPurchaseOrders returns purchase orders for a company, optionally filtered by status.
func (s *appService) ListPurchaseOrders(ctx context.Context, companyCode, status string) (*PurchaseOrdersResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	orders, err := s.purchaseOrderService.GetPOs(ctx, company.ID, status)
	if err != nil {
		return nil, err
	}
	return &PurchaseOrdersResult{Orders: orders}, nil
}

// CreatePurchaseOrder creates a new DRAFT purchase order.
func (s *appService) CreatePurchaseOrder(ctx context.Context, req CreatePurchaseOrderRequest) (*PurchaseOrderResult, error) {
	company, err := s.fetchCompany(ctx, req.CompanyCode)
	if err != nil {
		return nil, err
	}
	vendor, err := s.vendorService.GetVendorByCode(ctx, company.ID, req.VendorCode)
	if err != nil {
		return nil, fmt.Errorf("vendor %q: %w", req.VendorCode, err)
	}

	poDate, err := time.Parse("2006-01-02", req.PODate)
	if err != nil {
		return nil, fmt.Errorf("invalid po_date %q: %w", req.PODate, err)
	}

	var lines []core.PurchaseOrderLineInput
	for _, l := range req.Lines {
		lines = append(lines, core.PurchaseOrderLineInput{
			ProductCode:        l.ProductCode,
			Description:        l.Description,
			Quantity:           l.Quantity,
			UnitCost:           l.UnitCost,
			ExpenseAccountCode: l.ExpenseAccountCode,
		})
	}

	po, err := s.purchaseOrderService.CreatePO(ctx, company.ID, vendor.ID, poDate, lines, req.Notes)
	if err != nil {
		return nil, err
	}
	return &PurchaseOrderResult{PurchaseOrder: po}, nil
}

// ApprovePurchaseOrder transitions a DRAFT PO to APPROVED, assigning a gapless PO number.
func (s *appService) ApprovePurchaseOrder(ctx context.Context, companyCode string, poID int) (*PurchaseOrderResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, err
	}
	if err := s.purchaseOrderService.ApprovePO(ctx, company.ID, poID, s.docService); err != nil {
		return nil, err
	}
	po, err := s.purchaseOrderService.GetPO(ctx, poID)
	if err != nil {
		return nil, err
	}
	return &PurchaseOrderResult{PurchaseOrder: po}, nil
}

// buildToolRegistry constructs the ToolRegistry for Phase 7.5 with 5 read tools:
// search_accounts, search_customers, search_products, get_stock_levels, get_warehouses.
// Tool handlers are closures that capture the pool and companyCode.
func (s *appService) buildToolRegistry(ctx context.Context, companyCode string) *ai.ToolRegistry {
	registry := ai.NewToolRegistry()

	registry.Register(ai.ToolDefinition{
		Name:        "search_accounts",
		Description: "Search the chart of accounts by name or code. Returns top matching accounts with code, name, type, and current balance.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search text to match against account name or code (e.g. 'accounts receivable', '1200', 'sales revenue').",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			query, _ := params["query"].(string)
			return s.searchAccounts(hctx, companyCode, query)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "search_customers",
		Description: "Search the customer master by name or code. Returns matching customers with code, name, and contact information.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search text to match against customer name or code (e.g. 'Acme', 'C001').",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			query, _ := params["query"].(string)
			return s.searchCustomers(hctx, companyCode, query)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "search_products",
		Description: "Search the product catalogue by name or code. Returns matching products with code, name, and unit price.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search text to match against product name or code (e.g. 'Widget', 'P001').",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			query, _ := params["query"].(string)
			return s.searchProducts(hctx, companyCode, query)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_stock_levels",
		Description: "Get current inventory stock levels. Optionally filter by product code or warehouse code.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"product_code": map[string]any{
					"type":        "string",
					"description": "Filter to a specific product code (optional).",
				},
				"warehouse_code": map[string]any{
					"type":        "string",
					"description": "Filter to a specific warehouse code (optional).",
				},
			},
			"required": []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			productCode, _ := params["product_code"].(string)
			warehouseCode, _ := params["warehouse_code"].(string)
			return s.getStockLevels(hctx, companyCode, productCode, warehouseCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_warehouses",
		Description: "Get all active warehouses for the company.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
			"required":             []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			return s.getWarehousesJSON(hctx, companyCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_account_balance",
		Description: "Get the current balance for a specific account code. Returns the net debit position (positive = net debit, negative = net credit).",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"account_code": map[string]any{
					"type":        "string",
					"description": "The account code to query (e.g. '1200', '4000').",
				},
			},
			"required": []string{"account_code"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			accountCode, _ := params["account_code"].(string)
			return s.getAccountBalanceJSON(hctx, companyCode, accountCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_account_statement",
		Description: "Get a chronological account statement showing all movements and running balance for a given account. from_date and to_date are optional.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"account_code": map[string]any{
					"type":        "string",
					"description": "The account code to query (e.g. '1200').",
				},
				"from_date": map[string]any{
					"type":        "string",
					"description": "Start date in YYYY-MM-DD format (optional).",
				},
				"to_date": map[string]any{
					"type":        "string",
					"description": "End date in YYYY-MM-DD format (optional).",
				},
			},
			"required": []string{"account_code"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			accountCode, _ := params["account_code"].(string)
			fromDate, _ := params["from_date"].(string)
			toDate, _ := params["to_date"].(string)
			return s.getAccountStatementJSON(hctx, companyCode, accountCode, fromDate, toDate)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_pl_report",
		Description: "Get the Profit & Loss report for a given year and month. Returns revenue accounts, expense accounts, and net income.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"year": map[string]any{
					"type":        "integer",
					"description": "The calendar year (e.g. 2026).",
				},
				"month": map[string]any{
					"type":        "integer",
					"description": "The calendar month as an integer 1–12.",
				},
			},
			"required": []string{"year", "month"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			year := int(params["year"].(float64))
			month := int(params["month"].(float64))
			return s.getPLReportJSON(hctx, companyCode, year, month)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_balance_sheet",
		Description: "Get the Balance Sheet as of a given date. Returns assets, liabilities, equity, totals, and whether the sheet is balanced. If as_of_date is omitted, today's date is used.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"as_of_date": map[string]any{
					"type":        "string",
					"description": "Date in YYYY-MM-DD format (optional; defaults to today).",
				},
			},
			"required": []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			asOfDate, _ := params["as_of_date"].(string)
			return s.getBalanceSheetJSON(hctx, companyCode, asOfDate)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "refresh_views",
		Description: "Refresh the materialized reporting views (mv_account_period_balances and mv_trial_balance). Call before generating reports if data may be stale.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
			"required":             []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			if err := s.reportingService.RefreshViews(hctx); err != nil {
				return "", err
			}
			return `{"status":"ok","message":"Materialized views refreshed successfully."}`, nil
		},
	})

	// Phase 11 vendor tools
	registry.Register(ai.ToolDefinition{
		Name:        "get_vendors",
		Description: "List all active vendors for the company. Returns vendor code, name, contact, payment terms, and AP account.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
			"required":             []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			return s.getVendorsJSON(hctx, companyCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "search_vendors",
		Description: "Search vendors by name or code using partial match. Returns matching active vendors.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search text to match against vendor name or code (e.g. 'Acme', 'V001').",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			query, _ := params["query"].(string)
			return s.searchVendors(hctx, companyCode, query)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_vendor_info",
		Description: "Get full details for a specific vendor by vendor code, including contact, address, payment terms, and AP account.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"vendor_code": map[string]any{
					"type":        "string",
					"description": "The vendor code (e.g. 'V001').",
				},
			},
			"required": []string{"vendor_code"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			vendorCode, _ := params["vendor_code"].(string)
			return s.getVendorInfoJSON(hctx, companyCode, vendorCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "create_vendor",
		Description: "Propose creating a new vendor. The user must confirm before the vendor is saved. Requires at least a code and name.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"code": map[string]any{
					"type":        "string",
					"description": "Unique vendor code (e.g. 'V004').",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Full vendor name.",
				},
				"contact_person": map[string]any{
					"type":        "string",
					"description": "Name of the primary contact person (optional).",
				},
				"email": map[string]any{
					"type":        "string",
					"description": "Vendor contact email (optional).",
				},
				"phone": map[string]any{
					"type":        "string",
					"description": "Vendor phone number (optional).",
				},
				"address": map[string]any{
					"type":        "string",
					"description": "Vendor mailing address (optional).",
				},
				"payment_terms_days": map[string]any{
					"type":        "integer",
					"description": "Payment due in N days (default 30).",
				},
				"ap_account_code": map[string]any{
					"type":        "string",
					"description": "Accounts Payable account code (default '2000').",
				},
				"default_expense_account_code": map[string]any{
					"type":        "string",
					"description": "Default expense account code for this vendor's invoices (optional).",
				},
			},
			"required": []string{"code", "name"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	// Phase 12 purchase order tools
	registry.Register(ai.ToolDefinition{
		Name:        "get_purchase_orders",
		Description: "List purchase orders for the company. Optionally filter by status: DRAFT, APPROVED, RECEIVED, INVOICED, PAID. Empty status returns all orders.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"status": map[string]any{
					"type":        "string",
					"description": "Filter by PO status (optional). One of: DRAFT, APPROVED, RECEIVED, INVOICED, PAID.",
				},
			},
			"required": []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			status, _ := params["status"].(string)
			return s.getPurchaseOrdersJSON(hctx, companyCode, status)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_open_pos",
		Description: "List all open (DRAFT or APPROVED) purchase orders for the company — orders not yet received, invoiced, or paid.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
			"required":             []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			return s.getOpenPOsJSON(hctx, companyCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "create_purchase_order",
		Description: "Propose creating a new purchase order for a vendor. The user must confirm before the PO is saved. Requires vendor code, PO date, and at least one line item.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"vendor_code": map[string]any{
					"type":        "string",
					"description": "Vendor code (e.g. 'V001').",
				},
				"po_date": map[string]any{
					"type":        "string",
					"description": "Purchase order date in YYYY-MM-DD format.",
				},
				"notes": map[string]any{
					"type":        "string",
					"description": "Optional notes or instructions for the PO.",
				},
				"lines": map[string]any{
					"type":        "array",
					"description": "List of PO line items. At least one required.",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"product_code": map[string]any{
								"type":        "string",
								"description": "Product code if ordering a catalogued product (optional).",
							},
							"description": map[string]any{
								"type":        "string",
								"description": "Line item description.",
							},
							"quantity": map[string]any{
								"type":        "number",
								"description": "Quantity ordered.",
							},
							"unit_cost": map[string]any{
								"type":        "number",
								"description": "Unit cost in the PO currency.",
							},
							"expense_account_code": map[string]any{
								"type":        "string",
								"description": "Expense account code for non-inventory lines (optional).",
							},
						},
						"required": []string{"description", "quantity", "unit_cost"},
					},
				},
			},
			"required": []string{"vendor_code", "po_date", "lines"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	registry.Register(ai.ToolDefinition{
		Name:        "approve_po",
		Description: "Propose approving a DRAFT purchase order. Assigns a gapless PO number on approval. The user must confirm before the action is executed.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"po_id": map[string]any{
					"type":        "integer",
					"description": "Internal ID of the purchase order to approve.",
				},
			},
			"required": []string{"po_id"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	// Phase 13 goods receipt tools
	registry.Register(ai.ToolDefinition{
		Name:        "check_stock_availability",
		Description: "Check current inventory stock levels for products, optionally filtered by an APPROVED purchase order. Returns on-hand, reserved, and available quantities per product/warehouse, plus PO line details when a po_id is provided.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"po_id": map[string]any{
					"type":        "integer",
					"description": "Optional: purchase order ID to show stock levels for products in that PO.",
				},
				"product_code": map[string]any{
					"type":        "string",
					"description": "Optional: filter to a specific product code.",
				},
			},
			"required": []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			poID := 0
			if v, ok := params["po_id"].(float64); ok {
				poID = int(v)
			}
			productCode, _ := params["product_code"].(string)
			return s.checkStockAvailabilityJSON(hctx, companyCode, poID, productCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "receive_po",
		Description: "Propose recording goods/services received against an APPROVED purchase order. Updates inventory stock levels and creates the DR Inventory / CR AP accounting entry. The user must confirm before the receipt is posted.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"po_id": map[string]any{
					"type":        "integer",
					"description": "Internal ID of the approved purchase order to receive against.",
				},
				"warehouse_code": map[string]any{
					"type":        "string",
					"description": "Warehouse code to receive goods into (optional; defaults to the company's default warehouse).",
				},
				"lines": map[string]any{
					"type":        "array",
					"description": "Lines to receive. At least one required.",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"po_line_id": map[string]any{
								"type":        "integer",
								"description": "Internal ID of the purchase order line being received.",
							},
							"qty_received": map[string]any{
								"type":        "number",
								"description": "Quantity received on this line.",
							},
						},
						"required": []string{"po_line_id", "qty_received"},
					},
				},
			},
			"required": []string{"po_id", "lines"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	// Phase 14 vendor invoice + payment tools
	registry.Register(ai.ToolDefinition{
		Name:        "get_ap_balance",
		Description: "Get the current Accounts Payable balance for the company and optionally for a specific vendor. Returns total outstanding AP and per-vendor breakdown.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"vendor_code": map[string]any{
					"type":        "string",
					"description": "Optional: filter to a specific vendor code to see AP balance for that vendor only.",
				},
			},
			"required": []string{},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			vendorCode, _ := params["vendor_code"].(string)
			return s.getAPBalanceJSON(hctx, companyCode, vendorCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "get_vendor_payment_history",
		Description: "Get payment history for a vendor: list of paid purchase orders with invoice numbers, amounts, and payment dates.",
		IsReadTool:  true,
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"vendor_code": map[string]any{
					"type":        "string",
					"description": "The vendor code to retrieve payment history for (e.g. 'V001').",
				},
			},
			"required": []string{"vendor_code"},
		},
		Handler: func(hctx context.Context, params map[string]any) (string, error) {
			vendorCode, _ := params["vendor_code"].(string)
			return s.getVendorPaymentHistoryJSON(hctx, companyCode, vendorCode)
		},
	})

	registry.Register(ai.ToolDefinition{
		Name:        "record_vendor_invoice",
		Description: "Propose recording a vendor invoice against a RECEIVED purchase order. Creates a PI document number and transitions PO to INVOICED. The user must confirm before the action is executed.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"po_id": map[string]any{
					"type":        "integer",
					"description": "Internal ID of the RECEIVED purchase order to invoice.",
				},
				"invoice_number": map[string]any{
					"type":        "string",
					"description": "Vendor's invoice number as printed on the bill.",
				},
				"invoice_date": map[string]any{
					"type":        "string",
					"description": "Invoice date in YYYY-MM-DD format.",
				},
				"invoice_amount": map[string]any{
					"type":        "number",
					"description": "Total invoice amount in base currency. If it differs from the PO total by more than 5%, a warning is produced.",
				},
			},
			"required": []string{"po_id", "invoice_number", "invoice_date", "invoice_amount"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	registry.Register(ai.ToolDefinition{
		Name:        "pay_vendor",
		Description: "Propose paying a vendor for an INVOICED purchase order. Posts DR AP / CR Bank and transitions PO to PAID. The user must confirm before the payment is posted.",
		IsReadTool:  false, // write tool — requires human confirmation
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"po_id": map[string]any{
					"type":        "integer",
					"description": "Internal ID of the INVOICED purchase order to pay.",
				},
				"bank_account_code": map[string]any{
					"type":        "string",
					"description": "Account code of the bank account to pay from (e.g. '1100').",
				},
				"payment_date": map[string]any{
					"type":        "string",
					"description": "Payment date in YYYY-MM-DD format.",
				},
			},
			"required": []string{"po_id", "bank_account_code", "payment_date"},
		},
		Handler: nil, // write tool — no autonomous execution
	})

	return registry
}

// RecordVendorInvoice records the vendor's invoice against a RECEIVED PO.
func (s *appService) RecordVendorInvoice(ctx context.Context, req VendorInvoiceRequest) (*VendorInvoiceResult, error) {
	company, err := s.fetchCompany(ctx, req.CompanyCode)
	if err != nil {
		return nil, err
	}
	warning, err := s.purchaseOrderService.RecordVendorInvoice(
		ctx, company.ID, req.POID, req.InvoiceNumber, req.InvoiceDate, req.InvoiceAmount, s.docService,
	)
	if err != nil {
		return nil, err
	}
	po, err := s.purchaseOrderService.GetPO(ctx, req.POID)
	if err != nil {
		return nil, err
	}
	piDocNum := ""
	if po.PIDocumentNumber != nil {
		piDocNum = *po.PIDocumentNumber
	}
	return &VendorInvoiceResult{PurchaseOrder: po, PIDocumentNumber: piDocNum, Warning: warning}, nil
}

// PayVendor records payment against an INVOICED PO.
func (s *appService) PayVendor(ctx context.Context, req PayVendorRequest) (*PaymentResult, error) {
	if err := s.purchaseOrderService.PayVendor(
		ctx, req.POID, req.BankAccountCode, req.PaymentDate, req.CompanyCode, s.ledger,
	); err != nil {
		return nil, err
	}
	po, err := s.purchaseOrderService.GetPO(ctx, req.POID)
	if err != nil {
		return nil, err
	}
	return &PaymentResult{PurchaseOrder: po}, nil
}

// ReceivePurchaseOrder records goods and/or services received against an APPROVED PO.
func (s *appService) ReceivePurchaseOrder(ctx context.Context, req ReceivePORequest) (*POReceiptResult, error) {
	company, err := s.fetchCompany(ctx, req.CompanyCode)
	if err != nil {
		return nil, err
	}

	warehouseCode := req.WarehouseCode
	if warehouseCode == "" {
		wh, err := s.inventoryService.GetDefaultWarehouse(ctx, req.CompanyCode)
		if err != nil {
			return nil, fmt.Errorf("no active warehouse found: %w", err)
		}
		warehouseCode = wh.Code
	}

	// Look up the vendor's AP account code via the PO
	var apAccountCode string
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(v.ap_account_code, '2000')
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		WHERE po.id = $1 AND po.company_id = $2`,
		req.POID, company.ID,
	).Scan(&apAccountCode); err != nil {
		return nil, fmt.Errorf("resolve AP account for PO %d: %w", req.POID, err)
	}

	// Convert request lines to domain lines
	domainLines := make([]core.ReceivedLine, len(req.Lines))
	for i, l := range req.Lines {
		domainLines[i] = core.ReceivedLine{
			POLineID:    l.POLineID,
			QtyReceived: l.QtyReceived,
		}
	}

	if err := s.purchaseOrderService.ReceivePO(ctx, req.POID, warehouseCode, req.CompanyCode,
		domainLines, apAccountCode, s.ledger, s.docService, s.inventoryService); err != nil {
		return nil, err
	}

	po, err := s.purchaseOrderService.GetPO(ctx, req.POID)
	if err != nil {
		return nil, err
	}
	return &POReceiptResult{PurchaseOrder: po, LinesReceived: len(req.Lines)}, nil
}

// checkStockAvailabilityJSON returns current stock levels, optionally scoped to a PO's products.
func (s *appService) checkStockAvailabilityJSON(ctx context.Context, companyCode string, poID int, productCode string) (string, error) {
	result := map[string]any{}

	if poID > 0 {
		// Include PO context
		po, err := s.purchaseOrderService.GetPO(ctx, poID)
		if err != nil {
			return "", fmt.Errorf("PO %d not found: %w", poID, err)
		}
		result["po_id"] = po.ID
		result["po_status"] = po.Status
		if po.PONumber != nil {
			result["po_number"] = *po.PONumber
		}
		lines := make([]map[string]any, 0, len(po.Lines))
		for _, l := range po.Lines {
			m := map[string]any{
				"po_line_id":  l.ID,
				"line_number": l.LineNumber,
				"description": l.Description,
				"quantity":    l.Quantity.String(),
				"unit_cost":   l.UnitCost.String(),
			}
			if l.ProductCode != nil {
				m["product_code"] = *l.ProductCode
			}
			if l.ExpenseAccountCode != nil {
				m["expense_account_code"] = *l.ExpenseAccountCode
			}
			lines = append(lines, m)
		}
		result["po_lines"] = lines
	}

	stockLevels, err := s.inventoryService.GetStockLevels(ctx, companyCode)
	if err != nil {
		return "", err
	}

	var filtered []map[string]any
	for _, sl := range stockLevels {
		if productCode != "" && sl.ProductCode != productCode {
			continue
		}
		filtered = append(filtered, map[string]any{
			"product_code":   sl.ProductCode,
			"product_name":   sl.ProductName,
			"warehouse_code": sl.WarehouseCode,
			"warehouse_name": sl.WarehouseName,
			"on_hand":        sl.OnHand.String(),
			"reserved":       sl.Reserved.String(),
			"available":      sl.Available.String(),
			"unit_cost":      sl.UnitCost.String(),
		})
	}
	if filtered == nil {
		filtered = []map[string]any{}
	}
	result["stock_levels"] = filtered

	data, _ := json.Marshal(result)
	return string(data), nil
}

// getAPBalanceJSON returns outstanding AP balance, optionally filtered by vendor.
func (s *appService) getAPBalanceJSON(ctx context.Context, companyCode, vendorCode string) (string, error) {
	// AP balance = sum of POs in INVOICED status (not yet PAID)
	query := `
		SELECT COALESCE(SUM(
			CASE WHEN po.invoice_amount IS NOT NULL THEN po.invoice_amount ELSE po.total_base END
		), 0),
		COUNT(*)
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		JOIN companies c ON c.id = po.company_id
		WHERE c.company_code = $1 AND po.status = 'INVOICED'`
	args := []any{companyCode}
	if vendorCode != "" {
		query += " AND v.code = $2"
		args = append(args, vendorCode)
	}

	var totalAP decimal.Decimal
	var count int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&totalAP, &count); err != nil {
		return "", fmt.Errorf("get AP balance: %w", err)
	}

	result := map[string]any{
		"total_ap_outstanding": totalAP.StringFixed(2),
		"invoiced_po_count":    count,
	}
	if vendorCode != "" {
		result["vendor_code"] = vendorCode
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

// getVendorPaymentHistoryJSON returns payment history for a vendor.
func (s *appService) getVendorPaymentHistoryJSON(ctx context.Context, companyCode, vendorCode string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT po.id, po.po_number, po.invoice_number, po.invoice_date::text,
		       po.invoice_amount, po.total_base, po.paid_at, po.pi_document_number
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		JOIN companies c ON c.id = po.company_id
		WHERE c.company_code = $1 AND v.code = $2 AND po.status = 'PAID'
		ORDER BY po.paid_at DESC`,
		companyCode, vendorCode,
	)
	if err != nil {
		return "", fmt.Errorf("get vendor payment history: %w", err)
	}
	defer rows.Close()

	type paymentRecord struct {
		POID             int     `json:"po_id"`
		PONumber         *string `json:"po_number"`
		InvoiceNumber    *string `json:"invoice_number"`
		InvoiceDate      *string `json:"invoice_date"`
		InvoiceAmount    *string `json:"invoice_amount"`
		POTotal          string  `json:"po_total"`
		PaidAt           *string `json:"paid_at"`
		PIDocumentNumber *string `json:"pi_document_number"`
	}

	var payments []paymentRecord
	for rows.Next() {
		var pr paymentRecord
		var totalBase decimal.Decimal
		var invoiceAmount *decimal.Decimal
		var paidAt *time.Time
		if err := rows.Scan(
			&pr.POID, &pr.PONumber, &pr.InvoiceNumber, &pr.InvoiceDate,
			&invoiceAmount, &totalBase, &paidAt, &pr.PIDocumentNumber,
		); err != nil {
			return "", fmt.Errorf("scan payment record: %w", err)
		}
		pr.POTotal = totalBase.StringFixed(2)
		if invoiceAmount != nil {
			s := invoiceAmount.StringFixed(2)
			pr.InvoiceAmount = &s
		}
		if paidAt != nil {
			s := paidAt.Format("2006-01-02")
			pr.PaidAt = &s
		}
		payments = append(payments, pr)
	}
	if payments == nil {
		payments = []paymentRecord{}
	}

	result := map[string]any{
		"vendor_code":      vendorCode,
		"payment_count":    len(payments),
		"payment_history":  payments,
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

// CommitProposal validates and posts an AI-generated proposal to the ledger.
func (s *appService) CommitProposal(ctx context.Context, proposal core.Proposal) error {
	return s.ledger.Commit(ctx, proposal)
}

// ValidateProposal validates a proposal without committing it.
func (s *appService) ValidateProposal(ctx context.Context, proposal core.Proposal) error {
	return s.ledger.Validate(ctx, proposal)
}

// LoadDefaultCompany loads the active company, using COMPANY_CODE env var if set.
func (s *appService) LoadDefaultCompany(ctx context.Context) (*core.Company, error) {
	if code := os.Getenv("COMPANY_CODE"); code != "" {
		c := &core.Company{}
		err := s.pool.QueryRow(ctx,
			"SELECT id, company_code, name, base_currency FROM companies WHERE company_code = $1", code,
		).Scan(&c.ID, &c.CompanyCode, &c.Name, &c.BaseCurrency)
		if err != nil {
			return nil, fmt.Errorf("company %s not found: %w", code, err)
		}
		return c, nil
	}

	var count int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM companies").Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count companies: %w", err)
	}
	if count > 1 {
		return nil, fmt.Errorf("multiple companies found; set COMPANY_CODE env var (e.g. COMPANY_CODE=1000)")
	}

	c := &core.Company{}
	if err := s.pool.QueryRow(ctx,
		"SELECT id, company_code, name, base_currency FROM companies LIMIT 1",
	).Scan(&c.ID, &c.CompanyCode, &c.Name, &c.BaseCurrency); err != nil {
		return nil, fmt.Errorf("no default company found, have migrations run?: %w", err)
	}
	return c, nil
}

// ── private helpers ───────────────────────────────────────────────────────────

// resolveOrder looks up a sales order by numeric ID or order number string.
func (s *appService) resolveOrder(ctx context.Context, ref, companyCode string) (*core.SalesOrder, error) {
	if id, err := strconv.Atoi(ref); err == nil {
		return s.orderService.GetOrder(ctx, id)
	}
	return s.orderService.GetOrderByNumber(ctx, companyCode, ref)
}

// fetchCompany retrieves a company record by code.
func (s *appService) fetchCompany(ctx context.Context, companyCode string) (*core.Company, error) {
	c := &core.Company{}
	if err := s.pool.QueryRow(ctx,
		"SELECT id, company_code, name, base_currency FROM companies WHERE company_code = $1", companyCode,
	).Scan(&c.ID, &c.CompanyCode, &c.Name, &c.BaseCurrency); err != nil {
		return nil, fmt.Errorf("company %s not found: %w", companyCode, err)
	}
	return c, nil
}

// fetchCoA returns the chart of accounts for a company as a formatted string for the AI prompt.
func (s *appService) fetchCoA(ctx context.Context, companyCode string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.code, a.name, a.type
		FROM accounts a
		JOIN companies c ON c.id = a.company_id
		WHERE c.company_code = $1
		ORDER BY a.code
	`, companyCode)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var code, name, accType string
		if err := rows.Scan(&code, &name, &accType); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("- %s %s (%s)", code, name, accType))
	}
	return strings.Join(lines, "\n"), nil
}

// fetchDocumentTypes returns all document types as a formatted string for the AI prompt.
func (s *appService) fetchDocumentTypes(ctx context.Context) (string, error) {
	rows, err := s.pool.Query(ctx, "SELECT code, name FROM document_types")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", code, name))
	}
	return strings.Join(lines, "\n"), nil
}

// ── read tool handlers (Phase 7.5) ───────────────────────────────────────────

// searchAccounts queries accounts by name or code using ILIKE similarity and returns JSON.
func (s *appService) searchAccounts(ctx context.Context, companyCode, query string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.code, a.name, a.type
		FROM accounts a
		JOIN companies c ON c.id = a.company_id
		WHERE c.company_code = $1
		  AND (a.name ILIKE '%' || $2 || '%' OR a.code ILIKE '%' || $2 || '%')
		ORDER BY a.code
		LIMIT 10
	`, companyCode, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var results []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Code, &r.Name, &r.Type); err != nil {
			return "", err
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"accounts":[],"note":"No accounts matched the query."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"accounts": results})
	return string(data), nil
}

// searchCustomers queries customers by name or code using ILIKE and returns JSON.
func (s *appService) searchCustomers(ctx context.Context, companyCode, query string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cu.code, cu.name, cu.email
		FROM customers cu
		JOIN companies c ON c.id = cu.company_id
		WHERE c.company_code = $1
		  AND (cu.name ILIKE '%' || $2 || '%' OR cu.code ILIKE '%' || $2 || '%')
		ORDER BY cu.code
		LIMIT 10
	`, companyCode, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		Code  string `json:"code"`
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
	var results []row
	for rows.Next() {
		var r row
		var email *string
		if err := rows.Scan(&r.Code, &r.Name, &email); err != nil {
			return "", err
		}
		if email != nil {
			r.Email = *email
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"customers":[],"note":"No customers matched the query."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"customers": results})
	return string(data), nil
}

// searchProducts queries products by name or code using ILIKE and returns JSON.
func (s *appService) searchProducts(ctx context.Context, companyCode, query string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.code, p.name, p.unit_price
		FROM products p
		JOIN companies c ON c.id = p.company_id
		WHERE c.company_code = $1
		  AND (p.name ILIKE '%' || $2 || '%' OR p.code ILIKE '%' || $2 || '%')
		ORDER BY p.code
		LIMIT 10
	`, companyCode, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		UnitPrice string `json:"unit_price"`
	}
	var results []row
	for rows.Next() {
		var r row
		var unitPrice decimal.Decimal
		if err := rows.Scan(&r.Code, &r.Name, &unitPrice); err != nil {
			return "", err
		}
		r.UnitPrice = unitPrice.String()
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"products":[],"note":"No products matched the query."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"products": results})
	return string(data), nil
}

// getStockLevels returns current inventory stock levels, optionally filtered, as JSON.
func (s *appService) getStockLevels(ctx context.Context, companyCode, productCode, warehouseCode string) (string, error) {
	q := `
		SELECT p.code, p.name, w.code AS warehouse_code, w.name AS warehouse_name,
		       ii.qty_on_hand, ii.qty_reserved
		FROM inventory_items ii
		JOIN products p ON p.id = ii.product_id
		JOIN warehouses w ON w.id = ii.warehouse_id
		JOIN companies c ON c.id = p.company_id
		WHERE c.company_code = $1
	`
	args := []any{companyCode}
	if productCode != "" {
		args = append(args, productCode)
		q += fmt.Sprintf(" AND p.code = $%d", len(args))
	}
	if warehouseCode != "" {
		args = append(args, warehouseCode)
		q += fmt.Sprintf(" AND w.code = $%d", len(args))
	}
	q += " ORDER BY p.code, w.code"

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		ProductCode   string `json:"product_code"`
		ProductName   string `json:"product_name"`
		WarehouseCode string `json:"warehouse_code"`
		WarehouseName string `json:"warehouse_name"`
		QtyOnHand     string `json:"qty_on_hand"`
		QtyReserved   string `json:"qty_reserved"`
		QtyAvailable  string `json:"qty_available"`
	}
	var results []row
	for rows.Next() {
		var r row
		var onHand, reserved decimal.Decimal
		if err := rows.Scan(&r.ProductCode, &r.ProductName, &r.WarehouseCode, &r.WarehouseName, &onHand, &reserved); err != nil {
			return "", err
		}
		r.QtyOnHand = onHand.String()
		r.QtyReserved = reserved.String()
		r.QtyAvailable = onHand.Sub(reserved).String()
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"stock_levels":[],"note":"No inventory records found for the given filters."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"stock_levels": results})
	return string(data), nil
}

// getAccountBalanceJSON returns the current net-debit balance for a single account as JSON.
func (s *appService) getAccountBalanceJSON(ctx context.Context, companyCode, accountCode string) (string, error) {
	var accountName string
	var netDebit decimal.Decimal
	err := s.pool.QueryRow(ctx, `
		SELECT a.name,
		       COALESCE(SUM(jl.debit_base), 0) - COALESCE(SUM(jl.credit_base), 0)
		FROM accounts a
		JOIN companies c ON c.id = a.company_id
		LEFT JOIN journal_lines jl ON jl.account_id = a.id
		WHERE c.company_code = $1 AND a.code = $2
		GROUP BY a.name
	`, companyCode, accountCode).Scan(&accountName, &netDebit)
	if err != nil {
		return fmt.Sprintf(`{"error":"account %s not found or no activity"}`, accountCode), nil
	}
	data, _ := json.Marshal(map[string]any{
		"account_code": accountCode,
		"account_name": accountName,
		"balance":      netDebit.StringFixed(2),
		"note":         "Net debit position: positive = net debit, negative = net credit",
	})
	return string(data), nil
}

// getAccountStatementJSON returns a statement for the account as JSON.
func (s *appService) getAccountStatementJSON(ctx context.Context, companyCode, accountCode, fromDate, toDate string) (string, error) {
	lines, err := s.reportingService.GetAccountStatement(ctx, companyCode, accountCode, fromDate, toDate)
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return fmt.Sprintf(`{"account_code":%q,"lines":[],"closing_balance":"0.00","note":"No movements found."}`, accountCode), nil
	}

	type jsonLine struct {
		Date      string `json:"date"`
		Narration string `json:"narration"`
		Reference string `json:"reference,omitempty"`
		Debit     string `json:"debit"`
		Credit    string `json:"credit"`
		Balance   string `json:"balance"`
	}
	out := make([]jsonLine, len(lines))
	for i, l := range lines {
		out[i] = jsonLine{
			Date:      l.PostingDate,
			Narration: l.Narration,
			Reference: l.Reference,
			Debit:     l.Debit.StringFixed(2),
			Credit:    l.Credit.StringFixed(2),
			Balance:   l.RunningBalance.StringFixed(2),
		}
	}
	closing := lines[len(lines)-1].RunningBalance
	data, _ := json.Marshal(map[string]any{
		"account_code":    accountCode,
		"lines":           out,
		"closing_balance": closing.StringFixed(2),
	})
	return string(data), nil
}

// getPLReportJSON returns the P&L report as JSON for AI tool consumption.
func (s *appService) getPLReportJSON(ctx context.Context, companyCode string, year, month int) (string, error) {
	report, err := s.reportingService.GetProfitAndLoss(ctx, companyCode, year, month)
	if err != nil {
		return "", err
	}

	type accountLine struct {
		Code    string `json:"code"`
		Name    string `json:"name"`
		Balance string `json:"balance"`
	}
	toLines := func(lines []core.AccountLine) []accountLine {
		out := make([]accountLine, len(lines))
		for i, l := range lines {
			out[i] = accountLine{Code: l.Code, Name: l.Name, Balance: l.Balance.StringFixed(2)}
		}
		return out
	}

	data, _ := json.Marshal(map[string]any{
		"company_code": companyCode,
		"year":         year,
		"month":        month,
		"revenue":      toLines(report.Revenue),
		"expenses":     toLines(report.Expenses),
		"net_income":   report.NetIncome.StringFixed(2),
	})
	return string(data), nil
}

// getBalanceSheetJSON returns the Balance Sheet as JSON for AI tool consumption.
func (s *appService) getBalanceSheetJSON(ctx context.Context, companyCode, asOfDate string) (string, error) {
	report, err := s.reportingService.GetBalanceSheet(ctx, companyCode, asOfDate)
	if err != nil {
		return "", err
	}

	type accountLine struct {
		Code    string `json:"code"`
		Name    string `json:"name"`
		Balance string `json:"balance"`
	}
	toLines := func(lines []core.AccountLine) []accountLine {
		out := make([]accountLine, len(lines))
		for i, l := range lines {
			out[i] = accountLine{Code: l.Code, Name: l.Name, Balance: l.Balance.StringFixed(2)}
		}
		return out
	}

	data, _ := json.Marshal(map[string]any{
		"company_code":      companyCode,
		"as_of_date":        report.AsOfDate,
		"assets":            toLines(report.Assets),
		"liabilities":       toLines(report.Liabilities),
		"equity":            toLines(report.Equity),
		"total_assets":      report.TotalAssets.StringFixed(2),
		"total_liabilities": report.TotalLiabilities.StringFixed(2),
		"total_equity":      report.TotalEquity.StringFixed(2),
		"is_balanced":       report.IsBalanced,
	})
	return string(data), nil
}

// ── vendor tool helpers (Phase 11) ───────────────────────────────────────────

// getVendorsJSON returns all active vendors for the company as JSON.
func (s *appService) getVendorsJSON(ctx context.Context, companyCode string) (string, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return "", err
	}
	vendors, err := s.vendorService.GetVendors(ctx, company.ID)
	if err != nil {
		return "", err
	}
	if len(vendors) == 0 {
		return `{"vendors":[],"note":"No active vendors found."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"vendors": vendorsToJSON(vendors)})
	return string(data), nil
}

// searchVendors queries vendors by name (trigram similarity via GIN index from migration 021)
// or by code (ILIKE prefix/contains). Results ordered by name similarity then code.
func (s *appService) searchVendors(ctx context.Context, companyCode, query string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.code, v.name, v.email, v.phone, v.payment_terms_days, v.ap_account_code
		FROM vendors v
		JOIN companies c ON c.id = v.company_id
		WHERE c.company_code = $1
		  AND v.is_active = true
		  AND (v.name % $2 OR v.code ILIKE '%' || $2 || '%')
		ORDER BY similarity(v.name, $2) DESC, v.code
		LIMIT 10
	`, companyCode, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		Code             string  `json:"code"`
		Name             string  `json:"name"`
		Email            *string `json:"email,omitempty"`
		Phone            *string `json:"phone,omitempty"`
		PaymentTermsDays int     `json:"payment_terms_days"`
		APAccountCode    string  `json:"ap_account_code"`
	}
	var results []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Code, &r.Name, &r.Email, &r.Phone, &r.PaymentTermsDays, &r.APAccountCode); err != nil {
			return "", err
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"vendors":[],"note":"No vendors matched the query."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"vendors": results})
	return string(data), nil
}

// getVendorInfoJSON returns full vendor details by code as JSON.
func (s *appService) getVendorInfoJSON(ctx context.Context, companyCode, vendorCode string) (string, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return "", err
	}
	v, err := s.vendorService.GetVendorByCode(ctx, company.ID, vendorCode)
	if err != nil {
		return fmt.Sprintf(`{"error":"vendor %q not found"}`, vendorCode), nil
	}
	type out struct {
		Code                      string  `json:"code"`
		Name                      string  `json:"name"`
		ContactPerson             *string `json:"contact_person,omitempty"`
		Email                     *string `json:"email,omitempty"`
		Phone                     *string `json:"phone,omitempty"`
		Address                   *string `json:"address,omitempty"`
		PaymentTermsDays          int     `json:"payment_terms_days"`
		APAccountCode             string  `json:"ap_account_code"`
		DefaultExpenseAccountCode *string `json:"default_expense_account_code,omitempty"`
		IsActive                  bool    `json:"is_active"`
	}
	data, _ := json.Marshal(out{
		Code:                      v.Code,
		Name:                      v.Name,
		ContactPerson:             v.ContactPerson,
		Email:                     v.Email,
		Phone:                     v.Phone,
		Address:                   v.Address,
		PaymentTermsDays:          v.PaymentTermsDays,
		APAccountCode:             v.APAccountCode,
		DefaultExpenseAccountCode: v.DefaultExpenseAccountCode,
		IsActive:                  v.IsActive,
	})
	return string(data), nil
}

// vendorsToJSON converts a slice of Vendor to a JSON-friendly format.
func vendorsToJSON(vendors []core.Vendor) []map[string]any {
	out := make([]map[string]any, len(vendors))
	for i, v := range vendors {
		m := map[string]any{
			"code":               v.Code,
			"name":               v.Name,
			"payment_terms_days": v.PaymentTermsDays,
			"ap_account_code":    v.APAccountCode,
		}
		if v.Email != nil {
			m["email"] = *v.Email
		}
		if v.Phone != nil {
			m["phone"] = *v.Phone
		}
		if v.ContactPerson != nil {
			m["contact_person"] = *v.ContactPerson
		}
		out[i] = m
	}
	return out
}

// getPurchaseOrdersJSON returns purchase orders for the company as JSON, optionally filtered by status.
func (s *appService) getPurchaseOrdersJSON(ctx context.Context, companyCode, status string) (string, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return "", err
	}
	orders, err := s.purchaseOrderService.GetPOs(ctx, company.ID, status)
	if err != nil {
		return "", err
	}
	if len(orders) == 0 {
		return `{"purchase_orders":[],"note":"No purchase orders found."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"purchase_orders": purchaseOrdersToJSON(orders)})
	return string(data), nil
}

// getOpenPOsJSON returns DRAFT and APPROVED purchase orders for the company as JSON.
func (s *appService) getOpenPOsJSON(ctx context.Context, companyCode string) (string, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return "", err
	}

	var allOpen []core.PurchaseOrder
	for _, st := range []string{"DRAFT", "APPROVED"} {
		orders, err := s.purchaseOrderService.GetPOs(ctx, company.ID, st)
		if err != nil {
			return "", err
		}
		allOpen = append(allOpen, orders...)
	}

	if len(allOpen) == 0 {
		return `{"purchase_orders":[],"note":"No open purchase orders found."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"purchase_orders": purchaseOrdersToJSON(allOpen)})
	return string(data), nil
}

// purchaseOrdersToJSON converts a slice of PurchaseOrder to a JSON-friendly format.
func purchaseOrdersToJSON(orders []core.PurchaseOrder) []map[string]any {
	out := make([]map[string]any, len(orders))
	for i, po := range orders {
		m := map[string]any{
			"id":                po.ID,
			"vendor_code":       po.VendorCode,
			"vendor_name":       po.VendorName,
			"status":            po.Status,
			"po_date":           po.PODate,
			"currency":          po.Currency,
			"total_transaction": po.TotalTransaction.String(),
			"total_base":        po.TotalBase.String(),
		}
		if po.PONumber != nil {
			m["po_number"] = *po.PONumber
		}
		if po.ExpectedDeliveryDate != nil {
			m["expected_delivery_date"] = *po.ExpectedDeliveryDate
		}
		if po.Notes != nil {
			m["notes"] = *po.Notes
		}
		out[i] = m
	}
	return out
}

// getWarehousesJSON returns all active warehouses for the company as JSON.
func (s *appService) getWarehousesJSON(ctx context.Context, companyCode string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.code, w.name, w.location
		FROM warehouses w
		JOIN companies c ON c.id = w.company_id
		WHERE c.company_code = $1
		  AND w.is_active = true
		ORDER BY w.code
	`, companyCode)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type row struct {
		Code     string  `json:"code"`
		Name     string  `json:"name"`
		Location *string `json:"location,omitempty"`
	}
	var results []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Code, &r.Name, &r.Location); err != nil {
			return "", err
		}
		results = append(results, r)
	}
	if len(results) == 0 {
		return `{"warehouses":[],"note":"No active warehouses found."}`, nil
	}
	data, _ := json.Marshal(map[string]any{"warehouses": results})
	return string(data), nil
}

// AuthenticateUser verifies credentials and returns a session on success.
func (s *appService) AuthenticateUser(ctx context.Context, username, password string) (*UserSession, error) {
	user, err := s.userService.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	if !user.IsActive {
		return nil, fmt.Errorf("account is inactive")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	var companyCode string
	if err := s.pool.QueryRow(ctx,
		"SELECT company_code FROM companies WHERE id = $1", user.CompanyID,
	).Scan(&companyCode); err != nil {
		return nil, fmt.Errorf("company not found for user")
	}

	return &UserSession{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		CompanyCode: companyCode,
		CompanyID:   user.CompanyID,
	}, nil
}

// GetUser returns user profile by ID, including company code.
func (s *appService) GetUser(ctx context.Context, userID int) (*UserResult, error) {
	user, err := s.userService.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	var companyCode string
	if err := s.pool.QueryRow(ctx,
		"SELECT company_code FROM companies WHERE id = $1", user.CompanyID,
	).Scan(&companyCode); err != nil {
		return nil, fmt.Errorf("company not found for user")
	}

	return &UserResult{
		UserID:      user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		IsActive:    user.IsActive,
		CompanyCode: companyCode,
	}, nil
}
