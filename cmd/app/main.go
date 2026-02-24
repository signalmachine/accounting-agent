package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"accounting-agent/internal/ai"
	"accounting-agent/internal/core"
	"accounting-agent/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	orderService := core.NewOrderService(pool)
	inventoryService := core.NewInventoryService(pool)
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY is not set")
	}
	agent := ai.NewAgent(apiKey)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "propose", "prop", "p":
			if len(os.Args) < 3 {
				log.Fatal("Usage: app propose \"<event description>\"")
			}
			event := os.Args[2]
			coa, err := fetchCoA(ctx, pool)
			if err != nil {
				log.Fatalf("Failed to fetch inputs: %v", err)
			}
			company, err := loadDefaultCompany(ctx, pool)
			if err != nil {
				log.Fatalf("Failed to load company context: %v", err)
			}
			documentTypes, err := fetchDocumentTypes(ctx, pool)
			if err != nil {
				log.Fatalf("Failed to fetch document types: %v", err)
			}
			response, err := agent.InterpretEvent(ctx, event, coa, documentTypes, company)
			if err != nil {
				log.Fatalf("Agent error: %v", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(response)

		case "validate", "val", "v":
			var proposal core.Proposal
			if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
				log.Fatalf("Invalid JSON: %v", err)
			}
			if err := ledger.Validate(ctx, proposal); err != nil {
				log.Fatalf("Validation failed: %v", err)
			}

		case "commit", "com", "c":
			var proposal core.Proposal
			if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
				log.Fatalf("Invalid JSON: %v", err)
			}
			if err := ledger.Commit(ctx, proposal); err != nil {
				log.Fatalf("Commit failed: %v", err)
			}
			fmt.Println("Transaction Committed.")

		case "bal", "balances":
			company, err := loadDefaultCompany(ctx, pool)
			if err != nil {
				log.Fatalf("Failed to load company: %v", err)
			}
			printBalances(ctx, pool, ledger, company.CompanyCode)

		default:
			log.Fatalf("Unknown command: %s", os.Args[1])
		}
	} else {
		company, err := loadDefaultCompany(ctx, pool)
		if err != nil {
			log.Fatalf("Failed to load company for REPL: %v", err)
		}
		runREPL(ctx, agent, ledger, docService, orderService, inventoryService, pool, company)
	}
}

func loadDefaultCompany(ctx context.Context, pool *pgxpool.Pool) (*core.Company, error) {
	c := &core.Company{}
	err := pool.QueryRow(ctx, "SELECT id, company_code, name, base_currency FROM companies LIMIT 1").Scan(&c.ID, &c.CompanyCode, &c.Name, &c.BaseCurrency)
	if err != nil {
		return nil, fmt.Errorf("no default company found, have migrations run?: %w", err)
	}
	return c, nil
}

func fetchCoA(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	rows, err := pool.Query(ctx, "SELECT code, name, type FROM accounts ORDER BY code")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var code, name, reqType string
		if err := rows.Scan(&code, &name, &reqType); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("- %s %s (%s)", code, name, reqType))
	}
	return strings.Join(lines, "\n"), nil
}

func fetchDocumentTypes(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	rows, err := pool.Query(ctx, "SELECT code, name FROM document_types")
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var code, name string
		if err := rows.Scan(&code, &name); err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", code, name))
	}
	return strings.Join(lines, "\n"), nil
}

// resolveOrderByRef looks up a sales order by its order_number or numeric ID.
func resolveOrderByRef(ctx context.Context, orderService core.OrderService, company *core.Company, ref string) (*core.SalesOrder, error) {
	// Try numeric ID first
	if id, err := strconv.Atoi(ref); err == nil {
		return orderService.GetOrder(ctx, id)
	}
	// Try order number
	return orderService.GetOrderByNumber(ctx, company.CompanyCode, ref)
}

