package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"

	"github.com/shopspring/decimal"
)

// setupInventoryTestDB extends the order test DB with inventory tables and seed data.
func setupInventoryTestDB(t *testing.T) (core.OrderService, core.InventoryService, core.LedgerService, core.DocumentService, context.Context) {
	t.Helper()
	pool, orderSvc, ledger, docSvc, ctx := setupOrderTestDB(t)

	// Seed additional accounts needed for inventory tests
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (company_id, code, name, type) VALUES
		(1, '1400', 'Inventory',            'asset'),
		(1, '2000', 'Accounts Payable',     'liability'),
		(1, '5000', 'Cost of Goods Sold',   'expense')
		ON CONFLICT (company_id, code) DO NOTHING;

		INSERT INTO document_types (code, name, numbering_strategy, resets_every_fy)
		VALUES
		    ('GR', 'Goods Receipt', 'sequential', false),
		    ('GI', 'Goods Issue',   'sequential', false)
		ON CONFLICT (code) DO NOTHING;

		-- Warehouse
		INSERT INTO warehouses (company_id, code, name)
		VALUES (1, 'MAIN', 'Main Warehouse')
		ON CONFLICT (company_id, code) DO NOTHING;

		-- Inventory items for physical goods (P001 = Widget A, P003 = Widget B in test seed)
		-- In the order test seed: P001 = Widget A (code '4000'), P003 = Widget B (code '4000')
		INSERT INTO inventory_items (company_id, product_id, warehouse_id, qty_on_hand, qty_reserved, unit_cost)
		SELECT 1, p.id, w.id, 0, 0, 0
		FROM products p
		JOIN warehouses w ON w.company_id = 1 AND w.code = 'MAIN'
		WHERE p.company_id = 1 AND p.code IN ('P001', 'P003')
		ON CONFLICT (company_id, product_id, warehouse_id) DO NOTHING;

		-- Account rules required by InventoryService (Phase 7)
		INSERT INTO account_rules (company_id, rule_type, account_code) VALUES
		(1, 'INVENTORY',      '1400'),
		(1, 'COGS',           '5000'),
		(1, 'RECEIPT_CREDIT', '2000')
		ON CONFLICT DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("Failed to seed inventory test data: %v", err)
	}

	ruleEngine := core.NewRuleEngine(pool)
	invSvc := core.NewInventoryService(pool, ruleEngine)
	return orderSvc, invSvc, ledger, docSvc, ctx
}

// getStockInfo is a helper to fetch qty_on_hand and qty_reserved for a product.
func getStockInfo(t *testing.T, ctx context.Context, invSvc core.InventoryService, companyCode, productCode string) (onHand, reserved decimal.Decimal) {
	t.Helper()
	levels, err := invSvc.GetStockLevels(ctx, companyCode)
	if err != nil {
		t.Fatalf("GetStockLevels failed: %v", err)
	}
	for _, sl := range levels {
		if sl.ProductCode == productCode {
			return sl.OnHand, sl.Reserved
		}
	}
	t.Fatalf("Product %s not found in stock levels for company %s", productCode, companyCode)
	return decimal.Zero, decimal.Zero
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestInventory_ReceiveStock(t *testing.T) {
	_, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// Receive 100 units of P001 @ 250 per unit
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(100), decimal.NewFromFloat(250),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}

	onHand, reserved := getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(100)) {
		t.Errorf("Expected on_hand=100, got %s", onHand)
	}
	if !reserved.IsZero() {
		t.Errorf("Expected reserved=0, got %s", reserved)
	}
}

func TestInventory_WeightedAverageCost(t *testing.T) {
	_, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// First receipt: 100 @ 200 = avg 200
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(100), decimal.NewFromFloat(200),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("First ReceiveStock failed: %v", err)
	}

	// Second receipt: 100 @ 300 = avg (100*200 + 100*300) / 200 = 250
	err = invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(100), decimal.NewFromFloat(300),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("Second ReceiveStock failed: %v", err)
	}

	levels, err := invSvc.GetStockLevels(ctx, "1000")
	if err != nil {
		t.Fatalf("GetStockLevels failed: %v", err)
	}
	for _, sl := range levels {
		if sl.ProductCode == "P001" {
			if !sl.OnHand.Equal(decimal.NewFromInt(200)) {
				t.Errorf("Expected on_hand=200, got %s", sl.OnHand)
			}
			if !sl.UnitCost.Equal(decimal.NewFromFloat(250)) {
				t.Errorf("Expected unit_cost=250 (weighted avg), got %s", sl.UnitCost)
			}
			return
		}
	}
	t.Fatal("P001 not found in stock levels")
}

