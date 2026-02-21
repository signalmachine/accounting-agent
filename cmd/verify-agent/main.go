package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"accounting-agent/internal/ai"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load() // Load .env if present

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}

	agent := ai.NewAgent(apiKey)
	ctx := context.Background()

	chartOfAccounts := `
1000 Assets
1100 Cash
1200 Accounts Receivable
2000 Liabilities
2100 Accounts Payable
4000 Revenue
4100 Sales
5000 Expenses
5100 Rent Expense
`

	event := "Received $500.00 from a customer for services rendered."

	fmt.Printf("INTERPRETING EVENT: %s\n", event)
	proposal, err := agent.InterpretEvent(ctx, event, chartOfAccounts)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("\n--- PROPOSAL ---\n")
	fmt.Printf("Confidence: %.2f\n", proposal.Confidence)
	fmt.Printf("Reasoning: %s\n", proposal.Reasoning)

	fmt.Printf("\nEntries:\n")
	for _, line := range proposal.Lines {
		fmt.Printf("- Account: %s, Debit: %s, Credit: %s\n", line.AccountCode, line.Debit, line.Credit)
	}
}
