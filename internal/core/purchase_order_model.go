package core

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// PurchaseOrder represents a purchase order header.
type PurchaseOrder struct {
	ID                   int
	CompanyID            int
	VendorID             int
	VendorCode           string
	VendorName           string
	PONumber             *string
	Status               string
	PODate               string // YYYY-MM-DD
	ExpectedDeliveryDate *string
	Currency             string
	ExchangeRate         decimal.Decimal
	TotalTransaction     decimal.Decimal
	TotalBase            decimal.Decimal
	Notes                *string
	ApprovedAt           *time.Time
	ReceivedAt           *time.Time
	// Invoice fields (set by RecordVendorInvoice)
	InvoiceNumber    *string
	InvoiceDate      *string // YYYY-MM-DD
	InvoiceAmount    *decimal.Decimal
	PIDocumentNumber *string
	InvoicedAt       *time.Time
	// Payment fields (set by PayVendor)
	PaidAt *time.Time
	CreatedAt            time.Time
	Lines                []PurchaseOrderLine
}

// PurchaseOrderLine represents a single line on a purchase order.
type PurchaseOrderLine struct {
	ID                   int
	OrderID              int
	LineNumber           int
	ProductID            *int
	ProductCode          *string
	ProductName          *string
	Description          string
	Quantity             decimal.Decimal
	UnitCost             decimal.Decimal
	LineTotalTransaction decimal.Decimal
	LineTotalBase        decimal.Decimal
	ExpenseAccountCode   *string
}

// PurchaseOrderLineInput holds the fields required to create a purchase order line.
type PurchaseOrderLineInput struct {
	ProductCode        string
	Description        string
	Quantity           decimal.Decimal
	UnitCost           decimal.Decimal
	ExpenseAccountCode string
}

// ReceivedLine represents one PO line being received.
type ReceivedLine struct {
	POLineID    int             // references purchase_order_lines.id
	QtyReceived decimal.Decimal // quantity being received on this call
}

// PurchaseOrderService provides purchase order lifecycle operations.
type PurchaseOrderService interface {
	// CreatePO creates a new DRAFT purchase order with computed line totals.
	CreatePO(ctx context.Context, companyID, vendorID int, poDate time.Time, lines []PurchaseOrderLineInput, notes string) (*PurchaseOrder, error)

	// ApprovePO transitions a DRAFT PO to APPROVED, assigning a gapless PO number.
	// companyID must match the PO's company; returns an error if they differ.
	// It is idempotent: approving an already-APPROVED PO is a no-op.
	ApprovePO(ctx context.Context, companyID, poID int, docService DocumentService) error

	// ReceivePO records goods and/or services received against an APPROVED purchase order.
	// For physical-goods lines (product_id set): updates inventory via InventoryService.ReceiveStock
	// and links the movement to the PO line.
	// For service/expense lines (expense_account_code set, no product): posts DR expense / CR AP.
	// On completion, transitions PO status to RECEIVED.
	ReceivePO(ctx context.Context, poID int, warehouseCode, companyCode string,
		receivedLines []ReceivedLine, apAccountCode string,
		ledger *Ledger, docService DocumentService, inv InventoryService) error

	// RecordVendorInvoice records the vendor's invoice against a RECEIVED purchase order.
	// companyID must match the PO's company; returns an error if they differ.
	// Creates and posts a PI document (gapless number). Warns if invoiceAmount deviates
	// more than 5% from the PO total. Transitions status to INVOICED.
	// Returns a non-empty warning string if the amount deviation exceeds 5%.
	RecordVendorInvoice(ctx context.Context, companyID, poID int, invoiceNumber string, invoiceDate time.Time,
		invoiceAmount decimal.Decimal, docService DocumentService) (warning string, err error)

	// PayVendor records payment against an INVOICED purchase order.
	// Posts DR AP / CR Bank and transitions status to PAID.
	PayVendor(ctx context.Context, poID int, bankAccountCode string, paymentDate time.Time,
		companyCode string, ledger *Ledger) error

	// GetPO returns a purchase order by its internal ID, including all lines.
	GetPO(ctx context.Context, poID int) (*PurchaseOrder, error)

	// GetPOs returns purchase orders for a company, optionally filtered by status.
	// An empty status string returns all orders.
	GetPOs(ctx context.Context, companyID int, status string) ([]PurchaseOrder, error)
}