func runREPL(ctx context.Context, agent *ai.Agent, ledger *core.Ledger, docService core.DocumentService, orderService core.OrderService, inventoryService core.InventoryService, pool *pgxpool.Pool, company *core.Company) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Accounting Agent")
	fmt.Printf("Company: %s — %s (%s)\n", company.CompanyCode, company.Name, company.BaseCurrency)
	fmt.Println("Describe a business event to post a journal entry, or use /help for commands.")
	fmt.Println(strings.Repeat("-", 70))

	var errExit = fmt.Errorf("exit")

	// dispatchSlash handles all /command [args...] input deterministically.
	dispatchSlash := func(input string) error {
		tokens := strings.Fields(strings.TrimPrefix(input, "/"))
		if len(tokens) == 0 {
			return nil
		}
		cmd := strings.ToLower(tokens[0])
		args := tokens[1:]

		switch cmd {
		case "bal", "balances":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			printBalances(ctx, pool, ledger, code)

		case "customers":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			customers, err := orderService.GetCustomers(ctx, code)
			if err != nil {
				return err
			}
			printCustomers(customers, code)

		case "products":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			products, err := orderService.GetProducts(ctx, code)
			if err != nil {
				return err
			}
			printProducts(products, code)

		case "orders":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			orders, err := orderService.GetOrders(ctx, code, nil)
			if err != nil {
				return err
			}
			printOrders(orders, code)

		case "new-order":
			if len(args) < 1 {
				fmt.Println("Usage: /new-order <customer-code>")
				return nil
			}
			handleNewOrder(ctx, reader, orderService, docService, company, args[0])

		case "confirm":
			if len(args) < 1 {
				fmt.Println("Usage: /confirm <order-ref>")
				return nil
			}
			order, err := resolveOrderByRef(ctx, orderService, company, args[0])
			if err != nil {
				return err
			}
			order, err = orderService.ConfirmOrder(ctx, order.ID, docService, inventoryService)
			if err != nil {
				return err
			}
			fmt.Printf("Order CONFIRMED. Number: %s\n", order.OrderNumber)

		case "ship":
			if len(args) < 1 {
				fmt.Println("Usage: /ship <order-ref>")
				return nil
			}
			order, err := resolveOrderByRef(ctx, orderService, company, args[0])
			if err != nil {
				return err
			}
			order, err = orderService.ShipOrder(ctx, order.ID, inventoryService, ledger, docService)
			if err != nil {
				return err
			}
			fmt.Printf("Order %s marked as SHIPPED. COGS booked if applicable.\n", order.OrderNumber)

		case "invoice":
			if len(args) < 1 {
				fmt.Println("Usage: /invoice <order-ref>")
				return nil
			}
			order, err := resolveOrderByRef(ctx, orderService, company, args[0])
			if err != nil {
				return err
			}
			order, err = orderService.InvoiceOrder(ctx, order.ID, ledger, docService)
			if err != nil {
				return err
			}
			fmt.Printf("Order %s INVOICED. Journal entry committed (DR AR, CR Revenue).\n", order.OrderNumber)

		case "payment":
			if len(args) < 1 {
				fmt.Println("Usage: /payment <order-ref> [bank-account-code]")
				return nil
			}
			bankCode := "1100"
			if len(args) >= 2 {
				bankCode = args[1]
			}
			order, err := resolveOrderByRef(ctx, orderService, company, args[0])
			if err != nil {
				return err
			}
			if err = orderService.RecordPayment(ctx, order.ID, bankCode, "", ledger); err != nil {
				return err
			}
			fmt.Printf("Payment recorded for order %s. Status: PAID.\n", order.OrderNumber)

		case "warehouses":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			warehouses, err := inventoryService.GetWarehouses(ctx, code)
			if err != nil {
				return err
			}
			printWarehouses(warehouses, code)

		case "stock":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			levels, err := inventoryService.GetStockLevels(ctx, code)
			if err != nil {
				return err
			}
			printStockLevels(levels, code)

		case "receive":
			// Usage: /receive <product-code> <qty> <unit-cost> [credit-account]
			if len(args) < 3 {
				fmt.Println("Usage: /receive <product-code> <qty> <unit-cost> [credit-account]")
				fmt.Println("  Receives stock into the default warehouse.")
				fmt.Println("  Defaults: credit-account = 2000 (Accounts Payable)")
				return nil
			}
			productCode := strings.ToUpper(args[0])
			qty, err := decimal.NewFromString(args[1])
			if err != nil || qty.IsNegative() || qty.IsZero() {
				fmt.Printf("Invalid quantity: %s\n", args[1])
				return nil
			}
			unitCost, err := decimal.NewFromString(args[2])
			if err != nil || unitCost.IsNegative() {
				fmt.Printf("Invalid unit cost: %s\n", args[2])
				return nil
			}
			creditAccount := "2000"
			if len(args) >= 4 {
				creditAccount = args[3]
			}
			today := time.Now().Format("2006-01-02")
			defaultWH, err := inventoryService.GetDefaultWarehouse(ctx, company.CompanyCode)
			if err != nil {
				return fmt.Errorf("no active warehouse found: %w", err)
			}
			if err := inventoryService.ReceiveStock(ctx, company.CompanyCode, defaultWH.Code, productCode,
				qty, unitCost, today, creditAccount, ledger, docService); err != nil {
				return err
			}
			fmt.Printf("Received %s units of %s @ %s into warehouse %s. DR 1400 Inventory, CR %s.\n",
				qty.String(), productCode, unitCost.String(), defaultWH.Code, creditAccount)

		case "help", "h":
			printHelp()

		case "exit", "quit", "e", "q":
			return errExit

		default:
			fmt.Printf("Unknown command: /%s  (type /help for all commands)\n", cmd)
		}
		return nil
	}

	for {
		fmt.Print("\n> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Slash prefix → deterministic command dispatcher, no AI invoked.
		if strings.HasPrefix(input, "/") {
			if err := dispatchSlash(input); err != nil {
				if err == errExit {
					fmt.Println("Goodbye!")
					break
				}
				fmt.Printf("Error: %v\n", err)
			}
			continue
		}

		// No slash prefix → always route to AI agent for journal entry proposals.
		fmt.Println("[AI] Processing...")
		accumulatedInput := input

		for {
			coa, err := fetchCoA(ctx, pool)
			if err != nil {
				fmt.Printf("Error fetching accounts: %v\n", err)
				break
			}
			documentTypes, err := fetchDocumentTypes(ctx, pool)
			if err != nil {
				fmt.Printf("Error fetching document types: %v\n", err)
				break
			}

			response, err := agent.InterpretEvent(ctx, accumulatedInput, coa, documentTypes, company)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				break
			}

			if response.IsClarificationRequest {
				fmt.Printf("\n[AI]: %s\n", response.Clarification.Message)
				fmt.Print("> ")
				userFollowUp, _ := reader.ReadString('\n')
				userFollowUp = strings.TrimSpace(userFollowUp)
				if userFollowUp == "" || strings.ToLower(userFollowUp) == "cancel" {
					fmt.Println("Cancelled.")
					break
				}
				accumulatedInput = fmt.Sprintf("Original Event: %s\nClarification requested: %s\nUser response: %s",
					accumulatedInput, response.Clarification.Message, userFollowUp)
				fmt.Println("[AI] Thinking...")
				continue
			}

			proposal := response.Proposal
			printProposal(proposal)

			if proposal.Confidence < 0.6 {
				fmt.Println("\nWARNING: Low confidence proposal.")
			}

			fmt.Print("\nApprove this transaction? (y/n): ")
			choice, _ := reader.ReadString('\n')
			choice = strings.TrimSpace(strings.ToLower(choice))

			if choice == "y" || choice == "yes" {
				if err := ledger.Commit(ctx, *proposal); err != nil {
					fmt.Printf("Transaction FAILED: %v\n", err)
				} else {
					fmt.Println("Transaction COMMITTED.")
				}
			} else {
				fmt.Println("Transaction Cancelled.")
			}
			break
		}
	}
}

