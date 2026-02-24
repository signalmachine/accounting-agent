package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// setupOrderTestDB extends the base ledger test DB with the additional accounts,
// document types, customers, and products needed for order tests.
func setupOrderTestDB(t *testing.T) (*pgxpool.Pool, core.OrderService, core.LedgerService, core.DocumentService, context.Context) {
	t.Helper()
	pool := setupTestDB(t) // truncates DB and seeds company 1000 + accounts 1000, 4000 + doc types JE/SI/PI

	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (company_id, code, name, type) VALUES
		(1, '1100', 'Bank Account',         'asset'),
		(1, '1200', 'Accounts Receivable',  'asset'),
		(1, '4100', 'Service Revenue',      'revenue')
		ON CONFLICT (company_id, code) DO NOTHING;

		INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy)
		VALUES ('SO', 'Sales Order', false, false, true, false, 'global', true)
		ON CONFLICT (code) DO NOTHING;

		INSERT INTO customers (company_id, code, name, email, phone, address, credit_limit, payment_terms_days) VALUES
		(1, 'C001', 'Acme Corp',       'billing@acme.com', '+91-9800000001', 'Bengaluru', 100000, 30),
		(1, 'C002', 'Beta Industries', 'billing@beta.in',  '+91-9800000002', 'Mumbai',     50000, 45)
		ON CONFLICT (company_id, code) DO NOTHING;

		INSERT INTO products (company_id, code, name, description, unit_price, unit, revenue_account_code) VALUES
		(1, 'P001', 'Widget A',            'Standard widget',   500.00,  'unit', '4000'),
		(1, 'P002', 'Consulting Services', 'Advisory services', 5000.00, 'hour', '4100'),
		(1, 'P003', 'Widget B',            'Premium widget',    1200.00, 'unit', '4000')
		ON CONFLICT (company_id, code) DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("Failed to seed order test data: %v", err)
	}

	docSvc := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docSvc)
	orderSvc := core.NewOrderService(pool)

	return pool, orderSvc, ledger, docSvc, ctx
}

func TestOrderService_FullSalesCycle(t *testing.T) {
	pool, orderSvc, ledger, docSvc, ctx := setupOrderTestDB(t)
	defer pool.Close()

	// 1. Create a draft order: 10 × Widget A @ 500 = 5000
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{
			{ProductCode: "P001", Quantity: decimal.NewFromInt(10)},
		}, "Test order",
	)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if order.Status != "DRAFT" {
		t.Errorf("Expected DRAFT, got %s", order.Status)
	}
	if order.OrderNumber != "" {
		t.Errorf("DRAFT order should have no order number, got %q", order.OrderNumber)
	}
	if !order.TotalTransaction.Equal(decimal.NewFromFloat(5000)) {
		t.Errorf("Expected total 5000.00, got %s", order.TotalTransaction)
	}

	// 2. Confirm → assigns gapless SO number
	order, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, nil)
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}
	if order.Status != "CONFIRMED" {
		t.Errorf("Expected CONFIRMED, got %s", order.Status)
	}
	if order.OrderNumber == "" {
		t.Error("CONFIRMED order must have an order number")
	}
	if order.ConfirmedAt == nil {
		t.Error("CONFIRMED order must have confirmed_at timestamp")
	}
	t.Logf("Order number: %s", order.OrderNumber)

	// 3. Ship
	order, err = orderSvc.ShipOrder(ctx, order.ID, nil, nil, nil)
	if err != nil {
		t.Fatalf("ShipOrder failed: %v", err)
	}
	if order.Status != "SHIPPED" {
		t.Errorf("Expected SHIPPED, got %s", order.Status)
	}

	// 4. Invoice → creates SI document + journal entry (DR 1200 AR, CR 4000 Revenue)
	order, err = orderSvc.InvoiceOrder(ctx, order.ID, ledger, docSvc)
	if err != nil {
		t.Fatalf("InvoiceOrder failed: %v", err)
	}
	if order.Status != "INVOICED" {
		t.Errorf("Expected INVOICED, got %s", order.Status)
	}

	balances, err := ledger.GetBalances(ctx, "1000")
	if err != nil {
		t.Fatalf("GetBalances failed: %v", err)
	}
	bm := balanceMap(balances)
	if bm["1200"] != "5000.00" {
		t.Errorf("After invoicing: expected AR 5000.00, got %s", bm["1200"])
	}
	if bm["4000"] != "-5000.00" {
		t.Errorf("After invoicing: expected Revenue -5000.00, got %s", bm["4000"])
	}

	// 5. Record payment → DR Bank 1100, CR AR 1200
	err = orderSvc.RecordPayment(ctx, order.ID, "1100", "2026-02-15", ledger)
	if err != nil {
		t.Fatalf("RecordPayment failed: %v", err)
	}

	order, err = orderSvc.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("GetOrder after payment failed: %v", err)
	}
	if order.Status != "PAID" {
		t.Errorf("Expected PAID, got %s", order.Status)
	}

	balances, err = ledger.GetBalances(ctx, "1000")
	if err != nil {
		t.Fatalf("GetBalances after payment failed: %v", err)
	}
	bm = balanceMap(balances)
	if bm["1200"] != "0.00" {
		t.Errorf("After payment: expected AR 0.00, got %s", bm["1200"])
	}
	if bm["1100"] != "5000.00" {
		t.Errorf("After payment: expected Bank 5000.00, got %s", bm["1100"])
	}
}

