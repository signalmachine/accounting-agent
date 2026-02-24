package ai

import (
	"accounting-agent/internal/core"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

type AgentService interface {
	InterpretEvent(ctx context.Context, naturalLanguage string, chartOfAccounts string, documentTypes string, company *core.Company) (*core.AgentResponse, error)
}

type Agent struct {
	client *openai.Client
}

func NewAgent(apiKey string) *Agent {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(3),
	)
	return &Agent{client: &client}
}

func (a *Agent) InterpretEvent(ctx context.Context, naturalLanguage string, chartOfAccounts string, documentTypes string, company *core.Company) (*core.AgentResponse, error) {
	prompt := fmt.Sprintf(`You are an expert accountant operating within a multi-currency, multi-company ledger system.
Your goal is to interpret a business event described in natural language and propose a double-entry journal entry.
You MUST use the provided Chart of Accounts and Document Types.

CONTEXT:
Company Code: %s
Company Name: %s
Base Currency (Local Currency): %s

SAP CURRENCY RULES — READ CAREFULLY:
1. Each journal entry uses ONE transaction currency for ALL lines. Mixed currencies within a single entry are FORBIDDEN.
2. Identify the Transaction Currency from the event (e.g., if the user says "$500", the TransactionCurrency is "USD").
3. Set a single ExchangeRate for the whole entry (TransactionCurrency → Base Currency "%s"). If TransactionCurrency equals Base Currency, use "1.0".
4. Every line's Amount is in the TransactionCurrency. Do NOT mix currencies across lines.
5. Use ONLY account codes from the provided list below.
6. Create at least two lines. IsDebit=true for debit lines, IsDebit=false for credit lines.
7. In Base Currency: sum(Amount * ExchangeRate) for debits must equal sum(Amount * ExchangeRate) for credits.
8. Amounts are always positive numbers (no currency symbols, no negatives).
9. Extract a PostingDate (YYYY-MM-DD format) from the text. Use Today's Date below if context implies "today" or "now", or if completely unspecified use Today's Date.
10. Extract a DocumentDate (YYYY-MM-DD format). If there isn't a separate document date mentioned (like "invoice dated last week"), it defaults to the PostingDate.
11. Provide confidence (0.0-1.0) and brief reasoning.

DOCUMENT TYPE SELECTION:
1. Analyze the user's text. If they are talking about selling a product or service, set the type to 'SI' (Sales Invoice). If they are buying supplies or services, set it to 'PI' (Purchase Invoice). Otherwise, default to 'JE' (Journal Entry).
2. You MUST select a valid DocumentTypeCode from the list provided below.

CLARIFICATIONS:
If the user does not provide enough clues to confidently determine the Document Type, or if critical financial information (like amounts, parties, or intent) is missing, do NOT guess. Instead, set is_clarification_request to true, and provide a clarification message asking the user to specify the missing details (e.g., 'Please specify if this is a Sales Invoice, Purchase Invoice, or Journal Entry, and what the amount was.').

NON-ACCOUNTING INPUTS:
If the user's input is NOT a financial accounting event (e.g. they are asking to list orders, view customers, confirm a shipment, check products, or perform any operational task), do NOT attempt to create a journal entry. Instead, set is_clarification_request to true and respond with a helpful redirect pointing to the relevant slash command. Examples: "To list orders, use /orders.", "To confirm an order, use /confirm <order-ref>.", "To list customers, use /customers.", "For all available commands, type /help."

Today's Date: %s

Document Types:
%s

Chart of Accounts:
%s

Event: %s`, company.CompanyCode, company.Name, company.BaseCurrency, company.BaseCurrency, time.Now().Format("2006-01-02"), documentTypes, chartOfAccounts, naturalLanguage)

	// Enforce a hard timeout on the OpenAI API call.
	// Without this, a slow or unresponsive API will block the REPL indefinitely.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Build the strict OpenAI-compliant schema
	schemaMap := generateSchema()

	params := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4o,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(prompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Type:        constant.JSONSchema("json_schema"),
					Name:        "agent_response",
					Strict:      openai.Bool(true),
					Schema:      schemaMap,
					Description: openai.String("Either a clarification request or a double-entry proposal"),
				},
			},
		},
	}

	resp, err := a.client.Responses.New(ctx, params)
	if err != nil {
		var apierr *openai.Error
		if errors.As(err, &apierr) {
			log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
		}
		return nil, fmt.Errorf("openai responses error: %w", err)
	}

	if usage := resp.Usage; usage.TotalTokens > 0 {
		log.Printf("OpenAI usage — prompt: %d, completion: %d, total: %d tokens",
			usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	}

	content := resp.OutputText()
	if content == "" {
		return nil, fmt.Errorf("empty response content")
	}

	var response core.AgentResponse
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return nil, fmt.Errorf("failed to parse completion: %w", err)
	}

	if response.IsClarificationRequest {
		if response.Clarification == nil || response.Clarification.Message == "" {
			return nil, fmt.Errorf("clarification request was marked true but no message was provided")
		}
		return &response, nil
	}

	if response.Proposal == nil {
		return nil, fmt.Errorf("is_clarification_request was false but no proposal was provided")
	}

	response.Proposal.Normalize()
	if err := response.Proposal.Validate(); err != nil {
		return nil, fmt.Errorf("proposal validation failed: %w", err)
	}

	response.Proposal.IdempotencyKey = uuid.NewString()

	return &response, nil
}

