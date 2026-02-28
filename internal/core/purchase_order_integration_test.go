package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"accounting-agent/internal/core"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// setupReceivePOTestDB extends the PO test DB with inventory infrastructure.
func setupReceivePOTestDB(t *testing.T) (*pgxpool.Pool, core.PurchaseOrderService, *core.Ledger, core.DocumentService, core.InventoryService, int, context.Context) {
	t.Helper()
	pool, poService, docService, vendorID, ctx := setupPurchaseOrderTestDB(t)

	// Seed accounts, warehouse, GR doc type, and account rules for inventory
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (company_id, code, name, type) VALUES
		(1, '1400', 'Inventory',          'asset'),
		(1, '2000', 'Accounts Payable',   'liability'),
		(1, '5000', 'Cost of Goods Sold', 'expense'),
		(1, '5100', 'Freight Expense',    'expense')
		ON CONFLICT (company_id, code) DO NOTHING;

		INSERT INTO document_types (code, name, numbering_strategy, resets_every_fy)
		VALUES ('GR', 'Goods Receipt', 'sequential', false)
		ON CONFLICT (code) DO NOTHING;

		INSERT INTO warehouses (company_id, code, name)
		VALUES (1, 'MAIN', 'Main Warehouse')
		ON CONFLICT (company_id, code) DO NOTHING;

		INSERT INTO account_rules (company_id, rule_type, account_code) VALUES
		(1, 'INVENTORY',      '1400'),
		(1, 'COGS',           '5000'),
		(1, 'RECEIPT_CREDIT', '2000')
		ON CONFLICT DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("seed receive-PO test data: %v", err)
	}

	ruleEngine := core.NewRuleEngine(pool)
	invSvc := core.NewInventoryService(pool, ruleEngine)
	ledger := core.NewLedger(pool, docService)
	return pool, poService, ledger, docService, invSvc, vendorID, ctx
}

// setupPurchaseOrderTestDB extends the base test DB with vendors, the PO document type,
// and a product needed for PO line tests.
func setupPurchaseOrderTestDB(t *testing.T) (*pgxpool.Pool, core.PurchaseOrderService, core.DocumentService, int, context.Context) {
	t.Helper()
	pool := setupTestDB(t)

	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO document_types (code, name, affects_inventory, affects_gl, affects_ar, affects_ap, numbering_strategy, resets_every_fy)
		VALUES ('PO', 'Purchase Order', false, false, false, false, 'per_fy', true)
		ON CONFLICT (code) DO NOTHING;

		INSERT INTO products (company_id, code, name, description, unit_price, unit, revenue_account_code)
		VALUES (1, 'P001', 'Widget A', 'Standard widget', 500.00, 'unit', '4000')
		ON CONFLICT (company_id, code) DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("seed PO test data: %v", err)
	}

	// Create a vendor for testing
	_, err = pool.Exec(ctx, `
		TRUNCATE TABLE purchase_orders CASCADE;
		TRUNCATE TABLE vendors CASCADE;

		INSERT INTO vendors (id, company_id, code, name, payment_terms_days, ap_account_code)
		VALUES (1, 1, 'V001', 'Test Supplier Ltd', 30, '2000');
	`)
	if err != nil {
		t.Fatalf("seed vendor for PO tests: %v", err)
	}

	docService := core.NewDocumentService(pool)
	poService := core.NewPurchaseOrderService(pool)

	return pool, poService, docService, 1, ctx // vendorID = 1
}

