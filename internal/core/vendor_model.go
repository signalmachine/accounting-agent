package core

import (
	"context"
	"time"
)

// Vendor represents a supplier or service provider in the accounts payable system.
type Vendor struct {
	ID                       int
	CompanyID                int
	Code                     string
	Name                     string
	ContactPerson            *string
	Email                    *string
	Phone                    *string
	Address                  *string
	PaymentTermsDays         int
	APAccountCode            string
	DefaultExpenseAccountCode *string
	IsActive                 bool
	CreatedAt                time.Time
}

// VendorInput holds the fields required to create a new vendor.
type VendorInput struct {
	Code                     string
	Name                     string
	ContactPerson            string
	Email                    string
	Phone                    string
	Address                  string
	PaymentTermsDays         int
	APAccountCode            string
	DefaultExpenseAccountCode string
}

// VendorService provides vendor master data operations.
type VendorService interface {
	// CreateVendor creates a new vendor record for the given company.
	CreateVendor(ctx context.Context, companyID int, input VendorInput) (*Vendor, error)

	// GetVendors returns all active vendors for a company.
	GetVendors(ctx context.Context, companyID int) ([]Vendor, error)

	// GetVendorByCode returns a specific vendor by its code, scoped to the company.
	GetVendorByCode(ctx context.Context, companyID int, code string) (*Vendor, error)
}
