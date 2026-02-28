package core_test

import (
	"context"
	"testing"

	"accounting-agent/internal/core"
)

func TestVendor_CreateAndList(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()

	// Ensure vendors table is clean before starting
	pool.Exec(ctx, "TRUNCATE TABLE vendors CASCADE")

	svc := core.NewVendorService(pool)

	// Company 1 was seeded by setupTestDB
	companyID := 1

	t.Run("CreateVendor_Success", func(t *testing.T) {
		v, err := svc.CreateVendor(ctx, companyID, core.VendorInput{
			Code:             "V001",
			Name:             "Test Supplier Ltd",
			ContactPerson:    "Alice",
			Email:            "alice@testsupplier.com",
			Phone:            "+91-99999-00000",
			PaymentTermsDays: 30,
			APAccountCode:    "2000",
		})
		if err != nil {
			t.Fatalf("CreateVendor: %v", err)
		}
		if v.Code != "V001" {
			t.Errorf("expected code V001, got %s", v.Code)
		}
		if v.Name != "Test Supplier Ltd" {
			t.Errorf("expected name 'Test Supplier Ltd', got %s", v.Name)
		}
		if v.PaymentTermsDays != 30 {
			t.Errorf("expected payment_terms_days 30, got %d", v.PaymentTermsDays)
		}
		if v.APAccountCode != "2000" {
			t.Errorf("expected ap_account_code '2000', got %s", v.APAccountCode)
		}
		if v.ID == 0 {
			t.Error("expected vendor ID to be set")
		}
	})

	t.Run("CreateVendor_DuplicateCode_Fails", func(t *testing.T) {
		_, err := svc.CreateVendor(ctx, companyID, core.VendorInput{
			Code: "V001",
			Name: "Duplicate Vendor",
		})
		if err == nil {
			t.Error("expected error for duplicate vendor code, got nil")
		}
	})

	t.Run("GetVendors_ReturnsScopedVendors", func(t *testing.T) {
		// Add a second vendor
		_, err := svc.CreateVendor(ctx, companyID, core.VendorInput{
			Code:             "V002",
			Name:             "Second Supplier",
			PaymentTermsDays: 45,
			APAccountCode:    "2000",
		})
		if err != nil {
			t.Fatalf("CreateVendor V002: %v", err)
		}

		vendors, err := svc.GetVendors(ctx, companyID)
		if err != nil {
			t.Fatalf("GetVendors: %v", err)
		}
		if len(vendors) != 2 {
			t.Errorf("expected 2 vendors, got %d", len(vendors))
		}
	})

	t.Run("GetVendorByCode_Success", func(t *testing.T) {
		v, err := svc.GetVendorByCode(ctx, companyID, "V001")
		if err != nil {
			t.Fatalf("GetVendorByCode: %v", err)
		}
		if v.Name != "Test Supplier Ltd" {
			t.Errorf("expected 'Test Supplier Ltd', got %s", v.Name)
		}
	})

	t.Run("GetVendorByCode_NotFound", func(t *testing.T) {
		_, err := svc.GetVendorByCode(ctx, companyID, "VXXX")
		if err == nil {
			t.Error("expected error for missing vendor, got nil")
		}
	})

	t.Run("CompanyIsolation", func(t *testing.T) {
		// Seed a second company
		pool.Exec(ctx, `
			INSERT INTO companies (id, company_code, name, base_currency)
			VALUES (2, '2000', 'Other Company', 'INR')
			ON CONFLICT DO NOTHING
		`)
		otherCompanyID := 2

		// Create vendor for the other company
		_, err := svc.CreateVendor(ctx, otherCompanyID, core.VendorInput{
			Code:          "V001",
			Name:          "Other Company Vendor",
			APAccountCode: "2000",
		})
		if err != nil {
			t.Fatalf("CreateVendor for other company: %v", err)
		}

		// List vendors for company 1 — should not see company 2's vendors
		vendors, err := svc.GetVendors(ctx, companyID)
		if err != nil {
			t.Fatalf("GetVendors: %v", err)
		}
		for _, v := range vendors {
			if v.CompanyID != companyID {
				t.Errorf("vendor %s belongs to company %d, expected %d", v.Code, v.CompanyID, companyID)
			}
		}
	})
}
