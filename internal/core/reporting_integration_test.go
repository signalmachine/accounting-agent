package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestReporting_GetAccountStatement(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	reporting := core.NewReportingService(pool)
	ctx := context.Background()

	// Post 3 journal entries touching account 1000 (Test Asset):
	//   Entry 1 (2026-01-01): DR 1000 100, CR 4000 100  → running +100
	//   Entry 2 (2026-01-15): DR 1000 200, CR 4000 200  → running +300
	//   Entry 3 (2026-02-01): DR 4000  50, CR 1000  50  → running +250
	proposals := []core.Proposal{
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-01", DocumentDate: "2026-01-01",
			Summary: "Entry 1", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "100.00"},
				{AccountCode: "4000", IsDebit: false, Amount: "100.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-15", DocumentDate: "2026-01-15",
			Summary: "Entry 2", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "200.00"},
				{AccountCode: "4000", IsDebit: false, Amount: "200.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-02-01", DocumentDate: "2026-02-01",
			Summary: "Entry 3", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "4000", IsDebit: true, Amount: "50.00"},
				{AccountCode: "1000", IsDebit: false, Amount: "50.00"},
			},
		},
	}
	for _, p := range proposals {
		if err := ledger.Commit(ctx, p); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
	}

	t.Run("full statement no date filter", func(t *testing.T) {
		lines, err := reporting.GetAccountStatement(ctx, "1000", "1000", "", "")
		if err != nil {
			t.Fatalf("GetAccountStatement failed: %v", err)
		}
		if len(lines) != 3 {
			t.Fatalf("Expected 3 lines, got %d", len(lines))
		}

		// Line 1: debit 100, running 100
		if !lines[0].Debit.Equal(decimal.NewFromInt(100)) {
			t.Errorf("Line 1 debit: want 100, got %s", lines[0].Debit)
		}
		if !lines[0].Credit.IsZero() {
			t.Errorf("Line 1 credit: want 0, got %s", lines[0].Credit)
		}
		if !lines[0].RunningBalance.Equal(decimal.NewFromInt(100)) {
			t.Errorf("Line 1 running: want 100, got %s", lines[0].RunningBalance)
		}

		// Line 2: debit 200, running 300
		if !lines[1].Debit.Equal(decimal.NewFromInt(200)) {
			t.Errorf("Line 2 debit: want 200, got %s", lines[1].Debit)
		}
		if !lines[1].RunningBalance.Equal(decimal.NewFromInt(300)) {
			t.Errorf("Line 2 running: want 300, got %s", lines[1].RunningBalance)
		}

		// Line 3: credit 50, running 250
		if !lines[2].Debit.IsZero() {
			t.Errorf("Line 3 debit: want 0, got %s", lines[2].Debit)
		}
		if !lines[2].Credit.Equal(decimal.NewFromInt(50)) {
			t.Errorf("Line 3 credit: want 50, got %s", lines[2].Credit)
		}
		if !lines[2].RunningBalance.Equal(decimal.NewFromInt(250)) {
			t.Errorf("Line 3 running: want 250, got %s", lines[2].RunningBalance)
		}
	})

	t.Run("date range Jan only", func(t *testing.T) {
		lines, err := reporting.GetAccountStatement(ctx, "1000", "1000", "2026-01-01", "2026-01-31")
		if err != nil {
			t.Fatalf("GetAccountStatement (Jan) failed: %v", err)
		}
		if len(lines) != 2 {
			t.Fatalf("Expected 2 Jan lines, got %d", len(lines))
		}
		// Running balance after two Jan debits = 300
		if !lines[1].RunningBalance.Equal(decimal.NewFromInt(300)) {
			t.Errorf("Jan closing running: want 300, got %s", lines[1].RunningBalance)
		}
	})

	t.Run("empty result for unknown account", func(t *testing.T) {
		lines, err := reporting.GetAccountStatement(ctx, "1000", "9999", "", "")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(lines) != 0 {
			t.Errorf("Expected 0 lines for non-existent account, got %d", len(lines))
		}
	})
}
