package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"accounting-agent/internal/app"
	"accounting-agent/internal/core"
)

// Run executes a one-shot CLI command and exits.
// args is os.Args[1:] — the first element is the subcommand name.
func Run(ctx context.Context, svc app.ApplicationService, args []string) {
	company, err := svc.LoadDefaultCompany(ctx)
	if err != nil {
		log.Fatalf("Failed to load company: %v", err)
	}

	switch args[0] {
	case "propose", "prop", "p":
		if len(args) < 2 {
			log.Fatal("Usage: app propose \"<event description>\"")
		}
		event := args[1]
		result, err := svc.InterpretEvent(ctx, event, company.CompanyCode)
		if err != nil {
			log.Fatalf("Agent error: %v", err)
		}
		if result.IsClarification {
			fmt.Fprintln(os.Stderr, "AI needs clarification:", result.ClarificationMessage)
			os.Exit(1)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result.Proposal)

	case "validate", "val", "v":
		var proposal core.Proposal
		if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if err := svc.ValidateProposal(ctx, proposal); err != nil {
			log.Fatalf("Validation failed: %v", err)
		}
		fmt.Println("Proposal is valid.")

	case "commit", "com", "c":
		var proposal core.Proposal
		if err := json.NewDecoder(os.Stdin).Decode(&proposal); err != nil {
			log.Fatalf("Invalid JSON: %v", err)
		}
		if err := svc.CommitProposal(ctx, proposal); err != nil {
			log.Fatalf("Commit failed: %v", err)
		}
		fmt.Println("Transaction Committed.")

	case "bal", "balances":
		result, err := svc.GetTrialBalance(ctx, company.CompanyCode)
		if err != nil {
			log.Fatalf("Failed to get balances: %v", err)
		}
		printTrialBalance(result)

	default:
		log.Fatalf("Unknown command: %s\nAvailable: propose, validate, commit, bal", args[0])
	}
}

func printTrialBalance(result *app.TrialBalanceResult) {
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
