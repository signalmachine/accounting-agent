package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Normalize cleans up user input (LLM output) dealing with common formatting issues.
func (p *Proposal) Normalize() {
	// Normalize header-level currency fields
	p.TransactionCurrency = strings.ToUpper(strings.TrimSpace(p.TransactionCurrency))
	p.PostingDate = strings.TrimSpace(p.PostingDate)
	p.DocumentDate = strings.TrimSpace(p.DocumentDate)

	if p.DocumentDate == "" && p.PostingDate != "" {
		p.DocumentDate = p.PostingDate
	}

	if strings.TrimSpace(p.ExchangeRate) == "" || strings.ToLower(p.ExchangeRate) == "null" || p.ExchangeRate == "0" || p.ExchangeRate == "0.0" {
		p.ExchangeRate = "1.0"
	}

	for i := range p.Lines {
		line := &p.Lines[i]

		// Handle empty or "null" amounts
		if strings.TrimSpace(line.Amount) == "" || strings.ToLower(line.Amount) == "null" {
			line.Amount = "0.00"
		}
	}
}

// Validate enforces strict accounting rules on the proposal.
// KEY SAP RULE: All lines in a proposal share the same TransactionCurrency and ExchangeRate.
// This prevents mixed-currency journal entries. A transaction is either in local currency
// or in a single foreign currency — never a mix.
func (p *Proposal) Validate() error {
	if p.DocumentTypeCode == "" {
		return errors.New("proposal must specify a document type code")
	}

	if p.CompanyCode == "" {
		return errors.New("proposal must specify a company code")
	}

	if p.TransactionCurrency == "" {
		return errors.New("proposal must specify a transaction currency")
	}

	if p.PostingDate == "" {
		return errors.New("proposal must specify a posting date")
	}

	// Validate date formats
	if _, err := time.Parse("2006-01-02", p.PostingDate); err != nil {
		return fmt.Errorf("invalid posting date format: %w", err)
	}
	if p.DocumentDate != "" {
		if _, err := time.Parse("2006-01-02", p.DocumentDate); err != nil {
			return fmt.Errorf("invalid document date format: %w", err)
		}
	}

	// Parse header-level exchange rate
	rate, err := decimal.NewFromString(p.ExchangeRate)
	if err != nil {
		return fmt.Errorf("invalid exchange rate %q: %v", p.ExchangeRate, err)
	}
	if rate.IsNegative() || rate.IsZero() {
		return fmt.Errorf("exchange rate must be > 0, got %s", p.ExchangeRate)
	}

	if len(p.Lines) < 2 {
		return errors.New("transaction must have at least 2 lines")
	}

	totalDebitBase := decimal.Zero
	totalCreditBase := decimal.Zero

	for _, line := range p.Lines {
		// Parse Amount (in TransactionCurrency)
		amt, err := decimal.NewFromString(line.Amount)
		if err != nil {
			return fmt.Errorf("invalid amount %q for account %s: %v", line.Amount, line.AccountCode, err)
		}

		if amt.IsNegative() {
			return fmt.Errorf("amount cannot be negative for account %s", line.AccountCode)
		}
		if amt.IsZero() {
			return fmt.Errorf("amount must be > 0 for account %s", line.AccountCode)
		}

		// Base amount = transaction amount × header exchange rate
		baseAmt := amt.Mul(rate)

		if line.IsDebit {
			totalDebitBase = totalDebitBase.Add(baseAmt)
		} else {
			totalCreditBase = totalCreditBase.Add(baseAmt)
		}
	}

	// Base currency debits must exactly equal base currency credits
	if !totalDebitBase.Equal(totalCreditBase) {
		return fmt.Errorf("base currency imbalance: debits %s != credits %s", totalDebitBase, totalCreditBase)
	}

	return nil
}
