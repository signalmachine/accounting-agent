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

// defaultBankAccountCode is used when no bank account is specified for payment recording.
const defaultBankAccountCode = "1100"

// OrderService manages the sales order lifecycle and triggers ledger entries at key transitions.
type OrderService interface {
	// Master data
	CreateCustomer(ctx context.Context, companyCode, code, name, email, phone, address string, creditLimit decimal.Decimal, paymentTermsDays int) (*Customer, error)
	GetCustomers(ctx context.Context, companyCode string) ([]Customer, error)
	CreateProduct(ctx context.Context, companyCode, code, name, description string, unitPrice decimal.Decimal, unit, revenueAccountCode string) (*Product, error)
	GetProducts(ctx context.Context, companyCode string) ([]Product, error)

	// Order lifecycle
	CreateOrder(ctx context.Context, companyCode, customerCode, currency string, exchangeRate decimal.Decimal, orderDate string, lines []OrderLineInput, notes string) (*SalesOrder, error)
	// ConfirmOrder transitions DRAFT → CONFIRMED. Pass inv=nil to skip stock reservation.
	ConfirmOrder(ctx context.Context, orderID int, docService DocumentService, inv InventoryService) (*SalesOrder, error)
	// ShipOrder transitions CONFIRMED → SHIPPED. Pass inv=nil to skip COGS booking.
	ShipOrder(ctx context.Context, orderID int, inv InventoryService, ledger *Ledger, docService DocumentService) (*SalesOrder, error)
	InvoiceOrder(ctx context.Context, orderID int, ledger *Ledger, docService DocumentService) (*SalesOrder, error)
	RecordPayment(ctx context.Context, orderID int, bankAccountCode string, paymentDate string, ledger *Ledger) error
	// CancelOrder transitions DRAFT → CANCELLED. Pass inv=nil to skip reservation release.
	CancelOrder(ctx context.Context, orderID int, inv InventoryService) (*SalesOrder, error)

	// Queries
	GetOrder(ctx context.Context, orderID int) (*SalesOrder, error)
	GetOrders(ctx context.Context, companyCode string, status *string) ([]SalesOrder, error)
	GetOrderByNumber(ctx context.Context, companyCode, orderNumber string) (*SalesOrder, error)
}

type orderService struct {
	pool        *pgxpool.Pool
	ruleEngine  RuleEngine
}

func NewOrderService(pool *pgxpool.Pool, ruleEngine RuleEngine) OrderService {
	return &orderService{pool: pool, ruleEngine: ruleEngine}
}

// resolveCompanyID looks up the internal company ID from a company code.
func (s *orderService) resolveCompanyID(ctx context.Context, q pgxQuerier, companyCode string) (int, error) {
	var id int
	err := q.QueryRow(ctx, "SELECT id FROM companies WHERE company_code = $1", companyCode).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("company code %s not found", companyCode)
		}
		return 0, fmt.Errorf("failed to resolve company %s: %w", companyCode, err)
	}
	return id, nil
}

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, enabling shared query helpers.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ── Master Data ──────────────────────────────────────────────────────────────

func (s *orderService) CreateCustomer(ctx context.Context, companyCode, code, name, email, phone, address string, creditLimit decimal.Decimal, paymentTermsDays int) (*Customer, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	var c Customer
	err = s.pool.QueryRow(ctx, `
		INSERT INTO customers (company_id, code, name, email, phone, address, credit_limit, payment_terms_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, company_id, code, name, email, phone, address, credit_limit, payment_terms_days, created_at
	`, companyID, code, name, email, phone, address, creditLimit, paymentTermsDays).Scan(
		&c.ID, &c.CompanyID, &c.Code, &c.Name, &c.Email, &c.Phone, &c.Address,
		&c.CreditLimit, &c.PaymentTermsDays, &c.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create customer: %w", err)
	}
	return &c, nil
}

func (s *orderService) GetCustomers(ctx context.Context, companyCode string) ([]Customer, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, code, name, email, phone, address, credit_limit, payment_terms_days, created_at
		FROM customers
		WHERE company_id = $1
		ORDER BY code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query customers: %w", err)
	}
	defer rows.Close()

	var customers []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.CompanyID, &c.Code, &c.Name, &c.Email, &c.Phone, &c.Address,
			&c.CreditLimit, &c.PaymentTermsDays, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan customer: %w", err)
		}
		customers = append(customers, c)
	}
	return customers, nil
}

