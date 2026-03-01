package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// InventoryService manages warehouse stock levels, reservations, and goods movements.
// It integrates with the Ledger to book accounting entries for receipts and shipments.
type InventoryService interface {
	// Standalone operations (manage their own transactions).
	GetWarehouses(ctx context.Context, companyCode string) ([]Warehouse, error)
	GetDefaultWarehouse(ctx context.Context, companyCode string) (*Warehouse, error)
	GetStockLevels(ctx context.Context, companyCode string) ([]StockLevel, error)
	// ReceiveStock records a goods receipt: increases qty_on_hand and books DR Inventory / CR creditAccountCode.
	// poLineID, if non-nil, links the created inventory_movement to a purchase order line.
	ReceiveStock(ctx context.Context, companyCode, warehouseCode, productCode string,
		qty, unitCost decimal.Decimal, movementDate, creditAccountCode string,
		poLineID *int, ledger *Ledger, docService DocumentService) error

	// TX-scoped operations: work within a caller-provided transaction.
	// Used by OrderService to keep inventory changes atomic with order state transitions.

	// ReserveStockTx soft-locks stock when an order is confirmed.
	// Products without an inventory_item record are silently skipped (service items).
	ReserveStockTx(ctx context.Context, tx pgx.Tx, companyID, orderID int, lines []SalesOrderLine) error
	// ReleaseReservationTx releases soft-locked stock when an order is cancelled.
	ReleaseReservationTx(ctx context.Context, tx pgx.Tx, orderID int) error
	// ShipStockTx deducts physical stock and books COGS when an order is shipped.
	// The COGS journal entry is committed atomically within the provided TX via Ledger.CommitInTx.
	ShipStockTx(ctx context.Context, tx pgx.Tx, companyID, orderID int, lines []SalesOrderLine,
		ledger *Ledger, docService DocumentService) error
}

type inventoryService struct {
	pool       *pgxpool.Pool
	ruleEngine RuleEngine
}

func NewInventoryService(pool *pgxpool.Pool, ruleEngine RuleEngine) InventoryService {
	return &inventoryService{pool: pool, ruleEngine: ruleEngine}
}

// ── Standalone operations ─────────────────────────────────────────────────────

func (s *inventoryService) GetWarehouses(ctx context.Context, companyCode string) ([]Warehouse, error) {
	var companyID int
	if err := s.pool.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", companyCode).Scan(&companyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("company code %s not found", companyCode)
		}
		return nil, fmt.Errorf("failed to resolve company: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, code, name, is_active, created_at
		FROM warehouses
		WHERE company_id = $1 AND is_active = true
		ORDER BY code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query warehouses: %w", err)
	}
	defer rows.Close()

	var warehouses []Warehouse
	for rows.Next() {
		var w Warehouse
		if err := rows.Scan(&w.ID, &w.CompanyID, &w.Code, &w.Name, &w.IsActive, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan warehouse: %w", err)
		}
		warehouses = append(warehouses, w)
	}
	return warehouses, nil
}

func (s *inventoryService) GetDefaultWarehouse(ctx context.Context, companyCode string) (*Warehouse, error) {
	var companyID int
	if err := s.pool.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", companyCode).Scan(&companyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("company code %s not found", companyCode)
		}
		return nil, fmt.Errorf("failed to resolve company: %w", err)
	}

	var w Warehouse
	err := s.pool.QueryRow(ctx, `
		SELECT id, company_id, code, name, is_active, created_at
		FROM warehouses
		WHERE company_id = $1 AND is_active = true
		ORDER BY id
		LIMIT 1
	`, companyID).Scan(&w.ID, &w.CompanyID, &w.Code, &w.Name, &w.IsActive, &w.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no active warehouse found for company %s", companyCode)
		}
		return nil, fmt.Errorf("failed to fetch default warehouse: %w", err)
	}
	return &w, nil
}