func TestOrderService_MultiProductRevenue(t *testing.T) {
	pool, orderSvc, ledger, docSvc, ctx := setupOrderTestDB(t)
	defer pool.Close()

	// Two products with different revenue accounts:
	//   P001: 2 × 500 = 1000  → account 4000
	//   P002: 3 × 5000 = 15000 → account 4100
	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{
			{ProductCode: "P001", Quantity: decimal.NewFromInt(2)},
			{ProductCode: "P002", Quantity: decimal.NewFromInt(3)},
		}, "",
	)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if !order.TotalTransaction.Equal(decimal.NewFromFloat(16000)) {
		t.Errorf("Expected total 16000.00, got %s", order.TotalTransaction)
	}

	order, _ = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, nil)
	order, _ = orderSvc.ShipOrder(ctx, order.ID, nil, nil, nil)
	_, err = orderSvc.InvoiceOrder(ctx, order.ID, ledger, docSvc)
	if err != nil {
		t.Fatalf("InvoiceOrder failed: %v", err)
	}

	balances, err := ledger.GetBalances(ctx, "1000")
	if err != nil {
		t.Fatalf("GetBalances failed: %v", err)
	}
	bm := balanceMap(balances)

	if bm["1200"] != "16000.00" {
		t.Errorf("Expected AR 16000.00, got %s", bm["1200"])
	}
	if bm["4000"] != "-1000.00" {
		t.Errorf("Expected Sales Revenue -1000.00, got %s", bm["4000"])
	}
	if bm["4100"] != "-15000.00" {
		t.Errorf("Expected Service Revenue -15000.00, got %s", bm["4100"])
	}
}

func TestOrderService_StateTransitionGuards(t *testing.T) {
	pool, orderSvc, _, docSvc, ctx := setupOrderTestDB(t)
	defer pool.Close()

	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(1)}}, "",
	)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	// Cannot ship a DRAFT order
	if _, err = orderSvc.ShipOrder(ctx, order.ID, nil, nil, nil); err == nil {
		t.Error("Expected error shipping a DRAFT order")
	}

	// Confirm order
	order, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, nil)
	if err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	// Cannot cancel a CONFIRMED order
	if _, err = orderSvc.CancelOrder(ctx, order.ID, nil); err == nil {
		t.Error("Expected error cancelling a CONFIRMED order")
	}

	// Cannot confirm twice
	if _, err = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, nil); err == nil {
		t.Error("Expected error confirming an already-CONFIRMED order")
	}
}

