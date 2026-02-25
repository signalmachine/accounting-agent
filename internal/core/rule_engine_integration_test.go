package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"
)

func TestRuleEngine_ResolveAccount(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()

	// Seed rules for company 1
	_, err := pool.Exec(ctx, `
		INSERT INTO account_rules (company_id, rule_type, account_code, priority)
		VALUES
		  (1, 'AR',        '1200', 0),
		  (1, 'AP',        '2000', 0),
		  (1, 'HIGH_PRIO', '9999', 10),
		  (1, 'HIGH_PRIO', '8888', 0)
		ON CONFLICT DO NOTHING;
	`)
	if err != nil {
		t.Fatalf("Failed to seed account_rules: %v", err)
	}

	re := core.NewRuleEngine(pool)

	t.Run("resolves AR", func(t *testing.T) {
		code, err := re.ResolveAccount(ctx, 1, "AR")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != "1200" {
			t.Errorf("expected 1200, got %s", code)
		}
	})

	t.Run("resolves AP", func(t *testing.T) {
		code, err := re.ResolveAccount(ctx, 1, "AP")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != "2000" {
			t.Errorf("expected 2000, got %s", code)
		}
	})

	t.Run("priority DESC picks highest priority row", func(t *testing.T) {
		code, err := re.ResolveAccount(ctx, 1, "HIGH_PRIO")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if code != "9999" {
			t.Errorf("expected 9999 (priority 10), got %s", code)
		}
	})

	t.Run("missing rule returns descriptive error", func(t *testing.T) {
		_, err := re.ResolveAccount(ctx, 1, "NONEXISTENT_RULE")
		if err == nil {
			t.Error("expected error for missing rule, got nil")
		}
	})

	t.Run("company isolation — rule for company 1 not visible to company 2", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
			INSERT INTO companies (id, company_code, name, base_currency)
			VALUES (2, '2000', 'Company Two', 'USD')
			ON CONFLICT DO NOTHING;
		`)
		if err != nil {
			t.Fatalf("Failed to seed second company: %v", err)
		}

		_, err = re.ResolveAccount(ctx, 2, "AR")
		if err == nil {
			t.Error("expected error: company 2 has no AR rule")
		}
	})
}
