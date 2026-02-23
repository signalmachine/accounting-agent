package core_test

import (
	"accounting-agent/internal/core"
	"testing"
)

func TestProposal_Validate_Reproduction(t *testing.T) {
	// Propossal with blank credit amount — should fail after normalization
	p := core.Proposal{
		CompanyCode:         "1000",
		TransactionCurrency: "INR",
		ExchangeRate:        "1.0",
		PostingDate:         "2023-10-01",
		Lines: []core.ProposalLine{
			{AccountCode: "1000", IsDebit: true, Amount: "200.00"},
			{AccountCode: "1100", IsDebit: false, Amount: ""},
		},
	}

	p.Normalize()
	if err := p.Validate(); err == nil {
		t.Errorf("expected error after normalization due to zero amount, got nil")
	}
}

func TestProposal_NormalizationAndValidation(t *testing.T) {
	tests := []struct {
		name                string
		documentTypeCode    string
		transactionCurrency string
		exchangeRate        string
		lines               []core.ProposalLine
		expectErr           bool
	}{
		{
			name:                "Happy Path (local currency)",
			documentTypeCode:    "JE",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "200.00"},
				{AccountCode: "1100", IsDebit: false, Amount: "200.00"},
			},
			expectErr: false,
		},
		{
			name:                "Happy Path (foreign currency USD)",
			documentTypeCode:    "JE",
			transactionCurrency: "USD",
			exchangeRate:        "83.50",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "500.00"},
				{AccountCode: "4000", IsDebit: false, Amount: "500.00"},
			},
			expectErr: false,
		},
		{
			name:                "Blank credit amount",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "200.00"},
				{AccountCode: "1100", IsDebit: false, Amount: ""},
			},
			expectErr: true, // normalizes to 0.00, fails > 0 check
		},
		{
			name:                "Amount zero",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "0.00"},
				{AccountCode: "1000", IsDebit: false, Amount: "0.00"},
			},
			expectErr: true,
		},
		{
			name:                "Negative amount",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "-100.00"},
			},
			expectErr: true,
		},
		{
			name:                "Imbalanced entry",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "200.00"},
				{AccountCode: "1100", IsDebit: false, Amount: "100.00"},
			},
			expectErr: true,
		},
		{
			name:                "Missing company code",
			transactionCurrency: "INR",
			exchangeRate:        "1.0",
			lines: []core.ProposalLine{
				{AccountCode: "1000", IsDebit: true, Amount: "100.00"},
				{AccountCode: "1100", IsDebit: false, Amount: "100.00"},
			},
			expectErr: true, // CompanyCode will be "" in test below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			companyCode := "1000"
			if tt.name == "Missing company code" {
				companyCode = ""
			}
			p := core.Proposal{
				DocumentTypeCode:    tt.documentTypeCode,
				CompanyCode:         companyCode,
				TransactionCurrency: tt.transactionCurrency,
				ExchangeRate:        tt.exchangeRate,
				PostingDate:         "2023-10-01",
				Lines:               tt.lines,
			}
			p.Normalize()
			err := p.Validate()

			if tt.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v, proposal: %+v", err, p)
			}
		})
	}
}
