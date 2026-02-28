package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestReporting_GetProfitAndLoss(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	reporting := core.NewReportingService(pool)
	ctx := context.Background()

	// Post Jan 2026 revenue and expense entries.
	// Entry 1: DR Cash 1000 / CR Sales Revenue 1000  → Jan revenue = 1000
	// Entry 2: DR Operating Expenses 300 / CR Cash 300 → Jan expenses = 300
	// Entry 3 (Feb): DR Cash 500 / CR Sales Revenue 500 → NOT in Jan
	proposals := []core.Proposal{
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-10", DocumentDate: "2026-01-10",
			Summary: "Jan sale", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "1000.00"},
				{AccountCode: "4000", IsDebit: false, Amount: "1000.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-20", DocumentDate: "2026-01-20",
			Summary: "Jan expense", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "5100", IsDebit: true, Amount: "300.00"},
				{AccountCode: "1000", IsDebit: false, Amount: "300.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-02-05", DocumentDate: "2026-02-05",
			Summary: "Feb sale", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "500.00"},
				{AccountCode: "4000", IsDebit: false, Amount: "500.00"},
			},
		},
	}
	for _, p := range proposals {
		if err := ledger.Commit(ctx, p); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
	}

	t.Run("Jan P&L revenue and expenses", func(t *testing.T) {
		report, err := reporting.GetProfitAndLoss(ctx, "1000", 2026, 1)
		if err != nil {
			t.Fatalf("GetProfitAndLoss failed: %v", err)
		}
		if report.Year != 2026 || report.Month != 1 {
			t.Errorf("Period mismatch: got %d/%d", report.Year, report.Month)
		}

		// Revenue: credit 1000 - debit 0 = 1000
		var revTotal decimal.Decimal
		for _, r := range report.Revenue {
			revTotal = revTotal.Add(r.Balance)
		}
		if !revTotal.Equal(decimal.NewFromInt(1000)) {
			t.Errorf("Jan revenue: want 1000, got %s", revTotal)
		}

		// Expenses: debit 300 - credit 0 = 300
		var expTotal decimal.Decimal
		for _, e := range report.Expenses {
			expTotal = expTotal.Add(e.Balance)
		}
		if !expTotal.Equal(decimal.NewFromInt(300)) {
			t.Errorf("Jan expenses: want 300, got %s", expTotal)
		}

		// Net income = 700
		if !report.NetIncome.Equal(decimal.NewFromInt(700)) {
			t.Errorf("Jan net income: want 700, got %s", report.NetIncome)
		}
	})

	t.Run("Feb P&L excludes Jan entries", func(t *testing.T) {
		report, err := reporting.GetProfitAndLoss(ctx, "1000", 2026, 2)
		if err != nil {
			t.Fatalf("GetProfitAndLoss (Feb) failed: %v", err)
		}
		// Feb revenue = 500, expenses = 0, net income = 500
		var revTotal decimal.Decimal
		for _, r := range report.Revenue {
			revTotal = revTotal.Add(r.Balance)
		}
		if !revTotal.Equal(decimal.NewFromInt(500)) {
			t.Errorf("Feb revenue: want 500, got %s", revTotal)
		}
		if !report.NetIncome.Equal(decimal.NewFromInt(500)) {
			t.Errorf("Feb net income: want 500, got %s", report.NetIncome)
		}
	})
}

func TestReporting_GetBalanceSheet(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	reporting := core.NewReportingService(pool)
	ctx := context.Background()

	// Post entries only to balance-sheet accounts so IsBalanced holds.
	// Entry 1 (Jan 5): DR Cash 10000 / CR Share Capital 10000
	//   → Assets=10000, Equity=10000, Liabilities=0 → balanced
	// Entry 2 (Jan 15): DR Cash 5000 / CR Accounts Payable 5000
	//   → Assets=15000, Equity=10000, Liabilities=5000 → balanced
	// Entry 3 (Feb 1): DR AR 2000 / CR Cash 2000
	//   → internal asset swap; still balanced
	proposals := []core.Proposal{
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-05", DocumentDate: "2026-01-05",
			Summary: "Share capital injection", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "10000.00"},
				{AccountCode: "3000", IsDebit: false, Amount: "10000.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-01-15", DocumentDate: "2026-01-15",
			Summary: "Loan received", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "5000.00"},
				{AccountCode: "2000", IsDebit: false, Amount: "5000.00"},
			},
		},
		{
			DocumentTypeCode: "JE", CompanyCode: "1000",
			IdempotencyKey: uuid.NewString(), TransactionCurrency: "INR", ExchangeRate: "1.0",
			PostingDate: "2026-02-01", DocumentDate: "2026-02-01",
			Summary: "Cash to AR", Reasoning: "test",
			Lines: []core.ProposalLine{
				{AccountCode: "1200", IsDebit: true, Amount: "2000.00"},
				{AccountCode: "1000", IsDebit: false, Amount: "2000.00"},
			},
		},
	}
	for _, p := range proposals {
		if err := ledger.Commit(ctx, p); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
	}

	t.Run("balance sheet as of Jan 31 excludes Feb entry", func(t *testing.T) {
		report, err := reporting.GetBalanceSheet(ctx, "1000", "2026-01-31")
		if err != nil {
			t.Fatalf("GetBalanceSheet failed: %v", err)
		}

		// Cash (1000): DR 15000 (10000+5000), net = 15000 asset
		var cashBalance decimal.Decimal
		for _, a := range report.Assets {
			if a.Code == "1000" {
				cashBalance = a.Balance
			}
		}
		if !cashBalance.Equal(decimal.NewFromInt(15000)) {
			t.Errorf("Cash balance as of Jan 31: want 15000, got %s", cashBalance)
		}

		// AR (1200): 0 (Feb entry excluded)
		var arBalance decimal.Decimal
		for _, a := range report.Assets {
			if a.Code == "1200" {
				arBalance = a.Balance
			}
		}
		if !arBalance.IsZero() {
			t.Errorf("AR balance as of Jan 31: want 0, got %s", arBalance)
		}

		// Total assets = 15000, total liabilities = 5000, total equity = 10000
		if !report.TotalAssets.Equal(decimal.NewFromInt(15000)) {
			t.Errorf("TotalAssets: want 15000, got %s", report.TotalAssets)
		}
		if !report.TotalLiabilities.Equal(decimal.NewFromInt(5000)) {
			t.Errorf("TotalLiabilities: want 5000, got %s", report.TotalLiabilities)
		}
		if !report.TotalEquity.Equal(decimal.NewFromInt(10000)) {
			t.Errorf("TotalEquity: want 10000, got %s", report.TotalEquity)
		}
		if !report.IsBalanced {
			t.Error("Expected IsBalanced=true")
		}
	})

	t.Run("balance sheet includes all entries with empty date", func(t *testing.T) {
		report, err := reporting.GetBalanceSheet(ctx, "1000", "2026-12-31")
		if err != nil {
			t.Fatalf("GetBalanceSheet (all) failed: %v", err)
		}
		// Cash = 15000 - 2000 = 13000; AR = 2000; total assets still 15000
		if !report.TotalAssets.Equal(decimal.NewFromInt(15000)) {
			t.Errorf("TotalAssets (all): want 15000, got %s", report.TotalAssets)
		}
		if !report.IsBalanced {
			t.Error("Expected IsBalanced=true for all entries")
		}
	})
}

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
