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
	ID        int         `json:"id"`
	CompanyID int         `json:"company_id"`
	Code      string      `json:"code"`
	Name      string      `json:"name"`
	Type      AccountType `json:"type"`
}

type Company struct {
	ID           int    `json:"id"`
	CompanyCode  string `json:"company_code"`
	Name         string `json:"name"`
	BaseCurrency string `json:"base_currency"`
}

type JournalEntry struct {
	ID              int           `json:"id"`
	CompanyID       int           `json:"company_id"`
	IdempotencyKey  string        `json:"idempotency_key,omitempty"`
	PostingDate     time.Time     `json:"posting_date"`
	DocumentDate    time.Time     `json:"document_date"`
	CreatedAt       time.Time     `json:"created_at"`
	Narration       string        `json:"narration"`
	ReferenceType   *string       `json:"reference_type,omitempty"`
	ReferenceID     *string       `json:"reference_id,omitempty"`
	Reasoning       string        `json:"reasoning"`
	ReversedEntryID *int          `json:"reversed_entry_id,omitempty"`
	Lines           []JournalLine `json:"lines"`
}

type JournalLine struct {
	ID                  int    `json:"id"`
	EntryID             int    `json:"entry_id"`
	AccountID           int    `json:"account_id"`
	TransactionCurrency string `json:"transaction_currency"`
	ExchangeRate        string `json:"exchange_rate"`
	AmountTransaction   string `json:"amount_transaction"`
	DebitBase           string `json:"debit_base"`
	CreditBase          string `json:"credit_base"`
}

// ProposalLine represents a single debit or credit line in a journal entry proposal.
// NOTE: Currency is a header-level field on Proposal. All lines in one entry share
// the same TransactionCurrency and ExchangeRate (SAP model — no mixed-currency entries).
type ProposalLine struct {
	AccountCode string `json:"account_code" jsonschema_description:"The exact account code from the provided Chart of Accounts"`
	IsDebit     bool   `json:"is_debit" jsonschema_description:"True if this line is a debit, false for credit"`
	Amount      string `json:"amount" jsonschema_description:"The exact monetary amount of this single line (always positive) as a string, in the TransactionCurrency"`
}

// Proposal is the AI-generated journal entry proposal.
// TransactionCurrency and ExchangeRate are header-level: all lines use the same currency.
type Proposal struct {
	DocumentTypeCode    string         `json:"document_type_code" jsonschema_description:"The 2-character code for the document type (e.g., 'JE', 'SI', 'PI'). Must be one of the provided Document Types."`
	CompanyCode         string         `json:"company_code" jsonschema_description:"The 4-character code identifying the company this transaction belongs to"`
	IdempotencyKey      string         `json:"idempotency_key" jsonschema_description:"A unique identifier to prevent duplicate entries"`
	TransactionCurrency string         `json:"transaction_currency" jsonschema_description:"The ISO currency code for this transaction (e.g., 'USD', 'INR'). All lines in a journal entry must use the same currency."`
	ExchangeRate        string         `json:"exchange_rate" jsonschema_description:"The exchange rate of the TransactionCurrency to the company base currency. Use '1.0' if the transaction currency matches the base currency."`
	Summary             string         `json:"summary" jsonschema_description:"A brief summary of the business event"`
	PostingDate         string         `json:"posting_date" jsonschema_description:"The accounting period control date in YYYY-MM-DD format. Extrapolate from context or use today's date if unspecified."`
	DocumentDate        string         `json:"document_date" jsonschema_description:"The real-world transaction date in YYYY-MM-DD format (e.g. invoice date). Defaults to PostingDate if unknown."`
	Confidence          float64        `json:"confidence" jsonschema_description:"Confidence score between 0.0 and 1.0"`
	Reasoning           string         `json:"reasoning" jsonschema_description:"Explanation for the proposed journal entry"`
	Lines               []ProposalLine `json:"lines" jsonschema_description:"List of debit and credit lines. All lines share the header TransactionCurrency and ExchangeRate."`
}

// ClarificationRequest is returned by the AI when the user's input is ambiguous or missing critical information.
type ClarificationRequest struct {
	Message string `json:"message" jsonschema_description:"A message asking the user for missing details (e.g., 'Please specify if this is a Sales Invoice or a Journal Entry, and the total amount.')."`
}

// AgentResponse wraps the AI output to handle branching between a valid Proposal or a ClarificationRequest.
// The AI must return exactly one of these objects.
type AgentResponse struct {
	IsClarificationRequest bool                  `json:"is_clarification_request" jsonschema_description:"Set to true ONLY if you lack enough information to create a confident proposal."`
	Clarification          *ClarificationRequest `json:"clarification,omitempty" jsonschema_description:"Required if is_clarification_request is true."`
	Proposal               *Proposal             `json:"proposal,omitempty" jsonschema_description:"Required if is_clarification_request is false."`
}

type DocumentStatus string

const (
	DocumentStatusDraft     DocumentStatus = "DRAFT"
	DocumentStatusPosted    DocumentStatus = "POSTED"
	DocumentStatusCancelled DocumentStatus = "CANCELLED"
)

type DocumentType struct {
	Code              string `json:"code"`
	Name              string `json:"name"`
	AffectsInventory  bool   `json:"affects_inventory"`
	AffectsGL         bool   `json:"affects_gl"`
	AffectsAR         bool   `json:"affects_ar"`
	AffectsAP         bool   `json:"affects_ap"`
	NumberingStrategy string `json:"numbering_strategy"` // 'global', 'per_fy', 'per_branch'
	ResetsEveryFY     bool   `json:"resets_every_fy"`
}

type Document struct {
	ID             int            `json:"id"`
	CompanyID      int            `json:"company_id"`
	TypeCode       string         `json:"type_code"`
	Status         DocumentStatus `json:"status"`
	DocumentNumber *string        `json:"document_number,omitempty"`
	FinancialYear  *int           `json:"financial_year,omitempty"`
	BranchID       *int           `json:"branch_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	PostedAt       *time.Time     `json:"posted_at,omitempty"`
}

type DocumentSequence struct {
	CompanyID     int    `json:"company_id"`
	TypeCode      string `json:"type_code"`
	FinancialYear *int   `json:"financial_year,omitempty"`
	BranchID      *int   `json:"branch_id,omitempty"`
	LastNumber    int64  `json:"last_number"`
}