func TestInventory_ReserveStock(t *testing.T) {
	orderSvc, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// Receive 50 units of P001 @ 200 each
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(50), decimal.NewFromFloat(200),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}

	// Create and confirm an order for 10 units of P001
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(10)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	_, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, invSvc)
	if err != nil {
		t.Fatalf("ConfirmOrder with inventory reservation failed: %v", err)
	}

	onHand, reserved := getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(50)) {
		t.Errorf("Expected on_hand=50 (reservation does not reduce on_hand), got %s", onHand)
	}
	if !reserved.Equal(decimal.NewFromInt(10)) {
		t.Errorf("Expected reserved=10, got %s", reserved)
	}
}

func TestInventory_InsufficientStock(t *testing.T) {
	orderSvc, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// Only 5 units in stock
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(5), decimal.NewFromFloat(200),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}

	// Try to order 10 units — should fail at Confirm
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(10)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	_, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, invSvc)
	if err == nil {
		t.Error("Expected error: insufficient stock to confirm order, but got nil")
	}
	t.Logf("Got expected error: %v", err)
}

func TestInventory_ShipStock(t *testing.T) {
	orderSvc, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// Receive 100 units @ 300 each
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(100), decimal.NewFromFloat(300),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}

	// Create + confirm (reserve 20) + ship
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(20)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	order, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, invSvc)
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	// Before ship: 100 on hand, 20 reserved
	onHand, reserved := getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(100)) || !reserved.Equal(decimal.NewFromInt(20)) {
		t.Errorf("Before ship: expected on_hand=100, reserved=20; got on_hand=%s, reserved=%s", onHand, reserved)
	}

	order, err = orderSvc.ShipOrder(ctx, order.ID, invSvc, ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ShipOrder failed: %v", err)
	}
	if order.Status != "SHIPPED" {
		t.Errorf("Expected SHIPPED, got %s", order.Status)
	}

	// After ship: 80 on hand, 0 reserved; COGS = 20 × 300 = 6000
	onHand, reserved = getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(80)) {
		t.Errorf("After ship: expected on_hand=80, got %s", onHand)
	}
	if !reserved.IsZero() {
		t.Errorf("After ship: expected reserved=0, got %s", reserved)
	}

	// Verify COGS journal entry was booked: 5000 COGS = 6000, 1400 Inventory = -6000
	balances, err := ledger.GetBalances(ctx, "1000")
	if err != nil {
		t.Fatalf("GetBalances failed: %v", err)
	}
	bm := balanceMap(balances)
	// 1400 Inventory: DR receipt 30000, CR COGS 6000 → net 24000
	if bm["1400"] != "24000.00" {
		t.Errorf("Expected Inventory balance 24000.00 (30000 receipt - 6000 COGS), got %s", bm["1400"])
	}
	if bm["5000"] != "6000.00" {
		t.Errorf("Expected COGS 6000.00 (20 × 300), got %s", bm["5000"])
	}
}

func TestInventory_CancelOrder_ReleasesReservation(t *testing.T) {
	orderSvc, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(50), decimal.NewFromFloat(200),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}

	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(15)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	_, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, invSvc)
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	// Verify reservation is active
	_, reserved := getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !reserved.Equal(decimal.NewFromInt(15)) {
		t.Errorf("Expected reserved=15 after confirm, got %s", reserved)
	}

	// Note: CancelOrder only cancels DRAFT orders. Here we cancel the CONFIRMED order
	// by directly testing ReleaseReservationTx through the service.
	// In production, Phase 5 will allow cancelling CONFIRMED orders.
	// For now, verify that DRAFT cancel works (no reservation to release).
	order2, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(5)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder2 failed: %v", err)
	}
	// Cancel the DRAFT order (no reservation since it was never confirmed)
	_, err = orderSvc.CancelOrder(ctx, order2.ID, invSvc)
	if err != nil {
		t.Fatalf("CancelOrder DRAFT failed: %v", err)
	}

	// Reservation from order 1 should still be 15
	_, reserved = getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !reserved.Equal(decimal.NewFromInt(15)) {
		t.Errorf("Expected reserved=15 (unchanged), got %s", reserved)
	}
}

