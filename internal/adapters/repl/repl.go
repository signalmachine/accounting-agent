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

		case "statement":
			// Usage: /statement <account-code> [from-date] [to-date]
			if len(args) < 1 {
				fmt.Println("Usage: /statement <account-code> [from-date] [to-date]")
				fmt.Println("  from-date and to-date are optional YYYY-MM-DD.")
				return nil
			}
			accountCode := strings.ToUpper(args[0])
			fromDate, toDate := "", ""
			if len(args) >= 2 {
				fromDate = args[1]
			}
			if len(args) >= 3 {
				toDate = args[2]
			}
			result, err := svc.GetAccountStatement(ctx, company.CompanyCode, accountCode, fromDate, toDate)
			if err != nil {
				return err
			}
			printStatement(result)

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

		// No slash prefix → route to AI agent via InterpretDomainAction.
		// The agent calls read tools autonomously, then returns one of four outcomes:
		//   answer       — text response; display and done.
		//   clarification— agent needs more info; read follow-up, re-call (3-round cap).
		//   proposed     — domain write action; show card, confirm, execute.
		//   journal_entry— financial event; route to InterpretEvent for proposal.
		fmt.Println("[AI] Processing...")
		accumulatedInput := input
		rounds := 0

	domainLoop:
		for {
			rounds++
			if rounds > 3 {
				fmt.Println("Could not determine the right action. Try a slash command instead — type /help.")
				break
			}

			result, err := svc.InterpretDomainAction(ctx, accumulatedInput, company.CompanyCode)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				break
			}

			switch result.Kind {

			case app.DomainActionKindAnswer:
				// Agent answered from read tool results — display and done.
				fmt.Printf("\n[AI]: %s\n", result.Answer)
				break domainLoop

			case app.DomainActionKindClarification:
				// Agent needs more information.
				fmt.Printf("\n[AI]: %s\n", result.Question)
				if result.Context != "" {
					fmt.Printf("      (Context: %s)\n", result.Context)
				}
				fmt.Print("> ")
				userFollowUp, _ := reader.ReadString('\n')
				userFollowUp = strings.TrimSpace(userFollowUp)

				// Slash command during clarification — cancel AI flow and dispatch it.
				if strings.HasPrefix(userFollowUp, "/") {
					fmt.Println("(AI session cancelled)")
					if dispErr := dispatchSlash(userFollowUp); dispErr != nil {
						if dispErr == errExit {
							fmt.Println("Goodbye!")
							return
						}
						fmt.Printf("Error: %v\n", dispErr)
					}
					break domainLoop
				}
				if userFollowUp == "" || strings.ToLower(userFollowUp) == "cancel" {
					fmt.Println("Cancelled.")
					break domainLoop
				}
				accumulatedInput = fmt.Sprintf("Original: %s\nContext established: %s\nUser response: %s",
					accumulatedInput, result.Question, userFollowUp)
				fmt.Println("[AI] Thinking...")
				continue

			case app.DomainActionKindProposed:
				// Agent proposes a domain write action — show card and confirm.
				fmt.Printf("\n[AI proposes action]: %s\n", result.ToolName)
				if len(result.ToolArgs) > 0 {
					for k, v := range result.ToolArgs {
						fmt.Printf("  %s: %v\n", k, v)
					}
				}
				fmt.Print("\nExecute this action? (y/n): ")
				choice, _ := reader.ReadString('\n')
				choice = strings.TrimSpace(strings.ToLower(choice))
				if choice != "y" && choice != "yes" {
					fmt.Println("Action cancelled.")
				} else {
					// Write tool execution is wired in later phases as domain write tools are registered.
					fmt.Println("Action execution not yet wired for this tool. Use a slash command to complete it.")
				}
				break domainLoop

			case app.DomainActionKindJournalEntry:
				// Agent identified a financial event — route to InterpretEvent (structured output path).
				fmt.Println("[AI] Routing to journal entry handler...")
				jeInput := result.EventDescription
				jeRounds := 0

			journalLoop:
				for {
					jeRounds++
					if jeRounds > 3 {
						fmt.Println("Could not produce a proposal. Try a slash command instead — type /help.")
						break journalLoop
					}

					jeResult, jeErr := svc.InterpretEvent(ctx, jeInput, company.CompanyCode)
					if jeErr != nil {
						fmt.Printf("Error: %v\n", jeErr)
						break journalLoop
					}

					if jeResult.IsClarification {
						fmt.Printf("\n[AI]: %s\n", jeResult.ClarificationMessage)
						fmt.Print("> ")
						userFollowUp, _ := reader.ReadString('\n')
						userFollowUp = strings.TrimSpace(userFollowUp)

						if strings.HasPrefix(userFollowUp, "/") {
							fmt.Println("(AI session cancelled)")
							if dispErr := dispatchSlash(userFollowUp); dispErr != nil {
								if dispErr == errExit {
									fmt.Println("Goodbye!")
									return
								}
								fmt.Printf("Error: %v\n", dispErr)
							}
							break journalLoop
						}
						if userFollowUp == "" || strings.ToLower(userFollowUp) == "cancel" {
							fmt.Println("Cancelled.")
							break journalLoop
						}
						jeInput = fmt.Sprintf("Original Event: %s\nClarification requested: %s\nUser response: %s",
							jeInput, jeResult.ClarificationMessage, userFollowUp)
						fmt.Println("[AI] Thinking...")
						continue
					}

					proposal := jeResult.Proposal
					printProposal(proposal)
					if proposal.Confidence < 0.6 {
						fmt.Println("\nWARNING: Low confidence proposal.")
					}
					fmt.Print("\nApprove this transaction? (y/n): ")
					choice, _ := reader.ReadString('\n')
					choice = strings.TrimSpace(strings.ToLower(choice))
					if choice == "y" || choice == "yes" {
						if commitErr := svc.CommitProposal(ctx, *proposal); commitErr != nil {
							fmt.Printf("Transaction FAILED: %v\n", commitErr)
						} else {
							fmt.Println("Transaction COMMITTED.")
						}
					} else {
						fmt.Println("Transaction Cancelled.")
					}
					break journalLoop
				}
				break domainLoop
			}
		}
	}
}
