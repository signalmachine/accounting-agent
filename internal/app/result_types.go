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

// AIResult is returned by InterpretEvent.
type AIResult struct {
	Proposal             *core.Proposal
	ClarificationMessage string
	IsClarification      bool
}