func TestOrderService_CancelDraftOrder(t *testing.T) {
	pool, orderSvc, _, _, ctx := setupOrderTestDB(t)
	defer pool.Close()

	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(5)}}, "to be cancelled",
	)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	order, err = orderSvc.CancelOrder(ctx, order.ID, nil)
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
	if order.Status != "CANCELLED" {
		t.Errorf("Expected CANCELLED, got %s", order.Status)
	}
}

func TestOrderService_CrossCompanyIsolation(t *testing.T) {
	pool, orderSvc, _, _, ctx := setupOrderTestDB(t)
	defer pool.Close()

	// Seed a second company with no customers
	_, err := pool.Exec(ctx, `
		INSERT INTO companies (id, company_code, name, base_currency)
		VALUES (2, '2000', 'Company Two', 'USD')
		ON CONFLICT DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("Failed to seed second company: %v", err)
	}

	// Customer C001 belongs to company 1000; company 2000 should not find it
	_, err = orderSvc.CreateOrder(ctx, "2000", "C001", "USD", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(1)}}, "",
	)
	if err == nil {
		t.Error("Expected error: customer C001 is not accessible from company 2000")
	}
}

func TestOrderService_GetOrders(t *testing.T) {
	pool, orderSvc, _, docSvc, ctx := setupOrderTestDB(t)
	defer pool.Close()

	// Create two orders for different customers
	_, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(1)}}, "order 1",
	)
	if err != nil {
		t.Fatalf("CreateOrder 1 failed: %v", err)
	}

	order2, err := orderSvc.CreateOrder(ctx, "1000", "C002", "INR", decimal.NewFromFloat(1.0), "2026-02-02",
		[]core.OrderLineInput{{ProductCode: "P002", Quantity: decimal.NewFromInt(2)}}, "order 2",
	)
	if err != nil {
		t.Fatalf("CreateOrder 2 failed: %v", err)
	}

	// Confirm order 2
	if _, err = orderSvc.ConfirmOrder(ctx, order2.ID, docSvc, nil); err != nil {
		t.Fatalf("ConfirmOrder failed: %v", err)
	}

	// All orders
	orders, err := orderSvc.GetOrders(ctx, "1000", nil)
	if err != nil {
		t.Fatalf("GetOrders failed: %v", err)
	}
	if len(orders) != 2 {
		t.Errorf("Expected 2 orders, got %d", len(orders))
	}

	// Filter by CONFIRMED status
	status := "CONFIRMED"
	confirmed, err := orderSvc.GetOrders(ctx, "1000", &status)
	if err != nil {
		t.Fatalf("GetOrders CONFIRMED failed: %v", err)
	}
	if len(confirmed) != 1 {
		t.Errorf("Expected 1 CONFIRMED order, got %d", len(confirmed))
	}
}

// balanceMap converts []AccountBalance to a map keyed by account code for easy assertions.
func balanceMap(balances []core.AccountBalance) map[string]string {
	m := make(map[string]string)
	for _, b := range balances {
		m[b.Code] = b.Balance.StringFixed(2)
	}
	return m
}

func TestOrderService_InvoiceIdempotency(t *testing.T) {
	pool, orderSvc, ledger, docSvc, ctx := setupOrderTestDB(t)
	defer pool.Close()

	order, err := orderSvc.CreateOrder(ctx, "1000", "C001", "INR", decimal.NewFromFloat(1.0), "2026-02-01",
		[]core.OrderLineInput{{ProductCode: "P001", Quantity: decimal.NewFromInt(1)}}, "",
	)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	order, _ = orderSvc.ConfirmOrder(ctx, order.ID, docSvc, nil)
	order, _ = orderSvc.ShipOrder(ctx, order.ID, nil, nil, nil)

	if _, err = orderSvc.InvoiceOrder(ctx, order.ID, ledger, docSvc); err != nil {
		t.Fatalf("First InvoiceOrder failed: %v", err)
	}

	// Second attempt must fail — order is now INVOICED, not SHIPPED
	if _, err = orderSvc.InvoiceOrder(ctx, order.ID, ledger, docSvc); err == nil {
		t.Error("Expected error on second InvoiceOrder call (order already INVOICED)")
	}
}
