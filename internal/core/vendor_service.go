package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type vendorService struct {
	pool *pgxpool.Pool
}

// NewVendorService constructs a VendorService backed by PostgreSQL.
func NewVendorService(pool *pgxpool.Pool) VendorService {
	return &vendorService{pool: pool}
}

// CreateVendor inserts a new vendor record for the given company.
func (s *vendorService) CreateVendor(ctx context.Context, companyID int, input VendorInput) (*Vendor, error) {
	apAccountCode := input.APAccountCode
	if apAccountCode == "" {
		apAccountCode = "2000"
	}
	paymentTerms := input.PaymentTermsDays
	if paymentTerms == 0 {
		paymentTerms = 30
	}

	var expenseCode *string
	if input.DefaultExpenseAccountCode != "" {
		expenseCode = &input.DefaultExpenseAccountCode
	}

	toPtr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}

	v := &Vendor{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vendors (company_id, code, name, contact_person, email, phone, address,
		                     payment_terms_days, ap_account_code, default_expense_account_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, company_id, code, name, contact_person, email, phone, address,
		          payment_terms_days, ap_account_code, default_expense_account_code, is_active, created_at`,
		companyID, input.Code, input.Name, toPtr(input.ContactPerson), toPtr(input.Email),
		toPtr(input.Phone), toPtr(input.Address), paymentTerms, apAccountCode, expenseCode,
	).Scan(
		&v.ID, &v.CompanyID, &v.Code, &v.Name,
		&v.ContactPerson, &v.Email, &v.Phone, &v.Address,
		&v.PaymentTermsDays, &v.APAccountCode, &v.DefaultExpenseAccountCode,
		&v.IsActive, &v.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create vendor %q: %w", input.Code, err)
	}
	return v, nil
}

// GetVendors returns all active vendors for a company, ordered by code.
func (s *vendorService) GetVendors(ctx context.Context, companyID int) ([]Vendor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, code, name, contact_person, email, phone, address,
		       payment_terms_days, ap_account_code, default_expense_account_code, is_active, created_at
		FROM vendors
		WHERE company_id = $1 AND is_active = true
		ORDER BY code`,
		companyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get vendors: %w", err)
	}
	defer rows.Close()

	var vendors []Vendor
	for rows.Next() {
		var v Vendor
		if err := rows.Scan(
			&v.ID, &v.CompanyID, &v.Code, &v.Name,
			&v.ContactPerson, &v.Email, &v.Phone, &v.Address,
			&v.PaymentTermsDays, &v.APAccountCode, &v.DefaultExpenseAccountCode,
			&v.IsActive, &v.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan vendor: %w", err)
		}
		vendors = append(vendors, v)
	}
	return vendors, nil
}

// GetVendorByCode returns a vendor by code, scoped to the company.
func (s *vendorService) GetVendorByCode(ctx context.Context, companyID int, code string) (*Vendor, error) {
	v := &Vendor{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, company_id, code, name, contact_person, email, phone, address,
		       payment_terms_days, ap_account_code, default_expense_account_code, is_active, created_at
		FROM vendors
		WHERE company_id = $1 AND code = $2`,
		companyID, code,
	).Scan(
		&v.ID, &v.CompanyID, &v.Code, &v.Name,
		&v.ContactPerson, &v.Email, &v.Phone, &v.Address,
		&v.PaymentTermsDays, &v.APAccountCode, &v.DefaultExpenseAccountCode,
		&v.IsActive, &v.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("vendor %q not found: %w", code, err)
	}
	return v, nil
}
