package core

import (
	"time"

	"github.com/shopspring/decimal"
)

// Warehouse represents a physical storage location within a company.
type Warehouse struct {
	ID        int
	CompanyID int
	Code      string
	Name      string
	IsActive  bool
	CreatedAt time.Time
}

// StockLevel is a read view of an inventory_item joined with product and warehouse info.
type StockLevel struct {
	ProductCode   string
	ProductName   string
	WarehouseCode string
	WarehouseName string
	OnHand        decimal.Decimal
	Reserved      decimal.Decimal
	Available     decimal.Decimal // = OnHand - Reserved
	UnitCost      decimal.Decimal // weighted average purchase cost
}