func (s *inventoryService) GetStockLevels(ctx context.Context, companyCode string) ([]StockLevel, error) {
	var companyID int
	if err := s.pool.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", companyCode).Scan(&companyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("company code %s not found", companyCode)
		}
		return nil, fmt.Errorf("failed to resolve company: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.code, p.name, w.code, w.name,
		       ii.qty_on_hand, ii.qty_reserved,
		       ii.qty_on_hand - ii.qty_reserved AS qty_available,
		       ii.unit_cost
		FROM inventory_items ii
		JOIN products p   ON p.id = ii.product_id
		JOIN warehouses w ON w.id = ii.warehouse_id
		WHERE ii.company_id = $1
		ORDER BY p.code, w.code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query stock levels: %w", err)
	}
	defer rows.Close()

	var levels []StockLevel
	for rows.Next() {
		var sl StockLevel
		if err := rows.Scan(
			&sl.ProductCode, &sl.ProductName,
			&sl.WarehouseCode, &sl.WarehouseName,
			&sl.OnHand, &sl.Reserved, &sl.Available, &sl.UnitCost,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stock level: %w", err)
		}
		levels = append(levels, sl)
	}
	return levels, nil
}

// ReceiveStock records a goods receipt for a product into a warehouse.
// It updates qty_on_hand using weighted average cost and books the accounting entry:
//
//	DR 1400 Inventory / CR creditAccountCode (default 2000 AP)
//
// poLineID, if non-nil, links the created inventory_movement to a purchase order line.
func (s *inventoryService) ReceiveStock(ctx context.Context, companyCode, warehouseCode, productCode string,
	qty, unitCost decimal.Decimal, movementDate, creditAccountCode string,
	poLineID *int, ledger *Ledger, docService DocumentService) error {

	if qty.IsNegative() || qty.IsZero() {
		return fmt.Errorf("receive quantity must be positive, got %s", qty)
	}
	if unitCost.IsNegative() {
		return fmt.Errorf("unit cost cannot be negative, got %s", unitCost)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Resolve company
	var companyID int
	var baseCurrency string
	if err := tx.QueryRow(ctx, "SELECT id, base_currency FROM companies WHERE company_code = $1", companyCode).Scan(&companyID, &baseCurrency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("company code %s not found", companyCode)
		}
		return fmt.Errorf("failed to resolve company: %w", err)
	}

	// Resolve inventory account via rule engine
	inventoryAccount, err := s.ruleEngine.ResolveAccount(ctx, companyID, "INVENTORY")
	if err != nil {
		return fmt.Errorf("failed to resolve INVENTORY account: %w", err)
	}

	// Resolve credit account: use caller-supplied value or fall back to rule engine
	if creditAccountCode == "" {
		creditAccountCode, err = s.ruleEngine.ResolveAccount(ctx, companyID, "RECEIPT_CREDIT")
		if err != nil {
			return fmt.Errorf("failed to resolve RECEIPT_CREDIT account: %w", err)
		}
	}

	// Resolve warehouse
	var warehouseID int
	if err := tx.QueryRow(ctx,
		"SELECT id FROM warehouses WHERE company_id = $1 AND code = $2 AND is_active = true",
		companyID, warehouseCode,
	).Scan(&warehouseID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("warehouse %s not found for company %s", warehouseCode, companyCode)
		}
		return fmt.Errorf("failed to resolve warehouse: %w", err)
	}

	// Resolve product
	var productID int
	if err := tx.QueryRow(ctx,
		"SELECT id FROM products WHERE company_id = $1 AND code = $2 AND is_active = true",
		companyID, productCode,
	).Scan(&productID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("product %s not found for company %s", productCode, companyCode)
		}
		return fmt.Errorf("failed to resolve product: %w", err)
	}

	// Lock inventory_item row (create if it doesn't exist yet)
	var itemID int
	var oldQty, oldCost decimal.Decimal
	err = tx.QueryRow(ctx, `
		INSERT INTO inventory_items (company_id, product_id, warehouse_id, qty_on_hand, qty_reserved, unit_cost)
		VALUES ($1, $2, $3, 0, 0, 0)
		ON CONFLICT (company_id, product_id, warehouse_id) DO UPDATE SET updated_at = NOW()
		RETURNING id, qty_on_hand, unit_cost
	`, companyID, productID, warehouseID).Scan(&itemID, &oldQty, &oldCost)
	if err != nil {
		return fmt.Errorf("failed to upsert inventory item: %w", err)
	}

	// Lock the row for update
	err = tx.QueryRow(ctx,
		"SELECT id, qty_on_hand, unit_cost FROM inventory_items WHERE id = $1 FOR UPDATE",
		itemID,
	).Scan(&itemID, &oldQty, &oldCost)
	if err != nil {
		return fmt.Errorf("failed to lock inventory item: %w", err)
	}

	// Weighted average cost: new_cost = (old_qty * old_cost + qty * unitCost) / (old_qty + qty)
	newQty := oldQty.Add(qty)
	var newCost decimal.Decimal
	if newQty.IsZero() {
		newCost = unitCost
	} else {
		newCost = oldQty.Mul(oldCost).Add(qty.Mul(unitCost)).Div(newQty)
	}

	// Update inventory_item
	_, err = tx.Exec(ctx, `
		UPDATE inventory_items
		SET qty_on_hand = $1, unit_cost = $2, updated_at = NOW()
		WHERE id = $3
	`, newQty, newCost, itemID)
	if err != nil {
		return fmt.Errorf("failed to update inventory item: %w", err)
	}

	// Insert movement record
	totalCost := qty.Mul(unitCost)
	parsedDate, _ := time.Parse("2006-01-02", movementDate)
	if parsedDate.IsZero() {
		parsedDate = time.Now()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_movements (company_id, inventory_item_id, movement_type, quantity, unit_cost, total_cost, movement_date, notes, po_line_id)
		VALUES ($1, $2, 'RECEIPT', $3, $4, $5, $6, $7, $8)
	`, companyID, itemID, qty, unitCost, totalCost, parsedDate.Format("2006-01-02"),
		fmt.Sprintf("Goods receipt: %s × %s units @ %s", productCode, qty.String(), unitCost.String()),
		poLineID,
	)
	if err != nil {
		return fmt.Errorf("failed to insert inventory movement: %w", err)
	}

	// Book accounting entry inside the same tx: DR Inventory / CR creditAccount.
	// Using CommitInTx ensures inventory write and journal entry commit atomically.
	proposal := Proposal{
		DocumentTypeCode:    "GR",
		CompanyCode:         companyCode,
		IdempotencyKey:      fmt.Sprintf("goods-receipt-%s-%s-%s", companyCode, productCode, movementDate),
		TransactionCurrency: baseCurrency,
		ExchangeRate:        "1",
		Summary:             fmt.Sprintf("Goods Receipt: %s units of %s @ %s", qty.String(), productCode, unitCost.String()),
		PostingDate:         movementDate,
		DocumentDate:        movementDate,
		Confidence:          1.0,
		Reasoning:           fmt.Sprintf("Inventory receipt for product %s, %s units at unit cost %s.", productCode, qty.String(), unitCost.String()),
		Lines: []ProposalLine{
			{AccountCode: inventoryAccount, IsDebit: true, Amount: totalCost.String()},
			{AccountCode: creditAccountCode, IsDebit: false, Amount: totalCost.String()},
		},
	}

	if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
		return fmt.Errorf("failed to book goods receipt journal entry: %w", err)
	}

	// Single commit: inventory write + journal entry land together or not at all.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit goods receipt: %w", err)
	}

	return nil
}

// ── TX-scoped operations ──────────────────────────────────────────────────────

// ReserveStockTx soft-locks stock for each physical-goods order line within the caller's TX.
// Service products (no inventory_item record) are silently skipped.
func (s *inventoryService) ReserveStockTx(ctx context.Context, tx pgx.Tx, companyID, orderID int, lines []SalesOrderLine) error {
	for _, line := range lines {
		// Look for inventory_item for this product in the company's default warehouse.
		// We join through warehouses to find the first active one.
		var itemID int
		var onHand, reserved decimal.Decimal
		err := tx.QueryRow(ctx, `
			SELECT ii.id, ii.qty_on_hand, ii.qty_reserved
			FROM inventory_items ii
			JOIN warehouses w ON w.id = ii.warehouse_id
			WHERE ii.company_id = $1
			  AND ii.product_id = $2
			  AND w.is_active = true
			ORDER BY w.id
			LIMIT 1
			FOR UPDATE OF ii
		`, companyID, line.ProductID).Scan(&itemID, &onHand, &reserved)
		if errors.Is(err, pgx.ErrNoRows) {
			// No inventory_item = service product, skip
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to lock inventory item for product %s: %w", line.ProductCode, err)
		}

		available := onHand.Sub(reserved)
		if available.LessThan(line.Quantity) {
			return fmt.Errorf("insufficient stock for product %s: available %s, required %s",
				line.ProductCode, available.StringFixed(4), line.Quantity.StringFixed(4))
		}

		// Increase reservation
		_, err = tx.Exec(ctx, `
			UPDATE inventory_items SET qty_reserved = qty_reserved + $1, updated_at = NOW()
			WHERE id = $2
		`, line.Quantity, itemID)
		if err != nil {
			return fmt.Errorf("failed to reserve stock for product %s: %w", line.ProductCode, err)
		}

		// Append movement record
		_, err = tx.Exec(ctx, `
			INSERT INTO inventory_movements (company_id, inventory_item_id, movement_type, quantity, unit_cost, total_cost, order_id, movement_date, notes)
			VALUES ($1, $2, 'RESERVATION', $3, 0, 0, $4, CURRENT_DATE, $5)
		`, companyID, itemID, line.Quantity, orderID,
			fmt.Sprintf("Stock reserved for order ID %d, product %s", orderID, line.ProductCode),
		)
		if err != nil {
			return fmt.Errorf("failed to insert reservation movement for product %s: %w", line.ProductCode, err)
		}
	}
	return nil
}

// ReleaseReservationTx reverses all RESERVATION movements for an order within the caller's TX.
// Called when a CONFIRMED order is cancelled.
func (s *inventoryService) ReleaseReservationTx(ctx context.Context, tx pgx.Tx, orderID int) error {
	// Find all reservation movements for this order
	rows, err := tx.Query(ctx, `
		SELECT im.inventory_item_id, im.quantity, im.company_id
		FROM inventory_movements im
		WHERE im.order_id = $1 AND im.movement_type = 'RESERVATION'
	`, orderID)
	if err != nil {
		return fmt.Errorf("failed to fetch reservation movements for order %d: %w", orderID, err)
	}

	type reservationRow struct {
		itemID    int
		quantity  decimal.Decimal
		companyID int
	}
	var reservations []reservationRow
	for rows.Next() {
		var r reservationRow
		if err := rows.Scan(&r.itemID, &r.quantity, &r.companyID); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan reservation row: %w", err)
		}
		reservations = append(reservations, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating reservation rows: %w", err)
	}

	for _, r := range reservations {
		// Lock and decrease reservation
		_, err = tx.Exec(ctx, `
			UPDATE inventory_items SET qty_reserved = qty_reserved - $1, updated_at = NOW()
			WHERE id = $2
		`, r.quantity, r.itemID)
		if err != nil {
			return fmt.Errorf("failed to release reservation for item %d: %w", r.itemID, err)
		}

		// Append cancellation movement
		_, err = tx.Exec(ctx, `
			INSERT INTO inventory_movements (company_id, inventory_item_id, movement_type, quantity, unit_cost, total_cost, order_id, movement_date, notes)
			VALUES ($1, $2, 'RESERVATION_CANCEL', $3, 0, 0, $4, CURRENT_DATE, $5)
		`, r.companyID, r.itemID, r.quantity.Neg(), orderID,
			fmt.Sprintf("Reservation released for cancelled order ID %d", orderID),
		)
		if err != nil {
			return fmt.Errorf("failed to insert reservation cancel movement for item %d: %w", r.itemID, err)
		}
	}
	return nil
}

// ShipStockTx deducts physical stock and books COGS within the caller's TX.
// The COGS journal entry is committed atomically via Ledger.CommitInTx.
// Service products (no inventory_item) are silently skipped.
func (s *inventoryService) ShipStockTx(ctx context.Context, tx pgx.Tx, companyID, orderID int, lines []SalesOrderLine,
	ledger *Ledger, docService DocumentService) error {

	type shipLine struct {
		itemID    int
		quantity  decimal.Decimal
		unitCost  decimal.Decimal
		lineCOGS  decimal.Decimal
		productCode string
	}
	var toShip []shipLine
	var totalCOGS decimal.Decimal

	for _, line := range lines {
		var itemID int
		var onHand, reserved, unitCost decimal.Decimal
		err := tx.QueryRow(ctx, `
			SELECT ii.id, ii.qty_on_hand, ii.qty_reserved, ii.unit_cost
			FROM inventory_items ii
			JOIN warehouses w ON w.id = ii.warehouse_id
			WHERE ii.company_id = $1
			  AND ii.product_id = $2
			  AND w.is_active = true
			ORDER BY w.id
			LIMIT 1
			FOR UPDATE OF ii
		`, companyID, line.ProductID).Scan(&itemID, &onHand, &reserved, &unitCost)
		if errors.Is(err, pgx.ErrNoRows) {
			// No inventory_item = service product, skip
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to lock inventory item for product %s: %w", line.ProductCode, err)
		}

		if onHand.LessThan(line.Quantity) {
			return fmt.Errorf("insufficient stock for shipment: product %s has %s on hand, need %s",
				line.ProductCode, onHand.StringFixed(4), line.Quantity.StringFixed(4))
		}

		lineCOGS := line.Quantity.Mul(unitCost)
		totalCOGS = totalCOGS.Add(lineCOGS)

		// How much was actually reserved for this order (may be less if ConfirmOrder ran without inventory)
		reservedForOrder := line.Quantity
		if reserved.LessThan(reservedForOrder) {
			reservedForOrder = reserved
		}

		toShip = append(toShip, shipLine{
			itemID:      itemID,
			quantity:    line.Quantity,
			unitCost:    unitCost,
			lineCOGS:    lineCOGS,
			productCode: line.ProductCode,
		})

		// Deduct qty_on_hand and qty_reserved
		_, err = tx.Exec(ctx, `
			UPDATE inventory_items
			SET qty_on_hand  = qty_on_hand  - $1,
			    qty_reserved = GREATEST(qty_reserved - $2, 0),
			    updated_at   = NOW()
			WHERE id = $3
		`, line.Quantity, reservedForOrder, itemID)
		if err != nil {
			return fmt.Errorf("failed to deduct inventory for product %s: %w", line.ProductCode, err)
		}
	}

	// Insert SHIPMENT movement records
	for _, sl := range toShip {
		_, err := tx.Exec(ctx, `
			INSERT INTO inventory_movements (company_id, inventory_item_id, movement_type, quantity, unit_cost, total_cost, order_id, movement_date, notes)
			VALUES ($1, $2, 'SHIPMENT', $3, $4, $5, $6, CURRENT_DATE, $7)
		`, companyID, sl.itemID, sl.quantity.Neg(), sl.unitCost, sl.lineCOGS.Neg(), orderID,
			fmt.Sprintf("Goods shipped for order ID %d, product %s", orderID, sl.productCode),
		)
		if err != nil {
			return fmt.Errorf("failed to insert shipment movement for product %s: %w", sl.productCode, err)
		}
	}

	// Book COGS journal entry atomically within the caller's TX
	if !totalCOGS.IsZero() {
		// Resolve company code and base currency for the proposal
		var companyCode, baseCurrency string
		if err := tx.QueryRow(ctx, "SELECT company_code, base_currency FROM companies WHERE id = $1", companyID).Scan(&companyCode, &baseCurrency); err != nil {
			return fmt.Errorf("failed to resolve company code for COGS entry: %w", err)
		}

		cogsAccount, err := s.ruleEngine.ResolveAccount(ctx, companyID, "COGS")
		if err != nil {
			return fmt.Errorf("failed to resolve COGS account: %w", err)
		}
		inventoryAccount, err := s.ruleEngine.ResolveAccount(ctx, companyID, "INVENTORY")
		if err != nil {
			return fmt.Errorf("failed to resolve INVENTORY account for COGS entry: %w", err)
		}

		today := time.Now().Format("2006-01-02")
		cogsProposal := Proposal{
			DocumentTypeCode:    "GI",
			CompanyCode:         companyCode,
			IdempotencyKey:      fmt.Sprintf("goods-issue-order-%d", orderID),
			TransactionCurrency: baseCurrency,
			ExchangeRate:        "1",
			Summary:             fmt.Sprintf("Cost of Goods Sold — order ID %d", orderID),
			PostingDate:         today,
			DocumentDate:        today,
			Confidence:          1.0,
			Reasoning:           fmt.Sprintf("COGS booked automatically on shipment of order ID %d.", orderID),
			Lines: []ProposalLine{
				{AccountCode: cogsAccount, IsDebit: true, Amount: totalCOGS.StringFixed(2)},
				{AccountCode: inventoryAccount, IsDebit: false, Amount: totalCOGS.StringFixed(2)},
			},
		}

		if err := ledger.CommitInTx(ctx, tx, cogsProposal); err != nil {
			return fmt.Errorf("failed to book COGS journal entry for order %d: %w", orderID, err)
		}
	}

	return nil
}
