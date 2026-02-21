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

	ledger := core.NewLedger(pool)
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
			proposal, err := agent.InterpretEvent(ctx, event, coa)
			if err != nil {
				log.Fatalf("Agent error: %v", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(proposal)

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
		runREPL(ctx, agent, ledger, pool)
	}
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

func runREPL(ctx context.Context, agent *ai.Agent, ledger *core.Ledger, pool *pgxpool.Pool) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Agentic Accounting REPL")
	fmt.Println("Type 'balances' to see current state.")
	fmt.Println("-----------------------")

	for {
		fmt.Print("\n> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "exit" || input == "quit" {
			break
		}
		if input == "balances" {
			printBalances(ctx, ledger)
			continue
		}
		if input == "" {
			continue
		}

		fmt.Println("Thinking...")
		coa, err := fetchCoA(ctx, pool)
		if err != nil {
			fmt.Printf("Error fetching accounts: %v\n", err)
			continue
		}

		proposal, err := agent.InterpretEvent(ctx, input, coa)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

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
	fmt.Printf("\nSUMMARY: %s\n", p.Summary)
	fmt.Printf("REASONING: %s\n", p.Reasoning)
	fmt.Printf("CONFIDENCE: %.2f\n", p.Confidence)
	fmt.Println("ENTRIES:")
	for _, l := range p.Lines {
		fmt.Printf("  %s: Debit %s | Credit %s\n", l.AccountCode, l.Debit, l.Credit)
	}
}
