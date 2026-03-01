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

type purchaseOrderService struct {
	pool *pgxpool.Pool
}

// NewPurchaseOrderService constructs a PurchaseOrderService backed by PostgreSQL.
func NewPurchaseOrderService(pool *pgxpool.Pool) PurchaseOrderService {
	return &purchaseOrderService{pool: pool}
}

// CreatePO creates a new DRAFT purchase order with computed line totals.
func (s *purchaseOrderService) CreatePO(ctx context.Context, companyID, vendorID int, poDate time.Time, lines []PurchaseOrderLineInput, notes string) (*PurchaseOrder, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("purchase order must have at least one line")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Validate vendor belongs to this company
	var vendorExists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM vendors WHERE id = $1 AND company_id = $2 AND is_active = true)",
		vendorID, companyID,
	).Scan(&vendorExists); err != nil {
		return nil, fmt.Errorf("validate vendor: %w", err)
	}
	if !vendorExists {
		return nil, fmt.Errorf("vendor %d not found for company %d", vendorID, companyID)
	}

	// Resolve lines and compute totals
	exchangeRate := decimal.NewFromInt(1)
	type resolvedLine struct {
		productID          *int
		productCode        *string
		productName        *string
		description        string
		quantity           decimal.Decimal
		unitCost           decimal.Decimal
		lineTotalTx        decimal.Decimal
		lineTotalBase      decimal.Decimal
		expenseAccountCode *string
	}

	var resolved []resolvedLine
	var totalTransaction decimal.Decimal

	for i, input := range lines {
		rl := resolvedLine{
			description: input.Description,
			quantity:    input.Quantity,
			unitCost:    input.UnitCost,
		}

		if input.ProductCode != "" {
			var pid int
			var pcode, pname string
			err := tx.QueryRow(ctx,
				"SELECT id, code, name FROM products WHERE company_id = $1 AND code = $2 AND is_active = true",
				companyID, input.ProductCode,
			).Scan(&pid, &pcode, &pname)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, fmt.Errorf("line %d: product %q not found", i+1, input.ProductCode)
				}
				return nil, fmt.Errorf("line %d: resolve product: %w", i+1, err)
			}
			rl.productID = &pid
			rl.productCode = &pcode
			rl.productName = &pname
		}

		if input.ExpenseAccountCode != "" {
			code := input.ExpenseAccountCode
			rl.expenseAccountCode = &code
		}

		lineTotal := input.Quantity.Mul(input.UnitCost)
		rl.lineTotalTx = lineTotal
		rl.lineTotalBase = lineTotal.Mul(exchangeRate)
		totalTransaction = totalTransaction.Add(lineTotal)
		resolved = append(resolved, rl)
	}

	totalBase := totalTransaction.Mul(exchangeRate)

	var toNotes *string
	if notes != "" {
		toNotes = &notes
	}

	// Insert PO header
	var poID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO purchase_orders (company_id, vendor_id, status, po_date, currency, exchange_rate,
		                             total_transaction, total_base, notes)
		VALUES ($1, $2, 'DRAFT', $3, 'INR', $4, $5, $6, $7)
		RETURNING id`,
		companyID, vendorID, poDate.Format("2006-01-02"), exchangeRate, totalTransaction, totalBase, toNotes,
	).Scan(&poID); err != nil {
		return nil, fmt.Errorf("insert purchase order: %w", err)
	}

	// Insert lines
	for i, rl := range resolved {
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_order_lines
			            (order_id, line_number, product_id, description, quantity, unit_cost,
			             line_total_transaction, line_total_base, expense_account_code)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			poID, i+1, rl.productID, rl.description, rl.quantity, rl.unitCost,
			rl.lineTotalTx, rl.lineTotalBase, rl.expenseAccountCode,
		); err != nil {
			return nil, fmt.Errorf("insert PO line %d: %w", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit purchase order: %w", err)
	}

	return s.GetPO(ctx, poID)
}

// ApprovePO transitions a DRAFT PO to APPROVED, assigning a gapless PO number.
// companyID must match the PO's company — returns an error if they differ.
// Approving an already-APPROVED PO is a no-op.
func (s *purchaseOrderService) ApprovePO(ctx context.Context, companyID, poID int, docService DocumentService) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var poCompanyID int
	var status string
	if err := tx.QueryRow(ctx,
		"SELECT company_id, status FROM purchase_orders WHERE id = $1 FOR UPDATE",
		poID,
	).Scan(&poCompanyID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("purchase order %d not found", poID)
		}
		return fmt.Errorf("fetch purchase order %d: %w", poID, err)
	}

	if poCompanyID != companyID {
		return fmt.Errorf("purchase order %d does not belong to company %d", poID, companyID)
	}

	// Idempotent: already approved is a no-op
	if status == "APPROVED" {
		return nil
	}

	if status != "DRAFT" {
		return fmt.Errorf("purchase order %d cannot be approved: status is %s (must be DRAFT)", poID, status)
	}

	// Get current financial year for the PO document
	var financialYear int
	if err := tx.QueryRow(ctx,
		"SELECT EXTRACT(YEAR FROM po_date)::int FROM purchase_orders WHERE id = $1",
		poID,
	).Scan(&financialYear); err != nil {
		return fmt.Errorf("get PO date: %w", err)
	}

	// Create a DRAFT PO document inside this transaction
	var draftDocID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO documents (company_id, type_code, status, financial_year, branch_id)
		VALUES ($1, 'PO', 'DRAFT', $2, NULL)
		RETURNING id`,
		companyID, financialYear,
	).Scan(&draftDocID); err != nil {
		return fmt.Errorf("create PO document: %w", err)
	}

	// Post the document to assign gapless number
	if err := docService.PostDocumentTx(ctx, tx, draftDocID); err != nil {
		return fmt.Errorf("post PO document: %w", err)
	}

	var poNumber string
	if err := tx.QueryRow(ctx,
		"SELECT document_number FROM documents WHERE id = $1",
		draftDocID,
	).Scan(&poNumber); err != nil {
		return fmt.Errorf("retrieve PO document number: %w", err)
	}

	// Transition PO to APPROVED
	if _, err := tx.Exec(ctx, `
		UPDATE purchase_orders
		SET status = 'APPROVED', po_number = $1, approved_at = NOW()
		WHERE id = $2`,
		poNumber, poID,
	); err != nil {
		return fmt.Errorf("approve purchase order %d: %w", poID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit PO approval: %w", err)
	}

	return nil
}

