package app

import "github.com/shopspring/decimal"

// CreateOrderRequest is the input for creating a new sales order.
type CreateOrderRequest struct {
	CompanyCode  string
	CustomerCode string
	Currency     string
	OrderDate    string
	Notes        string
	ExchangeRate decimal.Decimal
	Lines        []OrderLineInput
}

// OrderLineInput is a single line within a CreateOrderRequest.
type OrderLineInput struct {
	ProductCode string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal // zero means "use product default"
}

// ReceiveStockRequest is the input for recording a goods receipt into a warehouse.
type ReceiveStockRequest struct {
	CompanyCode       string
	ProductCode       string
	WarehouseCode     string
	CreditAccountCode string
	MovementDate      string
	Qty               decimal.Decimal
	UnitCost          decimal.Decimal
}