func (s *orderService) CreateProduct(ctx context.Context, companyCode, code, name, description string, unitPrice decimal.Decimal, unit, revenueAccountCode string) (*Product, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	// Verify revenue account exists for this company
	var accountID int
	err = s.pool.QueryRow(ctx, "SELECT id FROM accounts WHERE company_id = $1 AND code = $2", companyID, revenueAccountCode).Scan(&accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("revenue account code %s not found for company %s", revenueAccountCode, companyCode)
		}
		return nil, fmt.Errorf("failed to verify revenue account: %w", err)
	}

	var p Product
	err = s.pool.QueryRow(ctx, `
		INSERT INTO products (company_id, code, name, description, unit_price, unit, revenue_account_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, company_id, code, name, description, unit_price, unit, revenue_account_code, is_active, created_at
	`, companyID, code, name, description, unitPrice, unit, revenueAccountCode).Scan(
		&p.ID, &p.CompanyID, &p.Code, &p.Name, &p.Description,
		&p.UnitPrice, &p.Unit, &p.RevenueAccountCode, &p.IsActive, &p.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create product: %w", err)
	}
	return &p, nil
}

func (s *orderService) GetProducts(ctx context.Context, companyCode string) ([]Product, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, code, name, description, unit_price, unit, revenue_account_code, is_active, created_at
		FROM products
		WHERE company_id = $1 AND is_active = true
		ORDER BY code
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("failed to query products: %w", err)
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.CompanyID, &p.Code, &p.Name, &p.Description,
			&p.UnitPrice, &p.Unit, &p.RevenueAccountCode, &p.IsActive, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan product: %w", err)
		}
		products = append(products, p)
	}
	return products, nil
}

// ── Order Lifecycle ──────────────────────────────────────────────────────────