func printHelp() {
	fmt.Println()
	fmt.Println("ACCOUNTING AGENT — COMMANDS")
	fmt.Println(strings.Repeat("=", 62))
	fmt.Println()
	fmt.Println("  LEDGER")
	fmt.Println("  /bal [company-code]             Trial balance")
	fmt.Println("  /balances [company-code]         Alias for /bal")
	fmt.Println()
	fmt.Println("  MASTER DATA")
	fmt.Println("  /customers [company-code]        List customers")
	fmt.Println("  /products  [company-code]        List products")
	fmt.Println()
	fmt.Println("  SALES ORDERS")
	fmt.Println("  /orders    [company-code]        List orders")
	fmt.Println("  /new-order <customer-code>       Create order (interactive)")
	fmt.Println("  /confirm   <order-ref>           Confirm DRAFT → assign SO number + reserve stock")
	fmt.Println("  /ship      <order-ref>           Mark as SHIPPED + deduct inventory + book COGS")
	fmt.Println("  /invoice   <order-ref>           Post sales invoice + journal entry")
	fmt.Println("  /payment   <order-ref> [bank]    Record payment (DR Bank, CR AR)")
	fmt.Println()
	fmt.Println("  INVENTORY")
	fmt.Println("  /warehouses [company-code]       List warehouses")
	fmt.Println("  /stock      [company-code]       View stock levels (on hand / reserved / available)")
	fmt.Println("  /receive <product> <qty> <cost>  Receive stock → DR Inventory, CR AP (default)")
	fmt.Println()
	fmt.Println("  SESSION")
	fmt.Println("  /help                            Show this help")
	fmt.Println("  /exit                            Exit")
	fmt.Println()
	fmt.Println("  AGENT MODE  (no / prefix)")
	fmt.Println("  Type any business event in natural language.")
	fmt.Println("  Example: \"record $5000 payment received from Acme Corp\"")
	fmt.Println(strings.Repeat("=", 62))
}