func TestPurchaseOrder_CreateAndApprove(t *testing.T) {
	pool, poService, docService, vendorID, ctx := setupPurchaseOrderTestDB(t)
	defer pool.Close()

	companyID := 1
	poDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	var createdPOID int

	t.Run("CreatePO_Success", func(t *testing.T) {
		lines := []core.PurchaseOrderLineInput{
			{
				ProductCode: "P001",
				Description: "Widget A - 100 units",
				Quantity:    decimal.NewFromInt(100),
				UnitCost:    decimal.NewFromFloat(450.00),
			},
			{
				Description:        "Shipping and Handling",
				Quantity:           decimal.NewFromInt(1),
				UnitCost:           decimal.NewFromFloat(500.00),
				ExpenseAccountCode: "5100",
			},
		}

		po, err := poService.CreatePO(ctx, companyID, vendorID, poDate, lines, "First test PO")
		if err != nil {
			t.Fatalf("CreatePO: %v", err)
		}

		if po.Status != "DRAFT" {
			t.Errorf("expected status DRAFT, got %s", po.Status)
		}
		if po.PONumber != nil {
			t.Errorf("DRAFT PO should not have po_number, got %q", *po.PONumber)
		}
		if po.VendorCode != "V001" {
			t.Errorf("expected vendor code V001, got %s", po.VendorCode)
		}
		if po.VendorName != "Test Supplier Ltd" {
			t.Errorf("expected vendor name 'Test Supplier Ltd', got %s", po.VendorName)
		}
		if len(po.Lines) != 2 {
			t.Errorf("expected 2 lines, got %d", len(po.Lines))
		}

		// Verify line totals: 100×450 + 1×500 = 45500
		expected := decimal.NewFromFloat(45500.00)
		if !po.TotalTransaction.Equal(expected) {
			t.Errorf("expected total %s, got %s", expected, po.TotalTransaction)
		}
		if po.ID == 0 {
			t.Error("expected PO ID to be set")
		}

		// Verify product line was linked
		if po.Lines[0].ProductCode == nil || *po.Lines[0].ProductCode != "P001" {
			t.Errorf("expected line 1 product code P001")
		}
		// Verify expense line
		if po.Lines[1].ExpenseAccountCode == nil || *po.Lines[1].ExpenseAccountCode != "5100" {
			t.Errorf("expected line 2 expense account code 5100")
		}

		createdPOID = po.ID
	})

	t.Run("CreatePO_NoLines_Fails", func(t *testing.T) {
		_, err := poService.CreatePO(ctx, companyID, vendorID, poDate, nil, "")
		if err == nil {
			t.Error("expected error for PO with no lines, got nil")
		}
	})

	t.Run("ApprovePO_AssignsPONumber", func(t *testing.T) {
		if createdPOID == 0 {
			t.Skip("CreatePO_Success must run first")
		}

		err := poService.ApprovePO(ctx, createdPOID, docService)
		if err != nil {
			t.Fatalf("ApprovePO: %v", err)
		}

		po, err := poService.GetPO(ctx, createdPOID)
		if err != nil {
			t.Fatalf("GetPO after approve: %v", err)
		}

		if po.Status != "APPROVED" {
			t.Errorf("expected status APPROVED, got %s", po.Status)
		}
		if po.PONumber == nil || *po.PONumber == "" {
			t.Error("approved PO must have po_number assigned")
		}
		// Gapless number format: PO-<year>-NNNNN
		if !strings.HasPrefix(*po.PONumber, "PO-") {
			t.Errorf("expected PO number to start with 'PO-', got %q", *po.PONumber)
		}
		if po.ApprovedAt == nil {
			t.Error("approved PO must have approved_at set")
		}
		t.Logf("Assigned PO number: %s", *po.PONumber)
	})

	t.Run("ApprovePO_Idempotent", func(t *testing.T) {
		if createdPOID == 0 {
			t.Skip("CreatePO_Success must run first")
		}

		// Approving an already-APPROVED PO is a no-op, not an error
		err := poService.ApprovePO(ctx, createdPOID, docService)
		if err != nil {
			t.Errorf("expected idempotent approve to succeed, got: %v", err)
		}
	})

	t.Run("ApprovePO_NotFound_Fails", func(t *testing.T) {
		err := poService.ApprovePO(ctx, 99999, docService)
		if err == nil {
			t.Error("expected error for non-existent PO, got nil")
		}
	})

	t.Run("GetPOs_FilteredByStatus", func(t *testing.T) {
		// Create another DRAFT PO
		_, err := poService.CreatePO(ctx, companyID, vendorID, poDate, []core.PurchaseOrderLineInput{
			{
				Description: "Service charge",
				Quantity:    decimal.NewFromInt(1),
				UnitCost:    decimal.NewFromFloat(1000.00),
			},
		}, "second PO")
		if err != nil {
			t.Fatalf("CreatePO second: %v", err)
		}

		approved, err := poService.GetPOs(ctx, companyID, "APPROVED")
		if err != nil {
			t.Fatalf("GetPOs APPROVED: %v", err)
		}
		if len(approved) != 1 {
			t.Errorf("expected 1 APPROVED PO, got %d", len(approved))
		}

		drafts, err := poService.GetPOs(ctx, companyID, "DRAFT")
		if err != nil {
			t.Fatalf("GetPOs DRAFT: %v", err)
		}
		if len(drafts) != 1 {
			t.Errorf("expected 1 DRAFT PO, got %d", len(drafts))
		}

		all, err := poService.GetPOs(ctx, companyID, "")
		if err != nil {
			t.Fatalf("GetPOs all: %v", err)
		}
		if len(all) != 2 {
			t.Errorf("expected 2 total POs, got %d", len(all))
		}
	})

	t.Run("CompanyIsolation", func(t *testing.T) {
		// Seed a second company and vendor
		pool.Exec(ctx, `
			INSERT INTO companies (id, company_code, name, base_currency)
			VALUES (2, '2000', 'Other Company', 'INR')
			ON CONFLICT DO NOTHING;

			INSERT INTO vendors (company_id, code, name, payment_terms_days, ap_account_code)
			VALUES (2, 'V001', 'Other Vendor', 30, '2000')
			ON CONFLICT DO NOTHING;
		`)

		var otherVendorID int
		if err := pool.QueryRow(ctx,
			"SELECT id FROM vendors WHERE company_id = 2 AND code = 'V001'",
		).Scan(&otherVendorID); err != nil {
			t.Fatalf("get other vendor ID: %v", err)
		}

		// Create a PO for the other company
		_, err := poService.CreatePO(ctx, 2, otherVendorID, poDate, []core.PurchaseOrderLineInput{
			{
				Description: "Other company item",
				Quantity:    decimal.NewFromInt(1),
				UnitCost:    decimal.NewFromFloat(100.00),
			},
		}, "")
		if err != nil {
			t.Fatalf("CreatePO for other company: %v", err)
		}

		// List POs for company 1 — must not see company 2's PO
		orders, err := poService.GetPOs(ctx, companyID, "")
		if err != nil {
			t.Fatalf("GetPOs company 1: %v", err)
		}
		for _, po := range orders {
			if po.CompanyID != companyID {
				t.Errorf("PO %d belongs to company %d, expected %d", po.ID, po.CompanyID, companyID)
			}
		}
	})
}