// generateSchema returns a JSON schema for AgentResponse that is fully compliant
// with OpenAI strict mode:
//   - Every property is listed in "required"
//   - Nullable (pointer) fields use anyOf: [{schema}, {type: "null"}]
//   - additionalProperties: false on every object
func generateSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"is_clarification_request", "clarification", "proposal"},
		"properties": map[string]any{
			"is_clarification_request": map[string]any{
				"type":        "boolean",
				"description": "Set to true ONLY if you lack enough information to create a confident proposal.",
			},
			"clarification": map[string]any{
				"description": "Required if is_clarification_request is true. Null otherwise.",
				"anyOf": []any{
					map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"message"},
						"properties": map[string]any{
							"message": map[string]any{
								"type":        "string",
								"description": "A question asking the user for missing details.",
							},
						},
					},
					map[string]any{"type": "null"},
				},
			},
			"proposal": map[string]any{
				"description": "Required if is_clarification_request is false. Null otherwise.",
				"anyOf": []any{
					proposalSchema(),
					map[string]any{"type": "null"},
				},
			},
		},
	}
}

func proposalSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"document_type_code", "company_code", "idempotency_key",
			"transaction_currency", "exchange_rate", "summary",
			"posting_date", "document_date", "confidence", "reasoning", "lines",
		},
		"properties": map[string]any{
			"document_type_code": map[string]any{
				"type":        "string",
				"description": "Document type code: 'JE', 'SI', or 'PI'.",
			},
			"company_code": map[string]any{
				"type":        "string",
				"description": "The 4-character company code (e.g., '1000').",
			},
			"idempotency_key": map[string]any{
				"type":        "string",
				"description": "Leave empty string. A UUID will be assigned by the system.",
			},
			"transaction_currency": map[string]any{
				"type":        "string",
				"description": "ISO currency code for this transaction (e.g., 'USD', 'INR').",
			},
			"exchange_rate": map[string]any{
				"type":        "string",
				"description": "Exchange rate of TransactionCurrency to base currency. Use '1.0' if same.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Brief summary of the business event.",
			},
			"posting_date": map[string]any{
				"type":        "string",
				"description": "Accounting period date in YYYY-MM-DD format.",
			},
			"document_date": map[string]any{
				"type":        "string",
				"description": "Real-world transaction date in YYYY-MM-DD format. Defaults to posting_date.",
			},
			"confidence": map[string]any{
				"type":        "number",
				"description": "Confidence score between 0.0 and 1.0.",
			},
			"reasoning": map[string]any{
				"type":        "string",
				"description": "Explanation for the proposed journal entry.",
			},
			"lines": map[string]any{
				"type":        "array",
				"description": "Debit and credit lines. All share the header currency and exchange rate.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"account_code", "is_debit", "amount"},
					"properties": map[string]any{
						"account_code": map[string]any{
							"type":        "string",
							"description": "Exact account code from the Chart of Accounts.",
						},
						"is_debit": map[string]any{
							"type":        "boolean",
							"description": "True if debit, false if credit.",
						},
						"amount": map[string]any{
							"type":        "string",
							"description": "Positive monetary amount as a string, in TransactionCurrency.",
						},
					},
				},
			},
		},
	}
}
