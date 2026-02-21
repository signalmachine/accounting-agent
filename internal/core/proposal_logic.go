package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Normalize cleans up user input (LLM output) dealing with common formatting issues.
func (p *Proposal) Normalize() {
	for i := range p.Lines {
		line := &p.Lines[i]

		// 1. Handle empty strings or "null" string literal
		if strings.TrimSpace(line.Debit) == "" || strings.ToLower(line.Debit) == "null" {
			line.Debit = "0.00"
		}
		if strings.TrimSpace(line.Credit) == "" || strings.ToLower(line.Credit) == "null" {
			line.Credit = "0.00"
		}
	}
}

// Validate enforces strict accounting rules on the proposal.
func (p *Proposal) Validate() error {
	if len(p.Lines) < 2 {
		return errors.New("transaction must have at least 2 lines")
	}

	totalDebit := decimal.Zero
	totalCredit := decimal.Zero

	for _, line := range p.Lines {
		// Parse Debit
		d, err := decimal.NewFromString(line.Debit)
		if err != nil {
			return fmt.Errorf("invalid debit amount for account %s: %v", line.AccountCode, err)
		}
		// Parse Credit
		c, err := decimal.NewFromString(line.Credit)
		if err != nil {
			return fmt.Errorf("invalid credit amount for account %s: %v", line.AccountCode, err)
		}

		// Validation Rule: Amounts must be non-negative
		if d.IsNegative() || c.IsNegative() {
			return fmt.Errorf("amounts cannot be negative for account %s", line.AccountCode)
		}

		// Validation Rule: Cannot be both > 0
		if d.GreaterThan(decimal.Zero) && c.GreaterThan(decimal.Zero) {
			return fmt.Errorf("account %s cannot have both debit and credit > 0", line.AccountCode)
		}

		// Validation Rule: At least one must be > 0
		if d.IsZero() && c.IsZero() {
			return fmt.Errorf("account %s must have either debit or credit > 0", line.AccountCode)
		}

		totalDebit = totalDebit.Add(d)
		totalCredit = totalCredit.Add(c)
	}

	// Validation Rule: Debits must equal Credits
	if !totalDebit.Equal(totalCredit) {
		return fmt.Errorf("credits do not equal debits: %s != %s", totalDebit, totalCredit)
	}

	return nil
}
