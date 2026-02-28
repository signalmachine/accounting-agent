package app

import (
	"time"

	"github.com/shopspring/decimal"
)

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

// CreateVendorRequest is the input for creating a new vendor.
type CreateVendorRequest struct {
	CompanyCode               string
	Code                      string
	Name                      string
	ContactPerson             string
	Email                     string
	Phone                     string
	Address                   string
	PaymentTermsDays          int
	APAccountCode             string
	DefaultExpenseAccountCode string
}

// CreatePurchaseOrderRequest is the input for creating a new purchase order.
type CreatePurchaseOrderRequest struct {
	CompanyCode          string
	VendorCode           string
	PODate               string // YYYY-MM-DD
	Notes                string
	Lines                []POLineInput
}

// POLineInput is a single line within a CreatePurchaseOrderRequest.
type POLineInput struct {
	ProductCode        string
	Description        string
	Quantity           decimal.Decimal
	UnitCost           decimal.Decimal
	ExpenseAccountCode string
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

// ReceivePORequest is the input for recording goods/services received against a PO.
type ReceivePORequest struct {
	CompanyCode   string
	POID          int
	WarehouseCode string          // optional; defaults to the company's default warehouse
	Lines         []ReceivedLineInput
}

// ReceivedLineInput is a single line in a ReceivePORequest.
type ReceivedLineInput struct {
	POLineID    int
	QtyReceived decimal.Decimal
}

// VendorInvoiceRequest is the input for recording a vendor invoice against a RECEIVED PO.
type VendorInvoiceRequest struct {
	CompanyCode   string
	POID          int
	InvoiceNumber string
	InvoiceDate   time.Time
	InvoiceAmount decimal.Decimal
}

// PayVendorRequest is the input for recording payment against an INVOICED PO.
type PayVendorRequest struct {
	CompanyCode     string
	POID            int
	BankAccountCode string
	PaymentDate     time.Time
}
