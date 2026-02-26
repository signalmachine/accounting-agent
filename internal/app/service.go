package app

import (
	"context"

	"accounting-agent/internal/core"
)

// ApplicationService is the single interface all UI adapters (REPL, CLI, Web) call.
// It decouples presentation from business logic. Implementations must contain
// no fmt.Println, no ANSI codes, and no display logic of any kind.
type ApplicationService interface {
	// GetTrialBalance returns a trial balance for the given company.
	GetTrialBalance(ctx context.Context, companyCode string) (*TrialBalanceResult, error)

	// ListCustomers returns all active customers for a company.
	ListCustomers(ctx context.Context, companyCode string) (*CustomerListResult, error)

	// ListProducts returns all active products for a company.
	ListProducts(ctx context.Context, companyCode string) (*ProductListResult, error)

	// ListOrders returns sales orders for a company, optionally filtered by status.
	ListOrders(ctx context.Context, companyCode string, status *string) (*OrderListResult, error)

	// CreateOrder creates a new DRAFT sales order.
	CreateOrder(ctx context.Context, req CreateOrderRequest) (*OrderResult, error)

	// ConfirmOrder transitions a DRAFT order to CONFIRMED, assigning an order number
	// and reserving stock. ref may be a numeric ID or order number string.
	ConfirmOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error)

	// ShipOrder transitions a CONFIRMED order to SHIPPED, deducting inventory and booking COGS.
	ShipOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error)

	// InvoiceOrder transitions a SHIPPED order to INVOICED, posting the sales invoice journal entry.
	InvoiceOrder(ctx context.Context, ref, companyCode string) (*OrderResult, error)

	// RecordPayment transitions an INVOICED order to PAID, posting the cash receipt journal entry.
	RecordPayment(ctx context.Context, ref, bankCode, companyCode string) (*OrderResult, error)

	// ListWarehouses returns all active warehouses for a company.
	ListWarehouses(ctx context.Context, companyCode string) (*WarehouseListResult, error)

	// GetStockLevels returns current stock levels for all inventory items in a company.
	GetStockLevels(ctx context.Context, companyCode string) (*StockResult, error)

	// ReceiveStock records a goods receipt: increases qty_on_hand and books DR Inventory / CR creditAccount.
	ReceiveStock(ctx context.Context, req ReceiveStockRequest) error

	// InterpretEvent sends a natural language event description to the AI agent and returns
	// either a journal entry Proposal or a clarification request.
	// This path uses structured output and must remain untouched per §16.4 of ai_agent_upgrade.md.
	InterpretEvent(ctx context.Context, text, companyCode string) (*AIResult, error)

	// InterpretDomainAction routes a natural language input through the agentic tool loop.
	// The agent calls read tools autonomously, then either proposes a domain write action,
	// asks a clarifying question, returns an answer, or signals that the input is a financial
	// event to be handled by InterpretEvent. InterpretEvent is not called by this method.
	InterpretDomainAction(ctx context.Context, text, companyCode string) (*DomainActionResult, error)

	// GetAccountStatement returns a chronological account statement with running balance.
	// fromDate and toDate are optional (empty string means unbounded).
	GetAccountStatement(ctx context.Context, companyCode, accountCode, fromDate, toDate string) (*AccountStatementResult, error)

	// CommitProposal validates and posts an AI-generated proposal to the ledger.
	// Must only be called after explicit user approval.
	CommitProposal(ctx context.Context, proposal core.Proposal) error

	// ValidateProposal validates a proposal without committing it.
	ValidateProposal(ctx context.Context, proposal core.Proposal) error

	// LoadDefaultCompany loads the active company. Uses COMPANY_CODE env var if set;
	// otherwise expects exactly one company in the database.
	LoadDefaultCompany(ctx context.Context) (*core.Company, error)
}