func (s *orderService) CreateOrder(ctx context.Context, companyCode, customerCode, currency string, exchangeRate decimal.Decimal, orderDate string, lines []OrderLineInput, notes string) (*SalesOrder, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("order must have at least one line")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	companyID, err := s.resolveCompanyID(ctx, tx, companyCode)
	if err != nil {
		return nil, err
	}

	// Resolve customer
	var customerID int
	var customerName string
	err = tx.QueryRow(ctx, "SELECT id, name FROM customers WHERE company_id = $1 AND code = $2", companyID, customerCode).Scan(&customerID, &customerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("customer code %s not found for company %s", customerCode, companyCode)
		}
		return nil, fmt.Errorf("failed to resolve customer: %w", err)
	}

	// Compute order totals from lines
	var totalTransaction decimal.Decimal
	type resolvedLine struct {
		productID            int
		productCode          string
		productName          string
		revenueAccountCode   string
		quantity             decimal.Decimal
		unitPrice            decimal.Decimal
		lineTotalTransaction decimal.Decimal
		lineTotalBase        decimal.Decimal
	}
	var resolved []resolvedLine

	for i, input := range lines {
		var prod Product
		err = tx.QueryRow(ctx,
			"SELECT id, code, name, unit_price, revenue_account_code FROM products WHERE company_id = $1 AND code = $2 AND is_active = true",
			companyID, input.ProductCode,
		).Scan(&prod.ID, &prod.Code, &prod.Name, &prod.UnitPrice, &prod.RevenueAccountCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("line %d: product code %s not found for company %s", i+1, input.ProductCode, companyCode)
			}
			return nil, fmt.Errorf("line %d: failed to resolve product: %w", i+1, err)
		}

		price := prod.UnitPrice
		if !input.UnitPrice.IsZero() {
			price = input.UnitPrice
		}

		lineTotal := input.Quantity.Mul(price)
		lineTotalBase := lineTotal.Mul(exchangeRate)
		totalTransaction = totalTransaction.Add(lineTotal)

		resolved = append(resolved, resolvedLine{
			productID:            prod.ID,
			productCode:          prod.Code,
			productName:          prod.Name,
			revenueAccountCode:   prod.RevenueAccountCode,
			quantity:             input.Quantity,
			unitPrice:            price,
			lineTotalTransaction: lineTotal,
			lineTotalBase:        lineTotalBase,
		})
	}

	totalBase := totalTransaction.Mul(exchangeRate)

	// Insert order header
	var orderID int
	err = tx.QueryRow(ctx, `
		INSERT INTO sales_orders (company_id, customer_id, status, order_date, currency, exchange_rate, total_transaction, total_base, notes)
		VALUES ($1, $2, 'DRAFT', $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, companyID, customerID, orderDate, currency, exchangeRate, totalTransaction, totalBase, notes).Scan(&orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to insert sales order: %w", err)
	}

	// Insert order lines
	for i, rl := range resolved {
		_, err = tx.Exec(ctx, `
			INSERT INTO sales_order_lines (order_id, line_number, product_id, quantity, unit_price, line_total_transaction, line_total_base)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, orderID, i+1, rl.productID, rl.quantity, rl.unitPrice, rl.lineTotalTransaction, rl.lineTotalBase)
		if err != nil {
			return nil, fmt.Errorf("failed to insert order line %d: %w", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit order creation: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

func (s *orderService) ConfirmOrder(ctx context.Context, orderID int, docService DocumentService, inv InventoryService) (*SalesOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock and validate order
	var companyID int
	var status string
	err = tx.QueryRow(ctx,
		"SELECT company_id, status FROM sales_orders WHERE id = $1 FOR UPDATE",
		orderID,
	).Scan(&companyID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("order %d not found", orderID)
		}
		return nil, fmt.Errorf("failed to fetch order %d: %w", orderID, err)
	}
	if status != "DRAFT" {
		return nil, fmt.Errorf("order %d cannot be confirmed: status is %s (must be DRAFT)", orderID, status)
	}

	// Create and post an SO document to assign a gapless order number
	var draftDocID int
	err = tx.QueryRow(ctx, `
		INSERT INTO documents (company_id, type_code, status, financial_year, branch_id)
		VALUES ($1, 'SO', 'DRAFT', NULL, NULL)
		RETURNING id
	`, companyID).Scan(&draftDocID)
	if err != nil {
		return nil, fmt.Errorf("failed to create SO document: %w", err)
	}

	if err = docService.PostDocumentTx(ctx, tx, draftDocID); err != nil {
		return nil, fmt.Errorf("failed to post SO document: %w", err)
	}

	var orderNumber string
	err = tx.QueryRow(ctx, "SELECT document_number FROM documents WHERE id = $1", draftDocID).Scan(&orderNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve SO document number: %w", err)
	}

	// Reserve stock for physical-goods lines (atomic with order confirmation).
	if inv != nil {
		lines, err := s.fetchOrderLinesTx(ctx, tx, orderID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch order lines for stock reservation: %w", err)
		}
		if err := inv.ReserveStockTx(ctx, tx, companyID, orderID, lines); err != nil {
			return nil, fmt.Errorf("stock reservation failed: %w", err)
		}
	}

	// Transition order to CONFIRMED
	_, err = tx.Exec(ctx, `
		UPDATE sales_orders
		SET status = 'CONFIRMED', order_number = $1, confirmed_at = NOW()
		WHERE id = $2
	`, orderNumber, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to confirm order %d: %w", orderID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit order confirmation: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

func (s *orderService) ShipOrder(ctx context.Context, orderID int, inv InventoryService, ledger *Ledger, docService DocumentService) (*SalesOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var companyID int
	var status string
	err = tx.QueryRow(ctx,
		"SELECT company_id, status FROM sales_orders WHERE id = $1 FOR UPDATE",
		orderID,
	).Scan(&companyID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("order %d not found", orderID)
		}
		return nil, fmt.Errorf("failed to fetch order %d: %w", orderID, err)
	}
	if status != "CONFIRMED" {
		return nil, fmt.Errorf("order %d cannot be shipped: status is %s (must be CONFIRMED)", orderID, status)
	}

	// Deduct inventory and book COGS atomically within this TX.
	if inv != nil && ledger != nil {
		lines, err := s.fetchOrderLinesTx(ctx, tx, orderID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch order lines for shipment: %w", err)
		}
		if err := inv.ShipStockTx(ctx, tx, companyID, orderID, lines, ledger, docService); err != nil {
			return nil, fmt.Errorf("inventory shipment failed: %w", err)
		}
	}

	_, err = tx.Exec(ctx,
		"UPDATE sales_orders SET status = 'SHIPPED', shipped_at = NOW() WHERE id = $1",
		orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to ship order %d: %w", orderID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit ship order: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

func (s *orderService) InvoiceOrder(ctx context.Context, orderID int, ledger *Ledger, docService DocumentService) (*SalesOrder, error) {
	// Fetch full order with lines (read-only pre-check, outside the write tx).
	order, err := s.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != "SHIPPED" {
		return nil, fmt.Errorf("order %s cannot be invoiced: status is %s (must be SHIPPED)", order.OrderNumber, order.Status)
	}

	// Resolve company code.
	var companyCode string
	if err = s.pool.QueryRow(ctx, "SELECT company_code FROM companies WHERE id = $1", order.CompanyID).Scan(&companyCode); err != nil {
		return nil, fmt.Errorf("failed to resolve company for order %d: %w", orderID, err)
	}

	// Build accounting proposal: DR AR, CR Revenue per account.
	revenueByAccount := make(map[string]decimal.Decimal)
	for _, line := range order.Lines {
		revenueByAccount[line.RevenueAccountCode] = revenueByAccount[line.RevenueAccountCode].Add(line.LineTotalTransaction)
	}

	arAccount, err := s.ruleEngine.ResolveAccount(ctx, order.CompanyID, "AR")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve AR account for invoicing: %w", err)
	}

	var proposalLines []ProposalLine
	proposalLines = append(proposalLines, ProposalLine{
		AccountCode: arAccount,
		IsDebit:     true,
		Amount:      order.TotalTransaction.String(),
	})
	for accountCode, amount := range revenueByAccount {
		proposalLines = append(proposalLines, ProposalLine{
			AccountCode: accountCode,
			IsDebit:     false,
			Amount:      amount.String(),
		})
	}

	today := time.Now().Format("2006-01-02")
	proposal := Proposal{
		DocumentTypeCode:    "SI",
		CompanyCode:         companyCode,
		IdempotencyKey:      fmt.Sprintf("invoice-order-%d", orderID),
		TransactionCurrency: order.Currency,
		ExchangeRate:        order.ExchangeRate.String(),
		Summary:             fmt.Sprintf("Sales Invoice for order %s — %s", order.OrderNumber, order.CustomerName),
		PostingDate:         today,
		DocumentDate:        order.OrderDate,
		Confidence:          1.0,
		Reasoning:           fmt.Sprintf("Automatically generated invoice for confirmed and shipped sales order %s.", order.OrderNumber),
		Lines:               proposalLines,
	}

	// Wrap ledger commit and status update in one transaction — atomic.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin invoice tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
		return nil, fmt.Errorf("failed to commit invoice journal entry for order %s: %w", order.OrderNumber, err)
	}

	// Fetch the SI document ID created inside the same tx.
	var invoiceDocID *int
	_ = tx.QueryRow(ctx, `
		SELECT d.id
		FROM documents d
		JOIN journal_entries je ON je.reference_id = d.document_number AND je.reference_type = 'DOCUMENT'
		WHERE je.idempotency_key = $1
		LIMIT 1
	`, fmt.Sprintf("invoice-order-%d", orderID)).Scan(&invoiceDocID)

	if _, err = tx.Exec(ctx, `
		UPDATE sales_orders
		SET status = 'INVOICED', invoiced_at = NOW(), invoice_document_id = $1
		WHERE id = $2
	`, invoiceDocID, orderID); err != nil {
		return nil, fmt.Errorf("failed to mark order %d as INVOICED: %w", orderID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit invoice tx: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

func (s *orderService) RecordPayment(ctx context.Context, orderID int, bankAccountCode string, paymentDate string, ledger *Ledger) error {
	if bankAccountCode == "" {
		bankAccountCode = defaultBankAccountCode
	}

	order, err := s.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.Status != "INVOICED" {
		return fmt.Errorf("order %s cannot record payment: status is %s (must be INVOICED)", order.OrderNumber, order.Status)
	}

	var companyCode string
	if err = s.pool.QueryRow(ctx, "SELECT company_code FROM companies WHERE id = $1", order.CompanyID).Scan(&companyCode); err != nil {
		return fmt.Errorf("failed to resolve company for order %d: %w", orderID, err)
	}

	if paymentDate == "" {
		paymentDate = time.Now().Format("2006-01-02")
	}

	arAccount, err := s.ruleEngine.ResolveAccount(ctx, order.CompanyID, "AR")
	if err != nil {
		return fmt.Errorf("failed to resolve AR account for payment: %w", err)
	}

	proposal := Proposal{
		DocumentTypeCode:    "JE",
		CompanyCode:         companyCode,
		IdempotencyKey:      fmt.Sprintf("payment-order-%d", orderID),
		TransactionCurrency: order.Currency,
		ExchangeRate:        order.ExchangeRate.String(),
		Summary:             fmt.Sprintf("Payment received from %s for order %s", order.CustomerName, order.OrderNumber),
		PostingDate:         paymentDate,
		DocumentDate:        paymentDate,
		Confidence:          1.0,
		Reasoning:           fmt.Sprintf("Customer payment for sales order %s.", order.OrderNumber),
		Lines: []ProposalLine{
			{AccountCode: bankAccountCode, IsDebit: true, Amount: order.TotalTransaction.String()},
			{AccountCode: arAccount, IsDebit: false, Amount: order.TotalTransaction.String()},
		},
	}

	// Wrap ledger commit and status update in one transaction — atomic.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin payment tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := ledger.CommitInTx(ctx, tx, proposal); err != nil {
		return fmt.Errorf("failed to commit payment journal entry for order %s: %w", order.OrderNumber, err)
	}

	if _, err = tx.Exec(ctx,
		"UPDATE sales_orders SET status = 'PAID', paid_at = NOW() WHERE id = $1",
		orderID,
	); err != nil {
		return fmt.Errorf("failed to mark order %d as PAID: %w", orderID, err)
	}

	return tx.Commit(ctx)
}

func (s *orderService) CancelOrder(ctx context.Context, orderID int, inv InventoryService) (*SalesOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx,
		"SELECT status FROM sales_orders WHERE id = $1 FOR UPDATE",
		orderID,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("order %d not found", orderID)
		}
		return nil, fmt.Errorf("failed to fetch order %d: %w", orderID, err)
	}
	if status != "DRAFT" {
		return nil, fmt.Errorf("order %d cannot be cancelled: status is %s (only DRAFT orders can be cancelled)", orderID, status)
	}

	// Release any stock reservations (applicable when a CONFIRMED order would be cancelled;
	// DRAFT orders have no reservations, so this is a no-op for standard cancellations).
	if inv != nil {
		if err := inv.ReleaseReservationTx(ctx, tx, orderID); err != nil {
			return nil, fmt.Errorf("failed to release stock reservation: %w", err)
		}
	}

	_, err = tx.Exec(ctx,
		"UPDATE sales_orders SET status = 'CANCELLED' WHERE id = $1",
		orderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel order %d: %w", orderID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit cancel order: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

// ── Queries ──────────────────────────────────────────────────────────────────

func (s *orderService) GetOrder(ctx context.Context, orderID int) (*SalesOrder, error) {
	var o SalesOrder
	err := s.pool.QueryRow(ctx, `
		SELECT so.id, so.company_id, COALESCE(so.order_number, ''), c.code, c.name,
		       so.status, so.order_date::text, so.currency, so.exchange_rate,
		       so.total_transaction, so.total_base, so.notes, so.invoice_document_id,
		       so.created_at, so.confirmed_at, so.shipped_at, so.invoiced_at, so.paid_at,
		       so.customer_id
		FROM sales_orders so
		JOIN customers c ON c.id = so.customer_id
		WHERE so.id = $1
	`, orderID).Scan(
		&o.ID, &o.CompanyID, &o.OrderNumber, &o.CustomerCode, &o.CustomerName,
		&o.Status, &o.OrderDate, &o.Currency, &o.ExchangeRate,
		&o.TotalTransaction, &o.TotalBase, &o.Notes, &o.InvoiceDocumentID,
		&o.CreatedAt, &o.ConfirmedAt, &o.ShippedAt, &o.InvoicedAt, &o.PaidAt,
		&o.CustomerID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("order %d not found", orderID)
		}
		return nil, fmt.Errorf("failed to fetch order %d: %w", orderID, err)
	}

	lines, err := s.fetchOrderLines(ctx, orderID)
	if err != nil {
		return nil, err
	}
	o.Lines = lines
	return &o, nil
}

func (s *orderService) GetOrderByNumber(ctx context.Context, companyCode, orderNumber string) (*SalesOrder, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	var orderID int
	err = s.pool.QueryRow(ctx,
		"SELECT id FROM sales_orders WHERE company_id = $1 AND order_number = $2",
		companyID, orderNumber,
	).Scan(&orderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("order %s not found for company %s", orderNumber, companyCode)
		}
		return nil, fmt.Errorf("failed to lookup order by number: %w", err)
	}

	return s.GetOrder(ctx, orderID)
}

func (s *orderService) GetOrders(ctx context.Context, companyCode string, status *string) ([]SalesOrder, error) {
	companyID, err := s.resolveCompanyID(ctx, s.pool, companyCode)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT so.id, so.company_id, COALESCE(so.order_number, ''), c.code, c.name,
		       so.status, so.order_date::text, so.currency, so.exchange_rate,
		       so.total_transaction, so.total_base, so.notes, so.invoice_document_id,
		       so.created_at, so.confirmed_at, so.shipped_at, so.invoiced_at, so.paid_at,
		       so.customer_id
		FROM sales_orders so
		JOIN customers c ON c.id = so.customer_id
		WHERE so.company_id = $1
	`
	args := []any{companyID}

	if status != nil {
		query += " AND so.status = $2"
		args = append(args, *status)
	}
	query += " ORDER BY so.id DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query orders: %w", err)
	}
	defer rows.Close()

	var orders []SalesOrder
	for rows.Next() {
		var o SalesOrder
		if err := rows.Scan(
			&o.ID, &o.CompanyID, &o.OrderNumber, &o.CustomerCode, &o.CustomerName,
			&o.Status, &o.OrderDate, &o.Currency, &o.ExchangeRate,
			&o.TotalTransaction, &o.TotalBase, &o.Notes, &o.InvoiceDocumentID,
			&o.CreatedAt, &o.ConfirmedAt, &o.ShippedAt, &o.InvoicedAt, &o.PaidAt,
			&o.CustomerID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, nil
}

func (s *orderService) fetchOrderLines(ctx context.Context, orderID int) ([]SalesOrderLine, error) {
	return fetchOrderLinesQ(ctx, s.pool, orderID)
}

// fetchOrderLinesTx fetches order lines within an existing TX.
func (s *orderService) fetchOrderLinesTx(ctx context.Context, tx pgx.Tx, orderID int) ([]SalesOrderLine, error) {
	return fetchOrderLinesQ(ctx, tx, orderID)
}

// pgxRowQuerier is satisfied by both *pgxpool.Pool and pgx.Tx (for Query).
type pgxRowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func fetchOrderLinesQ(ctx context.Context, q pgxRowQuerier, orderID int) ([]SalesOrderLine, error) {
	rows, err := q.Query(ctx, `
		SELECT sol.id, sol.order_id, sol.line_number,
		       p.id, p.code, p.name, p.revenue_account_code,
		       sol.quantity, sol.unit_price, sol.line_total_transaction, sol.line_total_base
		FROM sales_order_lines sol
		JOIN products p ON p.id = sol.product_id
		WHERE sol.order_id = $1
		ORDER BY sol.line_number
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to query order lines: %w", err)
	}
	defer rows.Close()

	var lines []SalesOrderLine
	for rows.Next() {
		var l SalesOrderLine
		if err := rows.Scan(
			&l.ID, &l.OrderID, &l.LineNumber,
			&l.ProductID, &l.ProductCode, &l.ProductName, &l.RevenueAccountCode,
			&l.Quantity, &l.UnitPrice, &l.LineTotalTransaction, &l.LineTotalBase,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}