// ReceivePO records goods and/or services received against an APPROVED purchase order.
// Physical-goods lines (with product_id) update inventory via InventoryService.ReceiveStock.
// Service/expense lines (with expense_account_code, no product_id) post DR expense / CR AP.
// On success, transitions PO status to RECEIVED and sets received_at.
func (s *purchaseOrderService) ReceivePO(ctx context.Context, poID int, warehouseCode, companyCode string,
	receivedLines []ReceivedLine, apAccountCode string,
	ledger *Ledger, docService DocumentService, inv InventoryService) error {

	if len(receivedLines) == 0 {
		return fmt.Errorf("at least one received line is required")
	}

	// Load and validate PO — assert company ownership in the query so cross-company IDs are rejected.
	po, err := s.getPOForCompany(ctx, poID, companyCode)
	if err != nil {
		return err
	}
	if po.Status != "APPROVED" {
		return fmt.Errorf("purchase order %d cannot be received: status is %s (must be APPROVED)", poID, po.Status)
	}

	// Build a map of PO lines by ID for quick lookup
	lineByID := make(map[int]PurchaseOrderLine, len(po.Lines))
	for _, l := range po.Lines {
		lineByID[l.ID] = l
	}

	// Resolve movement date: use today
	movementDate := time.Now().Format("2006-01-02")

	// Process each received line
	for _, rl := range receivedLines {
		if rl.QtyReceived.IsZero() || rl.QtyReceived.IsNegative() {
			return fmt.Errorf("PO line %d: received quantity must be positive", rl.POLineID)
		}

		pol, ok := lineByID[rl.POLineID]
		if !ok {
			return fmt.Errorf("PO line %d not found on purchase order %d", rl.POLineID, poID)
		}

		if pol.ProductID != nil {
			// Physical goods line — check cumulative received qty does not exceed ordered qty.
			var alreadyReceived decimal.Decimal
			if err := s.pool.QueryRow(ctx, `
				SELECT COALESCE(SUM(im.quantity), 0)
				FROM inventory_movements im
				WHERE im.po_line_id = $1 AND im.movement_type = 'RECEIPT'`,
				pol.ID,
			).Scan(&alreadyReceived); err != nil {
				return fmt.Errorf("check received quantity for PO line %d: %w", pol.ID, err)
			}
			totalAfterReceipt := alreadyReceived.Add(rl.QtyReceived)
			if totalAfterReceipt.GreaterThan(pol.Quantity) {
				return fmt.Errorf(
					"PO line %d: would receive %s but only %s ordered (already received %s)",
					pol.ID, totalAfterReceipt.StringFixed(4), pol.Quantity.StringFixed(4),
					alreadyReceived.StringFixed(4),
				)
			}

			// Receive into inventory
			productCode := ""
			if pol.ProductCode != nil {
				productCode = *pol.ProductCode
			}
			lineID := pol.ID
			if err := inv.ReceiveStock(ctx, companyCode, warehouseCode, productCode,
				rl.QtyReceived, pol.UnitCost, movementDate, apAccountCode,
				&lineID, ledger, docService); err != nil {
				return fmt.Errorf("receive inventory for PO line %d (product %s): %w", pol.ID, productCode, err)
			}
		} else if pol.ExpenseAccountCode != nil && *pol.ExpenseAccountCode != "" {
			// Service/expense line — post DR expense / CR AP
			lineAmount := rl.QtyReceived.Mul(pol.UnitCost)
			var baseCurrency string
			if err := s.pool.QueryRow(ctx,
				"SELECT base_currency FROM companies WHERE company_code = $1", companyCode,
			).Scan(&baseCurrency); err != nil {
				return fmt.Errorf("resolve company currency: %w", err)
			}

			proposal := Proposal{
				DocumentTypeCode:    "GR",
				CompanyCode:         companyCode,
				IdempotencyKey:      fmt.Sprintf("po-%d-line-%d-service-receipt", poID, pol.ID),
				TransactionCurrency: baseCurrency,
				ExchangeRate:        "1",
				Summary:             fmt.Sprintf("Service receipt: %s (PO %d, line %d)", pol.Description, poID, pol.LineNumber),
				PostingDate:         movementDate,
				DocumentDate:        movementDate,
				Confidence:          1.0,
				Reasoning:           fmt.Sprintf("Service/expense line received against PO %d line %d.", poID, pol.LineNumber),
				Lines: []ProposalLine{
					{AccountCode: *pol.ExpenseAccountCode, IsDebit: true, Amount: lineAmount.StringFixed(2)},
					{AccountCode: apAccountCode, IsDebit: false, Amount: lineAmount.StringFixed(2)},
				},
			}
			if err := ledger.Commit(ctx, proposal); err != nil {
				return fmt.Errorf("post service receipt journal entry for PO line %d: %w", pol.ID, err)
			}
		} else {
			return fmt.Errorf("PO line %d has no product or expense account code — cannot receive", pol.ID)
		}
	}

	// Transition PO to RECEIVED
	if _, err := s.pool.Exec(ctx, `
		UPDATE purchase_orders
		SET status = 'RECEIVED', received_at = NOW()
		WHERE id = $1`,
		poID,
	); err != nil {
		return fmt.Errorf("update PO %d status to RECEIVED: %w", poID, err)
	}

	return nil
}

