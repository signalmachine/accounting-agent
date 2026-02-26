package repl

import (
	"fmt"
	"strings"

	"accounting-agent/internal/app"
	"accounting-agent/internal/core"
)

func printBalances(result *app.TrialBalanceResult) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 62))
	fmt.Printf("  %-58s\n", "TRIAL BALANCE")
	fmt.Printf("  Company  : %s — %s\n", result.CompanyCode, result.CompanyName)
	fmt.Printf("  Currency : %s\n", result.Currency)
	fmt.Println(strings.Repeat("=", 62))
	fmt.Printf("  %-10s %-30s %15s\n", "CODE", "NAME", "BALANCE")
	fmt.Println(strings.Repeat("-", 62))
	for _, b := range result.Accounts {
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

func printCustomers(result *app.CustomerListResult, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  CUSTOMERS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 72))
	if len(result.Customers) == 0 {
		fmt.Println("  No customers found.")
		fmt.Println(strings.Repeat("=", 72))
		return
	}
	fmt.Printf("  %-8s %-25s %-15s %12s  %s\n", "CODE", "NAME", "TERMS", "CREDIT LIMIT", "EMAIL")
	fmt.Println(strings.Repeat("-", 72))
	for _, c := range result.Customers {
		fmt.Printf("  %-8s %-25s %12d days %12s  %s\n",
			c.Code, c.Name, c.PaymentTermsDays, c.CreditLimit.StringFixed(2), c.Email)
	}
	fmt.Println(strings.Repeat("=", 72))
}

func printProducts(result *app.ProductListResult, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 72))
	fmt.Printf("  PRODUCTS — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 72))
	if len(result.Products) == 0 {
		fmt.Println("  No products found.")
		fmt.Println(strings.Repeat("=", 72))
		return
	}
	fmt.Printf("  %-8s %-28s %-6s %12s  %s\n", "CODE", "NAME", "UNIT", "UNIT PRICE", "REVENUE A/C")
	fmt.Println(strings.Repeat("-", 72))
	for _, p := range result.Products {
		fmt.Printf("  %-8s %-28s %-6s %12s  %s\n",
			p.Code, p.Name, p.Unit, p.UnitPrice.StringFixed(2), p.RevenueAccountCode)
	}
	fmt.Println(strings.Repeat("=", 72))
}

func printOrders(result *app.OrderListResult) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("  SALES ORDERS — Company %s\n", result.CompanyCode)
	fmt.Println(strings.Repeat("=", 80))
	if len(result.Orders) == 0 {
		fmt.Println("  No orders found.")
		fmt.Println(strings.Repeat("=", 80))
		return
	}
	fmt.Printf("  %-5s %-24s %-20s %-12s %12s  %s\n", "ID", "ORDER NO", "CUSTOMER", "STATUS", "TOTAL", "DATE")
	fmt.Println(strings.Repeat("-", 80))
	for _, o := range result.Orders {
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

func printWarehouses(result *app.WarehouseListResult, companyCode string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("  WAREHOUSES — Company %s\n", companyCode)
	fmt.Println(strings.Repeat("=", 60))
	if len(result.Warehouses) == 0 {
		fmt.Println("  No warehouses found.")
		fmt.Println(strings.Repeat("=", 60))
		return
	}
	fmt.Printf("  %-10s %-40s\n", "CODE", "NAME")
	fmt.Println(strings.Repeat("-", 60))
	for _, w := range result.Warehouses {
		fmt.Printf("  %-10s %-40s\n", w.Code, w.Name)
	}
	fmt.Println(strings.Repeat("=", 60))
}

func printStockLevels(result *app.StockResult) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("  STOCK LEVELS — Company %s\n", result.CompanyCode)
	fmt.Println(strings.Repeat("=", 80))
	if len(result.Levels) == 0 {
		fmt.Println("  No stock records found.")
		fmt.Println(strings.Repeat("=", 80))
		return
	}
	fmt.Printf("  %-8s %-22s %-8s %10s %10s %10s %10s\n",
		"CODE", "PRODUCT", "WH", "ON HAND", "RESERVED", "AVAILABLE", "UNIT COST")
	fmt.Println(strings.Repeat("-", 80))
	for _, sl := range result.Levels {
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

func printStatement(result *app.AccountStatementResult) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("  ACCOUNT STATEMENT — %s  (%s)\n", result.AccountCode, result.Currency)
	fmt.Println(strings.Repeat("=", 90))
	if len(result.Lines) == 0 {
		fmt.Println("  No movements found for the given period.")
		fmt.Println(strings.Repeat("=", 90))
		return
	}
	fmt.Printf("  %-12s %-36s %12s %12s %12s\n", "DATE", "NARRATION", "DEBIT", "CREDIT", "BALANCE")
	fmt.Println(strings.Repeat("-", 90))
	for _, l := range result.Lines {
		narration := l.Narration
		if len(narration) > 35 {
			narration = narration[:32] + "..."
		}
		fmt.Printf("  %-12s %-36s %12s %12s %12s\n",
			l.PostingDate,
			narration,
			l.Debit.StringFixed(2),
			l.Credit.StringFixed(2),
			l.RunningBalance.StringFixed(2),
		)
	}
	closing := result.Lines[len(result.Lines)-1].RunningBalance
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("  %-49s %12s %12s %12s\n", "CLOSING BALANCE", "", "", closing.StringFixed(2))
	fmt.Println(strings.Repeat("=", 90))
}

func printHelp() {
	fmt.Println()
	fmt.Println("ACCOUNTING AGENT — COMMANDS")
	fmt.Println(strings.Repeat("=", 62))
	fmt.Println()
	fmt.Println("  LEDGER")
	fmt.Println("  /bal [company-code]                          Trial balance")
	fmt.Println("  /balances [company-code]                     Alias for /bal")
	fmt.Println("  /statement <acct> [from-date] [to-date]      Account statement with running balance")
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