func TestPurchaseOrder_ReceivePO(t *testing.T) {
	pool, poService, ledger, docService, invSvc, vendorID, ctx := setupReceivePOTestDB(t)
	defer pool.Close()

	companyID := 1
	companyCode := "1000"
	poDate := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)

	// Create and approve a PO with one product line and one service line
	lines := []core.PurchaseOrderLineInput{
		{
			ProductCode: "P001",
			Description: "Widget A — 50 units",
			Quantity:    decimal.NewFromInt(50),
			UnitCost:    decimal.NewFromFloat(400.00),
		},
		{
			Description:        "Freight charges",
			Quantity:           decimal.NewFromInt(1),
			UnitCost:           decimal.NewFromFloat(500.00),
			ExpenseAccountCode: "5100",
		},
	}

	po, err := poService.CreatePO(ctx, companyID, vendorID, poDate, lines, "receive test PO")
	if err != nil {
		t.Fatalf("CreatePO: %v", err)
	}

	if err := poService.ApprovePO(ctx, po.ID, docService); err != nil {
		t.Fatalf("ApprovePO: %v", err)
	}

	// Re-fetch after approval to get line IDs
	po, err = poService.GetPO(ctx, po.ID)
	if err != nil {
		t.Fatalf("GetPO after approve: %v", err)
	}

	if len(po.Lines) != 2 {
		t.Fatalf("expected 2 PO lines, got %d", len(po.Lines))
	}
	goodsLine := po.Lines[0]   // P001 product line
	serviceLine := po.Lines[1] // freight service line

	t.Run("ReceivePO_NotApproved_Fails", func(t *testing.T) {
		// Create a DRAFT PO and try to receive it — must fail
		draftPO, _ := poService.CreatePO(ctx, companyID, vendorID, poDate, []core.PurchaseOrderLineInput{
			{Description: "Test", Quantity: decimal.NewFromInt(1), UnitCost: decimal.NewFromFloat(100)},
		}, "")
		err := poService.ReceivePO(ctx, draftPO.ID, "MAIN", companyCode,
			[]core.ReceivedLine{{POLineID: draftPO.Lines[0].ID, QtyReceived: decimal.NewFromInt(1)}},
			"2000", ledger, docService, invSvc)
		if err == nil {
			t.Error("expected error receiving DRAFT PO, got nil")
		}
	})

	t.Run("ReceivePO_NoLines_Fails", func(t *testing.T) {
		err := poService.ReceivePO(ctx, po.ID, "MAIN", companyCode,
			[]core.ReceivedLine{}, "2000", ledger, docService, invSvc)
		if err == nil {
			t.Error("expected error for empty received lines, got nil")
		}
	})

	t.Run("ReceivePO_Success", func(t *testing.T) {
		receivedLines := []core.ReceivedLine{
			{POLineID: goodsLine.ID, QtyReceived: decimal.NewFromInt(50)},
			{POLineID: serviceLine.ID, QtyReceived: decimal.NewFromInt(1)},
		}

		err := poService.ReceivePO(ctx, po.ID, "MAIN", companyCode,
			receivedLines, "2000", ledger, docService, invSvc)
		if err != nil {
			t.Fatalf("ReceivePO: %v", err)
		}

		// PO status must be RECEIVED
		updated, err := poService.GetPO(ctx, po.ID)
		if err != nil {
			t.Fatalf("GetPO after receive: %v", err)
		}
		if updated.Status != "RECEIVED" {
			t.Errorf("expected status RECEIVED, got %s", updated.Status)
		}
		if updated.ReceivedAt == nil {
			t.Error("expected received_at to be set")
		}

		// Inventory qty_on_hand must have increased by 50
		stockLevels, err := invSvc.GetStockLevels(ctx, companyCode)
		if err != nil {
			t.Fatalf("GetStockLevels: %v", err)
		}
		var found bool
		for _, sl := range stockLevels {
			if sl.ProductCode == "P001" {
				found = true
				expected := decimal.NewFromInt(50)
				if !sl.OnHand.Equal(expected) {
					t.Errorf("P001 on_hand: expected %s, got %s", expected, sl.OnHand)
				}
				expectedCost := decimal.NewFromFloat(400.00)
				if !sl.UnitCost.Equal(expectedCost) {
					t.Errorf("P001 unit_cost: expected %s, got %s", expectedCost, sl.UnitCost)
				}
			}
		}
		if !found {
			t.Error("P001 not found in stock levels after receipt")
		}

		// inventory_movement for goods line must reference the PO line
		var movementPOLineID *int
		err = pool.QueryRow(ctx, `
			SELECT im.po_line_id
			FROM inventory_movements im
			JOIN inventory_items ii ON ii.id = im.inventory_item_id
			JOIN products p ON p.id = ii.product_id
			WHERE p.company_id = $1 AND p.code = 'P001' AND im.movement_type = 'RECEIPT'
			ORDER BY im.id DESC LIMIT 1`,
			companyID,
		).Scan(&movementPOLineID)
		if err != nil {
			t.Fatalf("query inventory movement: %v", err)
		}
		if movementPOLineID == nil || *movementPOLineID != goodsLine.ID {
			t.Errorf("expected po_line_id=%d on inventory movement, got %v", goodsLine.ID, movementPOLineID)
		}
	})

	t.Run("ReceivePO_AlreadyReceived_Fails", func(t *testing.T) {
		// Attempting to receive an already-RECEIVED PO must fail
		err := poService.ReceivePO(ctx, po.ID, "MAIN", companyCode,
			[]core.ReceivedLine{{POLineID: goodsLine.ID, QtyReceived: decimal.NewFromInt(1)}},
			"2000", ledger, docService, invSvc)
		if err == nil {
			t.Error("expected error receiving already-RECEIVED PO, got nil")
		}
	})
}

