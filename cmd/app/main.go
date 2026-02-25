package main

import (
	"bufio"
	"context"
	"log"
	"os"

	cliAdapter "accounting-agent/internal/adapters/cli"
	replAdapter "accounting-agent/internal/adapters/repl"
	"accounting-agent/internal/ai"
	"accounting-agent/internal/app"
	"accounting-agent/internal/core"
	"accounting-agent/internal/db"

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
	ruleEngine := core.NewRuleEngine(pool)
	orderService := core.NewOrderService(pool, ruleEngine)
	inventoryService := core.NewInventoryService(pool)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY is not set")
	}
	agent := ai.NewAgent(apiKey)

	svc := app.NewAppService(pool, ledger, docService, orderService, inventoryService, agent)

	if len(os.Args) > 1 {
		cliAdapter.Run(ctx, svc, os.Args[1:])
	} else {
		reader := bufio.NewReader(os.Stdin)
		replAdapter.Run(ctx, svc, reader)
	}
}
