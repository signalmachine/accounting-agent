package app

import (
	"context"
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
	agent            *ai.Agent
}

// NewAppService constructs an appService that satisfies ApplicationService.
func NewAppService(
	pool *pgxpool.Pool,
	ledger *core.Ledger,
	docService core.DocumentService,
	orderService core.OrderService,
	inventoryService core.InventoryService,
	agent *ai.Agent,
) ApplicationService {
	return &appService{
		pool:             pool,
		ledger:           ledger,
		docService:       docService,
		orderService:     orderService,
		inventoryService: inventoryService,
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