// handleNewOrder runs an interactive order creation session in the REPL.
func handleNewOrder(ctx context.Context, reader *bufio.Reader, orderService core.OrderService, docService core.DocumentService, company *core.Company, customerCode string) {
	fmt.Printf("Creating order for customer: %s\n", customerCode)
	fmt.Println("Enter order lines. Type 'done' when finished, 'cancel' to abort.")
	fmt.Println("Format per line: <product-code> <quantity> [unit-price]")
	fmt.Println("  Example: P001 10")
	fmt.Println("  Example: P001 5 450.00   (overrides product default price)")

	var lines []core.OrderLineInput
	lineNum := 1
	for {
		fmt.Printf("  Line %d: ", lineNum)
		raw, _ := reader.ReadString('\n')
		raw = strings.TrimSpace(raw)
		if strings.ToLower(raw) == "cancel" {
			fmt.Println("Order creation cancelled.")
			return
		}
		if strings.ToLower(raw) == "done" {
			break
		}
		if raw == "" {
			continue
		}

		parts := strings.Fields(raw)
		if len(parts) < 2 {
			fmt.Println("  Invalid format. Use: <product-code> <quantity> [unit-price]")
			continue
		}

		qty, err := decimal.NewFromString(parts[1])
		if err != nil || qty.IsNegative() || qty.IsZero() {
			fmt.Println("  Invalid quantity.")
			continue
		}

		var price decimal.Decimal
		if len(parts) >= 3 {
			price, err = decimal.NewFromString(parts[2])
			if err != nil || price.IsNegative() {
				fmt.Println("  Invalid price.")
				continue
			}
		}

		lines = append(lines, core.OrderLineInput{
			ProductCode: strings.ToUpper(parts[0]),
			Quantity:    qty,
			UnitPrice:   price,
		})
		lineNum++
	}

	if len(lines) == 0 {
		fmt.Println("No lines entered. Order not created.")
		return
	}

	fmt.Print("Order date (YYYY-MM-DD, leave blank for today): ")
	dateInput, _ := reader.ReadString('\n')
	dateInput = strings.TrimSpace(dateInput)
	today := dateInput
	if today == "" {
		today = time.Now().Format("2006-01-02")
	}

	fmt.Print("Notes (optional): ")
	notes, _ := reader.ReadString('\n')
	notes = strings.TrimSpace(notes)

	fmt.Print("Currency [INR]: ")
	currency, _ := reader.ReadString('\n')
	currency = strings.TrimSpace(strings.ToUpper(currency))
	if currency == "" {
		currency = company.BaseCurrency
	}

	order, err := orderService.CreateOrder(ctx, company.CompanyCode, customerCode, currency, decimal.NewFromFloat(1.0), today, lines, notes)
	if err != nil {
		fmt.Printf("[REPL] Error creating order: %v\n", err)
		return
	}

	fmt.Printf("\nOrder created (ID: %d, Status: DRAFT)\n", order.ID)
	printOrderDetail(order)
	fmt.Println("Use '/confirm <id>' to assign an order number.")
}

