package main

import (
	"context"
	"log"
	"net/http"
	"os"

	webAdapter "accounting-agent/internal/adapters/web"
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
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	ruleEngine := core.NewRuleEngine(pool)
	orderService := core.NewOrderService(pool, ruleEngine)
	inventoryService := core.NewInventoryService(pool, ruleEngine)
	reportingService := core.NewReportingService(pool)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY is not set")
	}
	agent := ai.NewAgent(apiKey)

	svc := app.NewAppService(pool, ledger, docService, orderService, inventoryService, reportingService, agent)

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	handler := webAdapter.NewHandler(svc, allowedOrigins)

	log.Printf("server starting on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