func TestPurchaseOrder_FullLifecycle(t *testing.T) {
	pool, poService, ledger, docService, invSvc, vendorID, ctx := setupReceivePOTestDB(t)
	defer pool.Close()

	companyID := 1
	companyCode := "1000"
	poDate := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

	// Seed a bank account for payment
	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (company_id, code, name, type)
		VALUES (1, '1100', 'Current Account', 'asset')
		ON CONFLICT (company_id, code) DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("seed bank account: %v", err)
	}

	// Create PO with one goods line
	lines := []core.PurchaseOrderLineInput{
		{
			ProductCode: "P001",
			Description: "Widget A — 20 units",
			Quantity:    decimal.NewFromInt(20),
			UnitCost:    decimal.NewFromFloat(500.00),
		},
	}
	po, err := poService.CreatePO(ctx, companyID, vendorID, poDate, lines, "lifecycle test")
	if err != nil {
		t.Fatalf("CreatePO: %v", err)
	}

	// Approve
	if err := poService.ApprovePO(ctx, po.ID, docService); err != nil {
		t.Fatalf("ApprovePO: %v", err)
	}

	// Re-fetch to get line IDs
	po, err = poService.GetPO(ctx, po.ID)
	if err != nil {
		t.Fatalf("GetPO after approve: %v", err)
	}

	// Receive
	if err := poService.ReceivePO(ctx, po.ID, "MAIN", companyCode,
		[]core.ReceivedLine{{POLineID: po.Lines[0].ID, QtyReceived: decimal.NewFromInt(20)}},
		"2000", ledger, docService, invSvc); err != nil {
		t.Fatalf("ReceivePO: %v", err)
	}

	t.Run("RecordVendorInvoice_NotReceived_Fails", func(t *testing.T) {
		// Create a fresh DRAFT PO and try to invoice it — must fail
		draftPO, _ := poService.CreatePO(ctx, companyID, vendorID, poDate, []core.PurchaseOrderLineInput{
			{Description: "Test item", Quantity: decimal.NewFromInt(1), UnitCost: decimal.NewFromFloat(100)},
		}, "")
		_, err := poService.RecordVendorInvoice(ctx, draftPO.ID, "INV-9999",
			time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC),
			decimal.NewFromFloat(100), docService)
		if err == nil {
			t.Error("expected error invoicing DRAFT PO, got nil")
		}
	})

	t.Run("RecordVendorInvoice_Success", func(t *testing.T) {
		invoiceDate := time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)
		invoiceAmount := decimal.NewFromFloat(10000.00) // exact match: 20×500

		warning, err := poService.RecordVendorInvoice(ctx, po.ID,
			"INV-2026-001", invoiceDate, invoiceAmount, docService)
		if err != nil {
			t.Fatalf("RecordVendorInvoice: %v", err)
		}
		if warning != "" {
			t.Logf("unexpected warning: %s", warning)
		}

		updated, err := poService.GetPO(ctx, po.ID)
		if err != nil {
			t.Fatalf("GetPO after invoice: %v", err)
		}
		if updated.Status != "INVOICED" {
			t.Errorf("expected status INVOICED, got %s", updated.Status)
		}
		if updated.InvoiceNumber == nil || *updated.InvoiceNumber != "INV-2026-001" {
			t.Errorf("expected invoice_number 'INV-2026-001', got %v", updated.InvoiceNumber)
		}
		if updated.PIDocumentNumber == nil || *updated.PIDocumentNumber == "" {
			t.Error("expected PI document number to be assigned")
		}
		if updated.InvoicedAt == nil {
			t.Error("expected invoiced_at to be set")
		}
		t.Logf("PI document number: %s", *updated.PIDocumentNumber)
	})

	t.Run("RecordVendorInvoice_AmountDeviation_Warning", func(t *testing.T) {
		// Create and receive a new expense-only PO to test the warning
		po2, err := poService.CreatePO(ctx, companyID, vendorID, poDate, []core.PurchaseOrderLineInput{
			{
				Description:        "Consulting services",
				Quantity:           decimal.NewFromInt(1),
				UnitCost:           decimal.NewFromFloat(1000),
				ExpenseAccountCode: "5100",
			},
		}, "")
		if err != nil {
			t.Fatalf("CreatePO for deviation test: %v", err)
		}
		if err := poService.ApprovePO(ctx, po2.ID, docService); err != nil {
			t.Fatalf("ApprovePO for deviation test: %v", err)
		}
		po2, err = poService.GetPO(ctx, po2.ID)
		if err != nil {
			t.Fatalf("GetPO for deviation test: %v", err)
		}
		if err := poService.ReceivePO(ctx, po2.ID, "MAIN", companyCode,
			[]core.ReceivedLine{{POLineID: po2.Lines[0].ID, QtyReceived: decimal.NewFromInt(1)}},
			"2000", ledger, docService, invSvc); err != nil {
			t.Fatalf("ReceivePO for deviation test: %v", err)
		}

		// Invoice with 10% more than PO total — should produce a warning
		warning, err := poService.RecordVendorInvoice(ctx, po2.ID, "INV-HIGH",
			time.Date(2026, 3, 26, 0, 0, 0, 0, time.UTC),
			decimal.NewFromFloat(1100), docService)
		if err != nil {
			t.Fatalf("RecordVendorInvoice with deviation: %v", err)
		}
		if warning == "" {
			t.Error("expected a warning for >5% amount deviation, got empty string")
		}
		t.Logf("Deviation warning: %s", warning)
	})

	t.Run("PayVendor_NotInvoiced_Fails", func(t *testing.T) {
		// Create and receive a new PO (RECEIVED but not INVOICED) — pay must fail
		po3, _ := poService.CreatePO(ctx, companyID, vendorID, poDate, []core.PurchaseOrderLineInput{
			{Description: "Non-invoiced item", Quantity: decimal.NewFromInt(1), UnitCost: decimal.NewFromFloat(200)},
		}, "")
		_ = poService.ApprovePO(ctx, po3.ID, docService)
		po3, _ = poService.GetPO(ctx, po3.ID)
		_ = poService.ReceivePO(ctx, po3.ID, "MAIN", companyCode,
			[]core.ReceivedLine{{POLineID: po3.Lines[0].ID, QtyReceived: decimal.NewFromInt(1)}},
			"2000", ledger, docService, invSvc)

		err := poService.PayVendor(ctx, po3.ID, "1100",
			time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC), companyCode, ledger)
		if err == nil {
			t.Error("expected error paying RECEIVED (not INVOICED) PO, got nil")
		}
	})

	t.Run("PayVendor_Success_APClears", func(t *testing.T) {
		// Get AP balance before payment (account 2000)
		// journal_lines stores account_id (FK), so join via accounts table
		var apBalanceBefore decimal.Decimal
		pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(jl.debit_base) - SUM(jl.credit_base), 0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.entry_id
			JOIN companies c ON c.id = je.company_id
			JOIN accounts a ON a.id = jl.account_id AND a.company_id = c.id
			WHERE c.company_code = $1 AND a.code = '2000'`,
			companyCode,
		).Scan(&apBalanceBefore)

		paymentDate := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
		err := poService.PayVendor(ctx, po.ID, "1100", paymentDate, companyCode, ledger)
		if err != nil {
			t.Fatalf("PayVendor: %v", err)
		}

		updated, err := poService.GetPO(ctx, po.ID)
		if err != nil {
			t.Fatalf("GetPO after payment: %v", err)
		}
		if updated.Status != "PAID" {
			t.Errorf("expected status PAID, got %s", updated.Status)
		}
		if updated.PaidAt == nil {
			t.Error("expected paid_at to be set")
		}

		// Verify AP account 2000 net balance: after payment DR AP 10000 was posted
		var apBalanceAfter decimal.Decimal
		pool.QueryRow(ctx, `
			SELECT COALESCE(SUM(jl.debit_base) - SUM(jl.credit_base), 0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.entry_id
			JOIN companies c ON c.id = je.company_id
			JOIN accounts a ON a.id = jl.account_id AND a.company_id = c.id
			WHERE c.company_code = $1 AND a.code = '2000'`,
			companyCode,
		).Scan(&apBalanceAfter)

		// AP balance should have increased (debit of 10000 reduces AP credit balance)
		diff := apBalanceAfter.Sub(apBalanceBefore)
		expectedDiff := decimal.NewFromFloat(10000.00)
		if !diff.Equal(expectedDiff) {
			t.Errorf("expected AP balance to increase by 10000 after payment, got diff %s", diff)
		}
		t.Logf("AP balance before: %s, after: %s, diff: %s", apBalanceBefore, apBalanceAfter, diff)
	})
}
