package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://app:electron@localhost:5432/appdb?sslmode=disable"
	}

	ctx := context.Background()
	pool := connectDB(ctx, url)
	defer pool.Close()

	conn := acquireLock(ctx, pool)
	defer conn.Release()

	setupSchemaMigrations(ctx, pool)

	migrations := discoverMigrations()

	for _, filename := range migrations {
		applyMigration(ctx, pool, filename)
	}

	log.Println("[DONE] All migrations processed.")
}

func connectDB(ctx context.Context, url string) *pgxpool.Pool {
	connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("[CONNECT] failed to create pool: %v", err)
	}

	if err := pool.Ping(connCtx); err != nil {
		log.Fatalf("[CONNECT] failed to ping database: %v", err)
	}

	log.Println("[CONNECT] success")
	return pool
}

func acquireLock(ctx context.Context, pool *pgxpool.Pool) *pgxpool.Conn {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		log.Fatalf("[LOCK] failed to acquire connection for lock: %v", err)
	}

	var locked bool
	err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(7462839)").Scan(&locked)
	if err != nil {
		log.Fatalf("[LOCK] failed to query advisory lock: %v", err)
	}

	if !locked {
		log.Fatalf("[LOCK] failed: another migrator is currently running")
	}

	log.Println("[LOCK] success")
	return conn
}

func setupSchemaMigrations(ctx context.Context, pool *pgxpool.Pool) {
	query := `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	filename TEXT NOT NULL,
	checksum TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`
	_, err := pool.Exec(ctx, query)
	if err != nil {
		log.Fatalf("[ERROR] failed to create schema_migrations table: %v", err)
	}
}

func discoverMigrations() []string {
	entries, err := os.ReadDir("migrations")
	if err != nil {
		log.Fatalf("[DISCOVER] failed to read migrations directory: %v", err)
	}

	var filenames []string
	versionMap := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		filename := entry.Name()
		version := extractVersion(filename)

		if versionMap[version] {
			log.Fatalf("[DISCOVER] duplicate version found: %s", version)
		}
		versionMap[version] = true

		filenames = append(filenames, filename)
	}

	sort.Strings(filenames)
	return filenames
}

func extractVersion(filename string) string {
	parts := strings.SplitN(filename, "_", 2)
	if len(parts) < 2 {
		log.Fatalf("[DISCOVER] invalid migration filename format: %s. Expected format NNN_description.sql", filename)
	}
	return parts[0]
}

func checksumFile(filename string) string {
	path := filepath.Join("migrations", filename)
	bytes, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("[ERROR] failed to read file for checksum %s: %v", filename, err)
	}

	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:])
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, filename string) {
	version := extractVersion(filename)
	checksum := checksumFile(filename)

	var existingChecksum string
	err := pool.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", version).Scan(&existingChecksum)

	if err == nil {
		// Version exists
		if existingChecksum == checksum {
			log.Printf("[SKIP] %s", filename)
			return
		} else {
			log.Fatalf("[ERROR] Checksum mismatch for %s. Expected %s, got %s", filename, existingChecksum, checksum)
		}
	} else if err == pgx.ErrNoRows {
		// Does not exist, proceed
	} else {
		log.Fatalf("[ERROR] failed to query schema_migrations for %s: %v", filename, err)
	}

	path := filepath.Join("migrations", filename)
	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("[ERROR] failed to read migration file %s: %v", filename, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("[ERROR] failed to begin transaction for %s: %v", filename, err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, string(sqlBytes))
	if err != nil {
		log.Fatalf("[ERROR] failed to execute migration %s: %v", filename, err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version, filename, checksum) VALUES ($1, $2, $3)", version, filename, checksum)
	if err != nil {
		log.Fatalf("[ERROR] failed to insert migration record for %s: %v", filename, err)
	}

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatalf("[ERROR] failed to commit transaction for %s: %v", filename, err)
	}

	log.Printf("[APPLY] %s", filename)
}