// RecordVendorInvoice records the vendor's invoice against a RECEIVED purchase order.
// companyID must match the PO's company — returns an error if they differ.
// Creates and posts a PI document. Warns if invoiceAmount deviates > 5% from PO total.
// Transitions status to INVOICED. Returns a non-empty warning if amount deviation > 5%.
func (s *purchaseOrderService) RecordVendorInvoice(ctx context.Context, companyID, poID int,
	invoiceNumber string, invoiceDate time.Time, invoiceAmount decimal.Decimal,
	docService DocumentService) (string, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var poCompanyID int
	var status string
	var totalBase decimal.Decimal
	if err := tx.QueryRow(ctx,
		"SELECT company_id, status, total_base FROM purchase_orders WHERE id = $1 FOR UPDATE",
		poID,
	).Scan(&poCompanyID, &status, &totalBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("purchase order %d not found", poID)
		}
		return "", fmt.Errorf("fetch purchase order %d: %w", poID, err)
	}

	if poCompanyID != companyID {
		return "", fmt.Errorf("purchase order %d does not belong to company %d", poID, companyID)
	}

	if status != "RECEIVED" {
		return "", fmt.Errorf("purchase order %d cannot be invoiced: status is %s (must be RECEIVED)", poID, status)
	}

	// Check for > 5% amount deviation
	var warning string
	if !totalBase.IsZero() {
		deviation := invoiceAmount.Sub(totalBase).Abs().Div(totalBase)
		threshold := decimal.NewFromFloat(0.05)
		if deviation.GreaterThan(threshold) {
			pct := deviation.Mul(decimal.NewFromInt(100)).StringFixed(1)
			warning = fmt.Sprintf("invoice amount %s deviates %s%% from PO total %s (threshold 5%%)",
				invoiceAmount.StringFixed(2), pct, totalBase.StringFixed(2))
		}
	}

	financialYear := invoiceDate.Year()

	// Create DRAFT PI document inside this transaction
	var draftDocID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO documents (company_id, type_code, status, financial_year, branch_id)
		VALUES ($1, 'PI', 'DRAFT', $2, NULL)
		RETURNING id`,
		companyID, financialYear,
	).Scan(&draftDocID); err != nil {
		return "", fmt.Errorf("create PI document: %w", err)
	}

	if err := docService.PostDocumentTx(ctx, tx, draftDocID); err != nil {
		return "", fmt.Errorf("post PI document: %w", err)
	}

	var piDocNumber string
	if err := tx.QueryRow(ctx,
		"SELECT document_number FROM documents WHERE id = $1",
		draftDocID,
	).Scan(&piDocNumber); err != nil {
		return "", fmt.Errorf("retrieve PI document number: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE purchase_orders
		SET status = 'INVOICED',
		    invoice_number     = $1,
		    invoice_date       = $2,
		    invoice_amount     = $3,
		    pi_document_number = $4,
		    invoiced_at        = NOW()
		WHERE id = $5`,
		invoiceNumber, invoiceDate.Format("2006-01-02"), invoiceAmount, piDocNumber, poID,
	); err != nil {
		return "", fmt.Errorf("update PO %d to INVOICED: %w", poID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit vendor invoice: %w", err)
	}

	return warning, nil
}

