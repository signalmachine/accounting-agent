package app

import "accounting-agent/internal/core"

// TrialBalanceResult is returned by GetTrialBalance.
type TrialBalanceResult struct {
	CompanyCode string
	CompanyName string
	Currency    string
	Accounts    []core.AccountBalance
}

// OrderResult is returned by order lifecycle operations.
type OrderResult struct {
	Order *core.SalesOrder
}

// OrderListResult is returned by ListOrders.
type OrderListResult struct {
	Orders      []core.SalesOrder
	CompanyCode string
}

// StockResult is returned by GetStockLevels.
type StockResult struct {
	Levels      []core.StockLevel
	CompanyCode string
}

// CustomerListResult is returned by ListCustomers.
type CustomerListResult struct {
	Customers []core.Customer
}

// ProductListResult is returned by ListProducts.
type ProductListResult struct {
	Products []core.Product
}

// WarehouseListResult is returned by ListWarehouses.
type WarehouseListResult struct {
	Warehouses []core.Warehouse
}

// AccountStatementResult is returned by GetAccountStatement.
type AccountStatementResult struct {
	CompanyCode string
	AccountCode string
	Currency    string
	Lines       []core.StatementLine
}

// AIResult is returned by InterpretEvent.
type AIResult struct {
	Proposal             *core.Proposal
	ClarificationMessage string
	IsClarification      bool
}

// DomainActionKind identifies the terminal outcome of an InterpretDomainAction call.
type DomainActionKind string

const (
	// DomainActionKindAnswer means the agent gathered context and produced a plain-text answer.
	DomainActionKindAnswer DomainActionKind = "answer"
	// DomainActionKindClarification means the agent needs more information from the user.
	DomainActionKindClarification DomainActionKind = "clarification"
	// DomainActionKindProposed means the agent is proposing a domain write action for human confirmation.
	DomainActionKindProposed DomainActionKind = "proposed"
	// DomainActionKindJournalEntry means the input is a financial event; the adapter should
	// route it to InterpretEvent for a structured-output journal entry proposal.
	DomainActionKindJournalEntry DomainActionKind = "journal_entry"
)

// DomainActionResult is returned by InterpretDomainAction.
type DomainActionResult struct {
	Kind DomainActionKind

	// Answer is populated when Kind == DomainActionKindAnswer.
	Answer string

	// Question and Context are populated when Kind == DomainActionKindClarification.
	Question string
	Context  string

	// ToolName and ToolArgs are populated when Kind == DomainActionKindProposed.
	ToolName string
	ToolArgs map[string]any

	// EventDescription is populated when Kind == DomainActionKindJournalEntry.
	// The adapter should pass this to InterpretEvent.
	EventDescription string
}
