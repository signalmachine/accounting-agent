package core_test

import (
	"accounting-agent/internal/core"
	"testing"
)

func TestProposal_Validate_Reproduction(t *testing.T) {
	// 1. Blank strings should fail without normalization (or with current logic)
	// This reproduction test now verifies that Normalize + Validate fixes the issue.

	p := core.Proposal{
		Lines: []core.ProposalLine{
			{AccountCode: "1000", Debit: "200.00", Credit: ""},
			{AccountCode: "1100", Debit: "", Credit: "200.00"},
		},
	}

	p.Normalize()
	if err := p.Validate(); err != nil {
		t.Errorf("expected nil error after normalization, got %v", err)
	}
}

func TestProposal_NormalizationAndValidation(t *testing.T) {
	tests := []struct {
		name      string
		lines     []core.ProposalLine
		expectErr bool
	}{
		{
			name: "Happy Path",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "200.00", Credit: "0.00"},
				{AccountCode: "1100", Debit: "0.00", Credit: "200.00"},
			},
			expectErr: false,
		},
		{
			name: "Blank strings (should be normalized)",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "200.00", Credit: ""},
				{AccountCode: "1100", Debit: "", Credit: "200.00"},
			},
			expectErr: false, // Should pass after normalization
		},
		{
			name: "Both Debit and Credit > 0 (Fail)",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "100.00", Credit: "100.00"},
			},
			expectErr: true,
		},
		{
			name: "Both Debit and Credit 0 (Fail - at least one > 0)",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "0.00", Credit: "0.00"},
			},
			expectErr: true,
		},
		{
			name: "Negative values (Fail)",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "-100.00", Credit: "0.00"},
			},
			expectErr: true,
		},
		{
			name: "Total Debits != Total Credits (Fail)",
			lines: []core.ProposalLine{
				{AccountCode: "1000", Debit: "200.00", Credit: "0.00"},
				{AccountCode: "1100", Debit: "0.00", Credit: "100.00"},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := core.Proposal{Lines: tt.lines}
			p.Normalize()       // We assume this is available now
			err := p.Validate() // We assume this is available now

			if tt.expectErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v, proposal: %+v", err, p)
			}
		})
	}
}