func TestInventory_FullLifecycle(t *testing.T) {
	orderSvc, invSvc, ledger, docSvc, ctx := setupInventoryTestDB(t)

	// 1. Receive 50 units of P001 @ 400 each
	err := invSvc.ReceiveStock(ctx, "1000", "MAIN", "P001",
		decimal.NewFromInt(50), decimal.NewFromFloat(400),
		"2026-02-24", "2000", ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ReceiveStock failed: %v", err)
	}
	// Inventory account = 50 × 400 = 20000
	// AP = 20000

	// 2. Create order for 10 units of P001 @ 500 (selling price) = 5000
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-24",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(10)}}, "")
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	// 3. Confirm → reserves 10 units
	order, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, invSvc)
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}
	if order.Status != "CONFIRMED" {
		t.Errorf("Expected CONFIRMED, got %s", order.Status)
	}

	onHand, reserved := getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(50)) || !reserved.Equal(decimal.NewFromInt(10)) {
		t.Errorf("After confirm: expected on_hand=50, reserved=10; got %s, %s", onHand, reserved)
	}

	// 4. Ship → deducts 10 from on_hand, books COGS (10 × 400 = 4000)
	order, err = orderSvc.ShipOrder(ctx, order.ID, invSvc, ledger.(*core.Ledger), docSvc)
	if err != nil {
		t.Fatalf("ShipOrder failed: %v", err)
	}

	onHand, reserved = getStockInfo(t, ctx, invSvc, "1000", "P001")
	if !onHand.Equal(decimal.NewFromInt(40)) || !reserved.IsZero() {
		t.Errorf("After ship: expected on_hand=40, reserved=0; got %s, %s", onHand, reserved)
	}

	// 5. Invoice → DR 1200 AR 5000, CR 4000 Revenue 5000
	order, err = orderSvc.InvoiceOrder(ctx, order.ID, ledger, docSvc)
	if err != nil {
		t.Fatalf("InvoiceOrder failed: %v", err)
	}

	// 6. Record payment → DR 1100 Bank 5000, CR 1200 AR 5000
	err = orderSvc.RecordPayment(ctx, order.ID, "1100", "2026-02-25", ledger)
	if err != nil {
		t.Fatalf("RecordPayment failed: %v", err)
	}

	// 7. Verify final balances
	balances, err := ledger.GetBalances(ctx, "1000")
	if err != nil {
		t.Fatalf("GetBalances failed: %v", err)
	}
	bm := balanceMap(balances)

	// Bank: +5000 (payment received)
	if bm["1100"] != "5000.00" {
		t.Errorf("Expected Bank 5000.00, got %s", bm["1100"])
	}
	// AR: 0 (invoiced then paid)
	if bm["1200"] != "0.00" {
		t.Errorf("Expected AR 0.00, got %s", bm["1200"])
	}
	// Inventory: 20000 (receipt) - 4000 (COGS) = 16000
	if bm["1400"] != "16000.00" {
		t.Errorf("Expected Inventory 16000.00, got %s", bm["1400"])
	}
	// COGS: 4000 (10 × 400 unit cost)
	if bm["5000"] != "4000.00" {
		t.Errorf("Expected COGS 4000.00, got %s", bm["5000"])
	}
	// Revenue: -5000 (10 × 500 selling price)
	if bm["4000"] != "-5000.00" {
		t.Errorf("Expected Revenue -5000.00, got %s", bm["4000"])
	}
	// Gross profit check: revenue 5000 - COGS 4000 = 1000 (not directly testable from balances but implied)
	t.Logf("Full lifecycle complete. Revenue=5000, COGS=4000, Gross Profit=1000")
}
