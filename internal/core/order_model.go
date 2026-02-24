package core

import (
	"time"

	"github.com/shopspring/decimal"
)

// Customer represents a sales customer master record, scoped to a company.
type Customer struct {
	ID               int             `json:"id"`
	CompanyID        int             `json:"company_id"`
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Email            string          `json:"email"`
	Phone            string          `json:"phone"`
	Address          string          `json:"address"`
	CreditLimit      decimal.Decimal `json:"credit_limit"`
	PaymentTermsDays int             `json:"payment_terms_days"`
	CreatedAt        time.Time       `json:"created_at"`
}

// Product represents a sellable item or service in the company catalog.
// RevenueAccountCode links to the chart of accounts for automatic revenue booking.
type Product struct {
	ID                 int             `json:"id"`
	CompanyID          int             `json:"company_id"`
	Code               string          `json:"code"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	UnitPrice          decimal.Decimal `json:"unit_price"`
	Unit               string          `json:"unit"`
	RevenueAccountCode string          `json:"revenue_account_code"`
	IsActive           bool            `json:"is_active"`
	CreatedAt          time.Time       `json:"created_at"`
}

// SalesOrder represents a customer sales order header.
// Status progresses through the state machine:
//
//	DRAFT → CONFIRMED → SHIPPED → INVOICED → PAID
//	Any status → CANCELLED (only from DRAFT in Phase 2)
type SalesOrder struct {
	ID                int             `json:"id"`
	CompanyID         int             `json:"company_id"`
	OrderNumber       string          `json:"order_number"`  // assigned at CONFIRMED via DocumentService
	CustomerID        int             `json:"customer_id"`
	CustomerCode      string          `json:"customer_code"` // joined from customers
	CustomerName      string          `json:"customer_name"` // joined from customers
	Status            string          `json:"status"`
	OrderDate         string          `json:"order_date"` // YYYY-MM-DD
	Currency          string          `json:"currency"`
	ExchangeRate      decimal.Decimal `json:"exchange_rate"`
	TotalTransaction  decimal.Decimal `json:"total_transaction"`
	TotalBase         decimal.Decimal `json:"total_base"`
	Notes             string          `json:"notes"`
	InvoiceDocumentID *int            `json:"invoice_document_id,omitempty"`
	Lines             []SalesOrderLine `json:"lines"`
	CreatedAt         time.Time       `json:"created_at"`
	ConfirmedAt       *time.Time      `json:"confirmed_at,omitempty"`
	ShippedAt         *time.Time      `json:"shipped_at,omitempty"`
	InvoicedAt        *time.Time      `json:"invoiced_at,omitempty"`
	PaidAt            *time.Time      `json:"paid_at,omitempty"`
}

// SalesOrderLine represents one line item on a sales order.
type SalesOrderLine struct {
	ID                   int             `json:"id"`
	OrderID              int             `json:"order_id"`
	LineNumber           int             `json:"line_number"`
	ProductID            int             `json:"product_id"`
	ProductCode          string          `json:"product_code"`          // joined from products
	ProductName          string          `json:"product_name"`          // joined from products
	RevenueAccountCode   string          `json:"revenue_account_code"`  // joined from products
	Quantity             decimal.Decimal `json:"quantity"`
	UnitPrice            decimal.Decimal `json:"unit_price"`
	LineTotalTransaction decimal.Decimal `json:"line_total_transaction"`
	LineTotalBase        decimal.Decimal `json:"line_total_base"`
}

// OrderLineInput is used when creating a new sales order.
// If UnitPrice is zero, the product's default unit_price is used.
type OrderLineInput struct {
	ProductCode string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal // zero means "use product default"
}
