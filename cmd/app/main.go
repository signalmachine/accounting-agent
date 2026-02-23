package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"accounting-agent/internal/ai"
	"accounting-agent/internal/core"
	"accounting-agent/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
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
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY is not set")
	}
	agent := ai.NewAgent(apiKey)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "propose":
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

		case "validate":
			var proposal core.Proposal
			if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
				log.Fatalf("Invalid JSON: %v", err)
			}
			if err := ledger.Validate(ctx, proposal); err != nil {
				log.Fatalf("Validation failed: %v", err)
			}

		case "commit":
			var proposal core.Proposal
			if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
				log.Fatalf("Invalid JSON: %v", err)
			}
			if err := ledger.Commit(ctx, proposal); err != nil {
				log.Fatalf("Commit failed: %v", err)
			}
			fmt.Println("Transaction Committed.")

		case "balances":
			printBalances(ctx, ledger)

		default:
			log.Fatalf("Unknown command: %s", os.Args[1])
		}
	} else {
		company, err := loadDefaultCompany(ctx, pool)
		if err != nil {
			log.Fatalf("Failed to load company for REPL: %v", err)
		}
		runREPL(ctx, agent, ledger, pool, company)
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

func runREPL(ctx context.Context, agent *ai.Agent, ledger *core.Ledger, pool *pgxpool.Pool, company *core.Company) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Agentic Accounting REPL")
	fmt.Println("Type 'balances' to see current state.")
	fmt.Println("-----------------------")

	var errExit = fmt.Errorf("exit repl")
	commands := map[string]func() error{
		"balances": func() error {
			printBalances(ctx, ledger)
			return nil
		},
		"help": func() error {
			fmt.Println("Available commands: balances, help, exit, quit")
			return nil
		},
		"exit": func() error { return errExit },
		"quit": func() error { return errExit },
	}

	for {
		fmt.Print("\n> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		tokens := strings.Fields(input)
		if len(tokens) == 1 {
			cmdStr := strings.ToLower(tokens[0])
			if cmd, exists := commands[cmdStr]; exists {
				if cmdStr != "exit" && cmdStr != "quit" {
					fmt.Printf("[REPL] Executing command: %s\n", cmdStr)
				}
				if err := cmd(); err != nil {
					if err == errExit {
						break
					}
					fmt.Printf("[REPL] Error: %v\n", err)
				}
				continue
			}

			fmt.Printf("Unknown command: %s\n", tokens[0])
			continue
		}

		fmt.Println("[AI] Processing natural language input...")
		fmt.Println("Thinking...")

		var accumulatedInput string = input

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
				fmt.Printf("\n[AI Clarification Needed]: %s\n", response.Clarification.Message)
				fmt.Print("Your response: ")
				userFollowUp, _ := reader.ReadString('\n')
				userFollowUp = strings.TrimSpace(userFollowUp)
				if userFollowUp == "" || strings.ToLower(userFollowUp) == "cancel" {
					fmt.Println("Transaction Cancelled.")
					break
				}
				// Append logic for the agent to have full context of the conversation
				accumulatedInput = fmt.Sprintf("Original Event: %s\nClarification requested by AI: %s\nUser provided clarification: %s", accumulatedInput, response.Clarification.Message, userFollowUp)
				fmt.Println("Thinking again...")
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
				err := ledger.Commit(ctx, *proposal)
				if err != nil {
					fmt.Printf("Transaction FAILED: %v\n", err)
				} else {
					fmt.Println("Transaction COMMITTED.")
				}
			} else {
				fmt.Println("Transaction Cancelled.")
			}
			break // Exit the clarification loop
		}
	}
}

func printBalances(ctx context.Context, ledger *core.Ledger) {
	balances, err := ledger.GetBalances(ctx)
	if err != nil {
		log.Printf("Failed to get balances: %v", err)
		return
	}

	fmt.Println("\n--- ACCOUNT BALANCES ---")
	fmt.Printf("%-10s %-30s %15s\n", "CODE", "NAME", "BALANCE")
	fmt.Println(strings.Repeat("-", 60))
	for _, b := range balances {
		fmt.Printf("%-10s %-30s %15s\n", b.Code, b.Name, b.Balance.StringFixed(2))
	}
	fmt.Println(strings.Repeat("-", 60))
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