// PayVendor records payment against an INVOICED purchase order.
// Posts DR AP / CR Bank and transitions status to PAID.
func (s *purchaseOrderService) PayVendor(ctx context.Context, poID int,
	bankAccountCode string, paymentDate time.Time, companyCode string, ledger *Ledger) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var companyID int
	var status string
	var invoiceAmount *decimal.Decimal
	var totalBase decimal.Decimal
	var apAccountCode string
	if err := tx.QueryRow(ctx, `
		SELECT po.company_id, po.status, po.invoice_amount, po.total_base,
		       COALESCE(v.ap_account_code, '2000')
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		WHERE po.id = $1
		FOR UPDATE OF po`,
		poID,
	).Scan(&companyID, &status, &invoiceAmount, &totalBase, &apAccountCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("purchase order %d not found", poID)
		}
		return fmt.Errorf("fetch purchase order %d: %w", poID, err)
	}

	if status != "INVOICED" {
		return fmt.Errorf("purchase order %d cannot be paid: status is %s (must be INVOICED)", poID, status)
	}

	// Verify the PO's company matches the supplied companyCode.
	var expectedCompanyID int
	if err := tx.QueryRow(ctx,
		"SELECT id FROM companies WHERE company_code = $1", companyCode,
	).Scan(&expectedCompanyID); err != nil {
		return fmt.Errorf("resolve company %s: %w", companyCode, err)
	}
	if companyID != expectedCompanyID {
		return fmt.Errorf("purchase order %d does not belong to company %s", poID, companyCode)
	}

	// Use invoice amount if recorded, otherwise fall back to PO total
	paymentAmount := totalBase
	if invoiceAmount != nil && !invoiceAmount.IsZero() {
		paymentAmount = *invoiceAmount
	}
	paymentDateStr := paymentDate.Format("2006-01-02")

	proposal := Proposal{
		DocumentTypeCode:    "JE",
		CompanyCode:         companyCode,
		IdempotencyKey:      fmt.Sprintf("pay-vendor-po-%d", poID),
		TransactionCurrency: "INR",
		ExchangeRate:        "1",
		Summary:             fmt.Sprintf("Vendor payment for PO %d", poID),
		PostingDate:         paymentDateStr,
		DocumentDate:        paymentDateStr,
		Confidence:          1.0,
		Reasoning:           fmt.Sprintf("Payment of vendor invoice for purchase order %d.", poID),
		Lines: []ProposalLine{
			{AccountCode: apAccountCode, IsDebit: true, Amount: paymentAmount.StringFixed(2)},
			{AccountCode: bankAccountCode, IsDebit: false, Amount: paymentAmount.StringFixed(2)},
		},
	}

	if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
		return fmt.Errorf("post payment journal entry for PO %d: %w", poID, err)
	}

	if _, err := tx.Exec(ctx,
		"UPDATE purchase_orders SET status = 'PAID', paid_at = NOW() WHERE id = $1",
		poID,
	); err != nil {
		return fmt.Errorf("update PO %d to PAID: %w", poID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit vendor payment: %w", err)
	}

	return nil
}

