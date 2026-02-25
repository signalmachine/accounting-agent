package repl

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"accounting-agent/internal/app"

	"github.com/shopspring/decimal"
)

// handleNewOrder runs an interactive order creation session.
func handleNewOrder(ctx context.Context, reader *bufio.Reader, svc app.ApplicationService, companyCode, baseCurrency, customerCode string) {
	fmt.Printf("Creating order for customer: %s\n", customerCode)
	fmt.Println("Enter order lines. Type 'done' when finished, 'cancel' to abort.")
	fmt.Println("Format per line: <product-code> <quantity> [unit-price]")
	fmt.Println("  Example: P001 10")
	fmt.Println("  Example: P001 5 450.00   (overrides product default price)")

	var lines []app.OrderLineInput
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

		lines = append(lines, app.OrderLineInput{
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
	orderDate := dateInput
	if orderDate == "" {
		orderDate = time.Now().Format("2006-01-02")
	}

	fmt.Print("Notes (optional): ")
	notes, _ := reader.ReadString('\n')
	notes = strings.TrimSpace(notes)

	fmt.Printf("Currency [%s]: ", baseCurrency)
	currency, _ := reader.ReadString('\n')
	currency = strings.TrimSpace(strings.ToUpper(currency))
	if currency == "" {
		currency = baseCurrency
	}

	result, err := svc.CreateOrder(ctx, app.CreateOrderRequest{
		CompanyCode:  companyCode,
		CustomerCode: customerCode,
		Currency:     currency,
		OrderDate:    orderDate,
		Notes:        notes,
		Lines:        lines,
	})
	if err != nil {
		fmt.Printf("[REPL] Error creating order: %v\n", err)
		return
	}

	fmt.Printf("\nOrder created (ID: %d, Status: DRAFT)\n", result.Order.ID)
	printOrderDetail(result.Order)
	fmt.Println("Use '/confirm <id>' to assign an order number.")
}
