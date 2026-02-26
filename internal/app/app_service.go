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
)

type appService struct {
	pool             *pgxpool.Pool
	ledger           *core.Ledger
	docService       core.DocumentService
	orderService     core.OrderService
	inventoryService core.InventoryService
	reportingService core.ReportingService
	agent            *ai.Agent
}

// NewAppService constructs an appService that satisfies ApplicationService.
func NewAppService(
	pool *pgxpool.Pool,
	ledger *core.Ledger,
	docService core.DocumentService,
	orderService core.OrderService,
	inventoryService core.InventoryService,
	reportingService core.ReportingService,
	agent *ai.Agent,
) ApplicationService {
	return &appService{
		pool:             pool,
		ledger:           ledger,
		docService:       docService,
		orderService:     orderService,
		inventoryService: inventoryService,
		reportingService: reportingService,
		agent:            agent,
	}
}

// GetTrialBalance returns the trial balance for the given company.
func (s *appService) GetTrialBalance(ctx context.Context, companyCode string) (*TrialBalanceResult, error) {
	var companyName, currency string
	if err := s.pool.QueryRow(ctx,
		"SELECT name, base_currency FROM companies WHERE company_code = $1", companyCode,
	).Scan(&companyName, &currency); err != nil {
		return nil, fmt.Errorf("company %s not found: %w", companyCode, err)
	}

	balances, err := s.ledger.GetBalances(ctx, companyCode)
	if err != nil {
		return nil, err
	}

	return &TrialBalanceResult{
		CompanyCode: companyCode,
		CompanyName: companyName,
		Currency:    currency,
		Accounts:    balances,
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
		req.Qty, req.UnitCost, movementDate, creditAccount, s.ledger, s.docService)
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
func (s *appService) InterpretDomainAction(ctx context.Context, text, companyCode string) (*DomainActionResult, error) {
	company, err := s.fetchCompany(ctx, companyCode)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch company: %w", err)
	}

	registry := s.buildToolRegistry(ctx, companyCode)

	result, err := s.agent.InterpretDomainAction(ctx, text, company, registry)
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

	return registry
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