// getPOForCompany fetches a PO by ID, asserting it belongs to the given companyCode.
// Returns pgx.ErrNoRows-wrapped error (indistinguishable from not-found) if ownership fails
// to prevent PO enumeration across companies.
func (s *purchaseOrderService) getPOForCompany(ctx context.Context, poID int, companyCode string) (*PurchaseOrder, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM purchase_orders po
			JOIN companies c ON c.id = po.company_id
			WHERE po.id = $1 AND c.company_code = $2
		)`, poID, companyCode,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("purchase order ownership check: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("purchase order %d not found", poID)
	}
	return s.GetPO(ctx, poID)
}

// GetPO returns a purchase order by its internal ID, including all lines.
func (s *purchaseOrderService) GetPO(ctx context.Context, poID int) (*PurchaseOrder, error) {
	po := &PurchaseOrder{}
	if err := s.pool.QueryRow(ctx, `
		SELECT po.id, po.company_id, po.vendor_id, v.code, v.name,
		       po.po_number, po.status, po.po_date::text, po.expected_delivery_date::text,
		       po.currency, po.exchange_rate, po.total_transaction, po.total_base,
		       po.notes, po.approved_at, po.received_at,
		       po.invoice_number, po.invoice_date::text, po.invoice_amount,
		       po.pi_document_number, po.invoiced_at, po.paid_at,
		       po.created_at
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		WHERE po.id = $1`,
		poID,
	).Scan(
		&po.ID, &po.CompanyID, &po.VendorID, &po.VendorCode, &po.VendorName,
		&po.PONumber, &po.Status, &po.PODate, &po.ExpectedDeliveryDate,
		&po.Currency, &po.ExchangeRate, &po.TotalTransaction, &po.TotalBase,
		&po.Notes, &po.ApprovedAt, &po.ReceivedAt,
		&po.InvoiceNumber, &po.InvoiceDate, &po.InvoiceAmount,
		&po.PIDocumentNumber, &po.InvoicedAt, &po.PaidAt,
		&po.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("purchase order %d not found", poID)
		}
		return nil, fmt.Errorf("get purchase order %d: %w", poID, err)
	}

	lines, err := s.fetchLines(ctx, poID)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	return po, nil
}

// GetPOs returns purchase orders for a company, optionally filtered by status.
func (s *purchaseOrderService) GetPOs(ctx context.Context, companyID int, status string) ([]PurchaseOrder, error) {
	query := `
		SELECT po.id, po.company_id, po.vendor_id, v.code, v.name,
		       po.po_number, po.status, po.po_date::text, po.expected_delivery_date::text,
		       po.currency, po.exchange_rate, po.total_transaction, po.total_base,
		       po.notes, po.approved_at, po.received_at,
		       po.invoice_number, po.invoice_date::text, po.invoice_amount,
		       po.pi_document_number, po.invoiced_at, po.paid_at,
		       po.created_at
		FROM purchase_orders po
		JOIN vendors v ON v.id = po.vendor_id
		WHERE po.company_id = $1`
	args := []any{companyID}

	if status != "" {
		query += " AND po.status = $2"
		args = append(args, status)
	}
	query += " ORDER BY po.created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list purchase orders: %w", err)
	}
	defer rows.Close()

	var orders []PurchaseOrder
	for rows.Next() {
		var po PurchaseOrder
		if err := rows.Scan(
			&po.ID, &po.CompanyID, &po.VendorID, &po.VendorCode, &po.VendorName,
			&po.PONumber, &po.Status, &po.PODate, &po.ExpectedDeliveryDate,
			&po.Currency, &po.ExchangeRate, &po.TotalTransaction, &po.TotalBase,
			&po.Notes, &po.ApprovedAt, &po.ReceivedAt,
			&po.InvoiceNumber, &po.InvoiceDate, &po.InvoiceAmount,
			&po.PIDocumentNumber, &po.InvoicedAt, &po.PaidAt,
			&po.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan purchase order: %w", err)
		}
		orders = append(orders, po)
	}
	return orders, nil
}

// fetchLines returns all lines for a purchase order.
func (s *purchaseOrderService) fetchLines(ctx context.Context, poID int) ([]PurchaseOrderLine, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pol.id, pol.order_id, pol.line_number,
		       pol.product_id, p.code, p.name,
		       pol.description, pol.quantity, pol.unit_cost,
		       pol.line_total_transaction, pol.line_total_base,
		       pol.expense_account_code
		FROM purchase_order_lines pol
		LEFT JOIN products p ON p.id = pol.product_id
		WHERE pol.order_id = $1
		ORDER BY pol.line_number`,
		poID,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch PO lines for order %d: %w", poID, err)
	}
	defer rows.Close()

	var lines []PurchaseOrderLine
	for rows.Next() {
		var l PurchaseOrderLine
		if err := rows.Scan(
			&l.ID, &l.OrderID, &l.LineNumber,
			&l.ProductID, &l.ProductCode, &l.ProductName,
			&l.Description, &l.Quantity, &l.UnitCost,
			&l.LineTotalTransaction, &l.LineTotalBase,
			&l.ExpenseAccountCode,
		); err != nil {
			return nil, fmt.Errorf("scan PO line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}
