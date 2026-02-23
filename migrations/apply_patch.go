package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	dbURL := os.Getenv("DATABASE_URL")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	sqlFile, err := os.ReadFile("migrations/002_sap_currency.sql")
	if err != nil {
		fmt.Printf("Failed to read sql file: %v\n", err)
		os.Exit(1)
	}

	_, err = pool.Exec(ctx, string(sqlFile))
	if err != nil {
		fmt.Printf("Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Migration successful.")
}
