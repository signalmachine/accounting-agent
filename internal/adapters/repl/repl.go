package repl

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"strings"

	"accounting-agent/internal/app"

	"github.com/shopspring/decimal"
)

// Run starts the interactive REPL loop.
// It reads commands from reader, dispatches slash commands deterministically,
// and routes natural language input through the AI agent.
func Run(ctx context.Context, svc app.ApplicationService, reader *bufio.Reader) {
	company, err := svc.LoadDefaultCompany(ctx)
	if err != nil {
		log.Fatalf("Failed to load company: %v", err)
	}

	fmt.Println("Accounting Agent")
	fmt.Printf("Company: %s — %s (%s)\n", company.CompanyCode, company.Name, company.BaseCurrency)
	fmt.Println("Describe a business event to post a journal entry, or use /help for commands.")
	fmt.Println(strings.Repeat("-", 70))

	errExit := fmt.Errorf("exit")

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
			result, err := svc.GetTrialBalance(ctx, code)
			if err != nil {
				return err
			}
			printBalances(result)

		case "customers":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			result, err := svc.ListCustomers(ctx, code)
			if err != nil {
				return err
			}
			printCustomers(result, code)

		case "products":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			result, err := svc.ListProducts(ctx, code)
			if err != nil {
				return err
			}
			printProducts(result, code)

		case "orders":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			result, err := svc.ListOrders(ctx, code, nil)
			if err != nil {
				return err
			}
			printOrders(result)

		case "new-order":
			if len(args) < 1 {
				fmt.Println("Usage: /new-order <customer-code>")
				return nil
			}
			handleNewOrder(ctx, reader, svc, company.CompanyCode, company.BaseCurrency, args[0])

		case "confirm":
			if len(args) < 1 {
				fmt.Println("Usage: /confirm <order-ref>")
				return nil
			}
			result, err := svc.ConfirmOrder(ctx, args[0], company.CompanyCode)
			if err != nil {
				return err
			}
			fmt.Printf("Order CONFIRMED. Number: %s\n", result.Order.OrderNumber)

		case "ship":
			if len(args) < 1 {
				fmt.Println("Usage: /ship <order-ref>")
				return nil
			}
			result, err := svc.ShipOrder(ctx, args[0], company.CompanyCode)
			if err != nil {
				return err
			}
			fmt.Printf("Order %s marked as SHIPPED. COGS booked if applicable.\n", result.Order.OrderNumber)

		case "invoice":
			if len(args) < 1 {
				fmt.Println("Usage: /invoice <order-ref>")
				return nil
			}
			result, err := svc.InvoiceOrder(ctx, args[0], company.CompanyCode)
			if err != nil {
				return err
			}
			fmt.Printf("Order %s INVOICED. Journal entry committed (DR AR, CR Revenue).\n", result.Order.OrderNumber)

		case "payment":
			if len(args) < 1 {
				fmt.Println("Usage: /payment <order-ref> [bank-account-code]")
				return nil
			}
			bankCode := "1100"
			if len(args) >= 2 {
				bankCode = args[1]
			}
			result, err := svc.RecordPayment(ctx, args[0], bankCode, company.CompanyCode)
			if err != nil {
				return err
			}
			fmt.Printf("Payment recorded for order %s. Status: PAID.\n", result.Order.OrderNumber)

		case "warehouses":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			result, err := svc.ListWarehouses(ctx, code)
			if err != nil {
				return err
			}
			printWarehouses(result, code)

		case "stock":
			code := company.CompanyCode
			if len(args) > 0 {
				code = strings.ToUpper(args[0])
			}
			result, err := svc.GetStockLevels(ctx, code)
			if err != nil {
				return err
			}
			printStockLevels(result)

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
			if err := svc.ReceiveStock(ctx, app.ReceiveStockRequest{
				CompanyCode:       company.CompanyCode,
				ProductCode:       productCode,
				CreditAccountCode: creditAccount,
				Qty:               qty,
				UnitCost:          unitCost,
			}); err != nil {
				return err
			}
			fmt.Printf("Received %s units of %s @ %s. DR 1400 Inventory, CR %s.\n",
				qty.String(), productCode, unitCost.String(), creditAccount)

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

		// No slash prefix → route to AI agent.
		fmt.Println("[AI] Processing...")
		accumulatedInput := input

		rounds := 0
		for {
			rounds++
			if rounds > 3 {
				fmt.Println("Could not produce a proposal. Try a slash command instead — type /help.")
				break
			}

			result, err := svc.InterpretEvent(ctx, accumulatedInput, company.CompanyCode)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				break
			}

			if result.IsClarification {
				fmt.Printf("\n[AI]: %s\n", result.ClarificationMessage)
				fmt.Print("> ")
				userFollowUp, _ := reader.ReadString('\n')
				userFollowUp = strings.TrimSpace(userFollowUp)

				// Slash command during clarification — cancel AI flow and execute it.
				if strings.HasPrefix(userFollowUp, "/") {
					fmt.Println("(AI session cancelled)")
					if dispErr := dispatchSlash(userFollowUp); dispErr != nil {
						if dispErr == errExit {
							fmt.Println("Goodbye!")
							return
						}
						fmt.Printf("Error: %v\n", dispErr)
					}
					break
				}

				if userFollowUp == "" || strings.ToLower(userFollowUp) == "cancel" {
					fmt.Println("Cancelled.")
					break
				}
				accumulatedInput = fmt.Sprintf("Original Event: %s\nClarification requested: %s\nUser response: %s",
					accumulatedInput, result.ClarificationMessage, userFollowUp)
				fmt.Println("[AI] Thinking...")
				continue
			}

			proposal := result.Proposal
			printProposal(proposal)

			if proposal.Confidence < 0.6 {
				fmt.Println("\nWARNING: Low confidence proposal.")
			}

			fmt.Print("\nApprove this transaction? (y/n): ")
			choice, _ := reader.ReadString('\n')
			choice = strings.TrimSpace(strings.ToLower(choice))

			if choice == "y" || choice == "yes" {
				if err := svc.CommitProposal(ctx, *proposal); err != nil {
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
