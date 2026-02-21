package core

import "time"

type AccountType string

const (
	Asset     AccountType = "asset"
	Liability AccountType = "liability"
	Equity    AccountType = "equity"
	Revenue   AccountType = "revenue"
	Expense   AccountType = "expense"
)

type Account struct {
	ID   int         `json:"id"`
	Code string      `json:"code"`
	Name string      `json:"name"`
	Type AccountType `json:"type"`
}

type JournalEntry struct {
	ID              int           `json:"id"`
	CreatedAt       time.Time     `json:"created_at"`
	Narration       string        `json:"narration"`
	ReferenceType   *string       `json:"reference_type,omitempty"`
	ReferenceID     *string       `json:"reference_id,omitempty"`
	Reasoning       string        `json:"reasoning"`
	ReversedEntryID *int          `json:"reversed_entry_id,omitempty"`
	Lines           []JournalLine `json:"lines"`
}

type JournalLine struct {
	ID        int    `json:"id"`
	EntryID   int    `json:"entry_id"`
	AccountID int    `json:"account_id"`
	Debit     string `json:"debit"`
	Credit    string `json:"credit"`
}

type ProposalLine struct {
	AccountCode string `json:"account_code" jsonschema_description:"The exact account code from the provided Chart of Accounts"`
	Debit       string `json:"debit" jsonschema_description:"Debit amount as a string (e.g. '100.00')"`
	Credit      string `json:"credit" jsonschema_description:"Credit amount as a string (e.g. '0.00')"`
}

type Proposal struct {
	Summary    string         `json:"summary" jsonschema_description:"A brief summary of the business event"`
	Confidence float64        `json:"confidence" jsonschema_description:"Confidence score between 0.0 and 1.0"`
	Reasoning  string         `json:"reasoning" jsonschema_description:"Explanation for the proposed journal entry"`
	Lines      []ProposalLine `json:"lines" jsonschema_description:"List of debit and credit lines"`
}