// ── Display helpers ───────────────────────────────────────────────────────────

func printBalances(ctx context.Context, pool *pgxpool.Pool, ledger *core.Ledger, companyCode string) {
	var companyName, baseCurrency string
	err := pool.QueryRow(ctx,
		"SELECT name, base_currency FROM companies WHERE company_code = $1",
		companyCode,
	).Scan(&companyName, &baseCurrency)
	if err != nil {
		log.Printf("Failed to load company details for %s: %v", companyCode, err)
		companyName = companyCode
		baseCurrency = "???"
	}

	balances, err := ledger.GetBalances(ctx, companyCode)
	if err != nil {
		log.Printf("Failed to get balances: %v", err)
		return
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 62))
	fmt.Printf("  %-58s\n", "TRIAL BALANCE")
	fmt.Printf("  Company  : %s — %s\n", companyCode, companyName)
	fmt.Printf("  Currency : %s\n", baseCurrency)
	fmt.Println(strings.Repeat("=", 62))
	fmt.Printf("  %-10s %-30s %15s\n", "CODE", "NAME", "BALANCE")
	fmt.Println(strings.Repeat("-", 62))
	for _, b := range balances {
		fmt.Printf("  %-10s %-30s %15s\n", b.Code, b.Name, b.Balance.StringFixed(2))
	}
	fmt.Println(strings.Repeat("=", 62))
}

func printProposal(p *core.Proposal) {
	fmt.Printf("\nSUMMARY:    %s\n", p.Summary)
	fmt.Printf("DOC TYPE:   %s\n", p.DocumentTypeCode)
	fmt.Printf("COMPANY:    %s\n", p.CompanyCode)
	fmt.Printf("CURRENCY:   %s @ rate %s\n", p.TransactionCurrency, p.ExchangeRate)
	fmt.Printf("REASONING:  %s\n", p.Reasoning)
	fmt.Printf("CONFIDENCE: %.2f\n", p.Confidence)
	fmt.Println("ENTRIES:")
	for _, l := range p.Lines {
		dOrC := "CR"
		if l.IsDebit {
			dOrC = "DR"
		}
		fmt.Printf("  [%s] Account %-8s  %s %s\n", dOrC, l.AccountCode, l.Amount, p.TransactionCurrency)
	}
}

func printCustomers(customers []core.Customer, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  CUSTOMERS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 72))
	if len(customers) == 0 {
		fmt.Println("  No customers found.")
		fmt.Println(strings.Repeat("=", 72))
		return
	}
	fmt.Printf("  %-8s %-25s %-15s %12s  %s\n", "CODE", "NAME", "TERMS", "CREDIT LIMIT", "EMAIL")
	fmt.Println(strings.Repeat("-", 72))
	for _, c := range customers {
		fmt.Printf("  %-8s %-25s %12d days %12s  %s\n",
			c.Code, c.Name, c.PaymentTermsDays, c.CreditLimit.StringFixed(2), c.Email)
	}
	fmt.Println(strings.Repeat("=", 72))
}

