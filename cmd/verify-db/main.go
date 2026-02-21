package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		// Fallback or error if env not loaded (though we can load .env or hardcode for this script)
		url = "postgres://app:electron@localhost:5432/appdb?sslmode=disable"
	}

	fmt.Printf("Connecting to DB...\n")
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create connection pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Read migration file
	sqlBytes, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to read migration file: %v\n", err)
		os.Exit(1)
	}
	sql := string(sqlBytes)

	fmt.Println("Running migration...")
	_, err = pool.Exec(context.Background(), sql)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Migration successful!")
}
