package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"accounting-agent/internal/ai"
	"accounting-agent/internal/core"

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

	company := &core.Company{
		ID:           1,
		CompanyCode:  "1000",
		Name:         "Local Operations India",
		BaseCurrency: "INR",
	}

	documentTypes := `
- JE: Journal Entry
- SI: Sales Invoice
- PI: Purchase Invoice
`

	fmt.Printf("INTERPRETING EVENT: %s\n", event)
	response, err := agent.InterpretEvent(ctx, event, chartOfAccounts, documentTypes, company)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	if response.IsClarificationRequest {
		fmt.Printf("\n--- CLARIFICATION NEEDED ---\n")
		fmt.Printf("%s\n", response.Clarification.Message)
		return
	}

	proposal := response.Proposal
	fmt.Printf("\n--- PROPOSAL ---\n")
	fmt.Printf("Document Type: %s\n", proposal.DocumentTypeCode)
	fmt.Printf("Confidence: %.2f\n", proposal.Confidence)
	fmt.Printf("Reasoning: %s\n", proposal.Reasoning)

	fmt.Printf("\nCurrency: %s @ rate %s\n", proposal.TransactionCurrency, proposal.ExchangeRate)
	fmt.Printf("\nEntries:\n")
	for _, line := range proposal.Lines {
		dOrC := "CR"
		if line.IsDebit {
			dOrC = "DR"
		}
		fmt.Printf("- Account: %s [%s] %s %s\n", line.AccountCode, dOrC, line.Amount, proposal.TransactionCurrency)
	}
}