func printProducts(products []core.Product, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  PRODUCTS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 72))
	if len(products) == 0 {
		fmt.Println("  No products found.")
		fmt.Println(strings.Repeat("=", 72))
		return
	}
	fmt.Printf("  %-8s %-28s %-6s %12s  %s\n", "CODE", "NAME", "UNIT", "UNIT PRICE", "REVENUE A/C")
	fmt.Println(strings.Repeat("-", 72))
	for _, p := range products {
		fmt.Printf("  %-8s %-28s %-6s %12s  %s\n",
			p.Code, p.Name, p.Unit, p.UnitPrice.StringFixed(2), p.RevenueAccountCode)
	}
	fmt.Println(strings.Repeat("=", 72))
}

func printOrders(orders []core.SalesOrder, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("  SALES ORDERS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 80))
	if len(orders) == 0 {
		fmt.Println("  No orders found.")
		fmt.Println(strings.Repeat("=", 80))
		return
	}
	fmt.Printf("  %-5s %-24s %-20s %-12s %12s  %s\n", "ID", "ORDER NO", "CUSTOMER", "STATUS", "TOTAL", "DATE")
	fmt.Println(strings.Repeat("-", 80))
	for _, o := range orders {
		orderNo := o.OrderNumber
		if orderNo == "" {
			orderNo = "(draft)"
		}
		fmt.Printf("  %-5d %-24s %-20s %-12s %12s  %s\n",
			o.ID, orderNo, o.CustomerName, o.Status, o.TotalTransaction.StringFixed(2), o.OrderDate)
	}
	fmt.Println(strings.Repeat("=", 80))
}

func printOrderDetail(o *core.SalesOrder) {
	orderNo := o.OrderNumber
	if orderNo == "" {
		orderNo = fmt.Sprintf("(ID: %d, DRAFT)", o.ID)
	}
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("  Order:     %s\n", orderNo)
	fmt.Printf("  Customer:  %s (%s)\n", o.CustomerName, o.CustomerCode)
	fmt.Printf("  Status:    %s\n", o.Status)
	fmt.Printf("  Date:      %s\n", o.OrderDate)
	fmt.Printf("  Currency:  %s\n", o.Currency)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("  %-5s %-25s %8s %12s %12s\n", "LINE", "PRODUCT", "QTY", "UNIT PRICE", "TOTAL")
	fmt.Println(strings.Repeat("-", 60))
	for _, l := range o.Lines {
		fmt.Printf("  %-5d %-25s %8s %12s %12s\n",
			l.LineNumber, l.ProductName,
			l.Quantity.StringFixed(2),
			l.UnitPrice.StringFixed(2),
			l.LineTotalTransaction.StringFixed(2),
		)
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("  %-43s %12s\n", "TOTAL", o.TotalTransaction.StringFixed(2))
	fmt.Println(strings.Repeat("-", 60))
}

func printWarehouses(warehouses []core.Warehouse, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  WAREHOUSES — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 60))
	if len(warehouses) == 0 {
		fmt.Println("  No warehouses found.")
		fmt.Println(strings.Repeat("=", 60))
		return
	}
	fmt.Printf("  %-10s %-40s\n", "CODE", "NAME")
	fmt.Println(strings.Repeat("-", 60))
	for _, w := range warehouses {
		fmt.Printf("  %-10s %-40s\n", w.Code, w.Name)
	}
	fmt.Println(strings.Repeat("=", 60))
}

func printStockLevels(levels []core.StockLevel, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("  STOCK LEVELS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 80))
	if len(levels) == 0 {
		fmt.Println("  No stock records found.")
		fmt.Println(strings.Repeat("=", 80))
		return
	}
	fmt.Printf("  %-8s %-22s %-8s %10s %10s %10s %10s\n",
		"CODE", "PRODUCT", "WH", "ON HAND", "RESERVED", "AVAILABLE", "UNIT COST")
	fmt.Println(strings.Repeat("-", 80))
	for _, sl := range levels {
		fmt.Printf("  %-8s %-22s %-8s %10s %10s %10s %10s\n",
			sl.ProductCode,
			sl.ProductName,
			sl.WarehouseCode,
			sl.OnHand.StringFixed(2),
			sl.Reserved.StringFixed(2),
			sl.Available.StringFixed(2),
			sl.UnitCost.StringFixed(2),
		)
	}
	fmt.Println(strings.Repeat("=", 80))
}
