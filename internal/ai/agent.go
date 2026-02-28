package ai

import (
	"accounting-agent/internal/core"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

// AgentDomainResultKind identifies the terminal outcome of an InterpretDomainAction call.
type AgentDomainResultKind string

const (
	// AgentDomainResultKindAnswer means the agent gathered context via read tools and produced a plain-text answer.
	AgentDomainResultKindAnswer AgentDomainResultKind = "answer"
	// AgentDomainResultKindClarification means the agent needs more information from the user.
	AgentDomainResultKindClarification AgentDomainResultKind = "clarification"
	// AgentDomainResultKindProposed means the agent is proposing a write-tool action for human confirmation.
	AgentDomainResultKindProposed AgentDomainResultKind = "proposed"
	// AgentDomainResultKindJournalEntry means the input is a financial accounting event;
	// the caller should route it to InterpretEvent for structured-output journal entry proposal.
	AgentDomainResultKindJournalEntry AgentDomainResultKind = "journal_entry"
)

// AgentDomainResult is the terminal output of InterpretDomainAction.
type AgentDomainResult struct {
	Kind AgentDomainResultKind

	// Answer is populated when Kind == AgentDomainResultKindAnswer.
	Answer string

	// Question is populated when Kind == AgentDomainResultKindClarification.
	Question string
	// Context is additional context the agent has established so far (for clarification).
	Context string

	// ToolName and ToolArgs are populated when Kind == AgentDomainResultKindProposed.
	ToolName string
	ToolArgs map[string]any

	// EventDescription is populated when Kind == AgentDomainResultKindJournalEntry.
	// It contains the user's original event description (possibly refined) to pass to InterpretEvent.
	EventDescription string
}

type AgentService interface {
	// InterpretEvent interprets a natural language event as a double-entry journal entry proposal.
	// This path uses structured output (Responses API JSON schema mode) and must remain untouched
	// until InterpretDomainAction has been stable across ≥2 domain phases with write tools.
	InterpretEvent(ctx context.Context, naturalLanguage string, chartOfAccounts string, documentTypes string, company *core.Company) (*core.AgentResponse, error)

	// InterpretDomainAction routes a natural language input through the agentic tool loop.
	// The agent calls read tools autonomously to gather context, then either proposes a write
	// tool (for human confirmation), asks a clarifying question, returns an answer, or signals
	// that the input is a financial event to be handled by InterpretEvent.
	// InterpretEvent is not called or modified by this method.
	// attachments is optional — when non-empty, image content is passed to the vision model.
	InterpretDomainAction(ctx context.Context, userInput string, company *core.Company, registry *ToolRegistry, attachments []Attachment) (*AgentDomainResult, error)
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

// InterpretDomainAction runs the agentic tool loop for a user's natural language input.
//
// Loop invariants (enforced here, per §14.3 of ai_agent_upgrade.md):
//   - Read tools are executed autonomously — results are fed back to the model.
//   - The loop terminates when the model produces a text message (answer), calls a write
//     tool (proposed action or meta-tool), or the 5-iteration cap is reached.
//   - InterpretEvent is not called or modified by this method.
//   - attachments is optional — when non-empty, image content is passed via the vision API.
func (a *Agent) InterpretDomainAction(ctx context.Context, userInput string, company *core.Company, registry *ToolRegistry, attachments []Attachment) (*AgentDomainResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	systemPrompt := fmt.Sprintf(`You are an expert business assistant for %s (%s, base currency: %s).

You have access to tools to look up accounts, customers, products, stock levels, and warehouses.
Use read tools to gather the information you need before responding.

ROUTING RULES — follow these exactly:
1. If the user asks a question about accounts, customers, products, or stock: call the relevant read tools and provide a clear answer.
2. If the user is describing a financial accounting event (recording a payment, posting an expense, booking a journal entry, recording revenue): call route_to_journal_entry with the event description.
3. If you need more information before you can help: call request_clarification with a specific question.
4. If you have gathered enough information via read tools: respond with a plain-text answer.

TOOL USAGE:
- Call read tools as many times as needed to gather context.
- Do not guess account codes or customer names — always verify via search tools.
- After calling read tools, provide a specific, actionable response.

Today's date: %s`, company.Name, company.CompanyCode, company.BaseCurrency, time.Now().Format("2006-01-02"))

	tools := registry.ToOpenAITools()

	// Add meta-tools that terminate the loop.
	tools = append(tools,
		responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        "request_clarification",
				Description: openai.String("Use this when you need more information from the user before you can help. Ask one specific question."),
				Parameters: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The specific question to ask the user.",
						},
						"context": map[string]any{
							"type":        "string",
							"description": "What you have established so far, to give the user context.",
						},
					},
					"required": []string{"question", "context"},
				},
			},
		},
		responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:        "route_to_journal_entry",
				Description: openai.String("Use this when the user is describing a financial accounting event that requires a journal entry (e.g. recording a payment, posting an expense, booking revenue). Do NOT use this for queries or lookups."),
				Parameters: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"event_description": map[string]any{
							"type":        "string",
							"description": "The user's event description, preserved verbatim or lightly cleaned up.",
						},
					},
					"required": []string{"event_description"},
				},
			},
		},
	)

	const maxLoops = 5
	prevRespID := ""

	// Build the initial input. If images are attached, use a content list; otherwise plain string.
	var inputParam responses.ResponseNewParamsInputUnion
	if len(attachments) == 0 {
		inputParam = responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userInput),
		}
	} else {
		contentList := responses.ResponseInputMessageContentListParam{
			responses.ResponseInputContentParamOfInputText(userInput),
		}
		for _, att := range attachments {
			b64 := base64.StdEncoding.EncodeToString(att.Data)
			dataURL := fmt.Sprintf("data:%s;base64,%s", att.MimeType, b64)
			contentList = append(contentList, responses.ResponseInputContentUnionParam{
				OfInputImage: &responses.ResponseInputImageParam{
					Detail:   responses.ResponseInputImageDetailAuto,
					ImageURL: param.NewOpt(dataURL),
				},
			})
		}
		inputParam = responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{
				responses.ResponseInputItemParamOfMessage(contentList, responses.EasyInputMessageRoleUser),
			},
		}
	}

	for i := 0; i < maxLoops; i++ {
		params := responses.ResponseNewParams{
			Model:        openai.ChatModelGPT4o,
			Instructions: openai.String(systemPrompt),
			Tools:        tools,
		}
		if prevRespID != "" {
			params.PreviousResponseID = openai.String(prevRespID)
		}
		params.Input = inputParam

		resp, err := a.client.Responses.New(ctx, params)
		if err != nil {
			var apierr *openai.Error
			if errors.As(err, &apierr) {
				log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
			}
			return nil, fmt.Errorf("openai responses error: %w", err)
		}

		if usage := resp.Usage; usage.TotalTokens > 0 {
			log.Printf("OpenAI usage (domain action) — prompt: %d, completion: %d, total: %d tokens",
				usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}

		prevRespID = resp.ID

		// Collect read tool calls; a single write tool or text message terminates the loop.
		var toolResults []responses.ResponseInputItemUnionParam
		hasReadToolCalls := false

		for _, item := range resp.Output {
			switch item.Type {
			case "message":
				// Agent produced a text response — this is the "answer" outcome.
				text := resp.OutputText()
				return &AgentDomainResult{Kind: AgentDomainResultKindAnswer, Answer: text}, nil

			case "function_call":
				fc := item.AsFunctionCall()

				// Meta-tools terminate the loop immediately.
				if fc.Name == "request_clarification" || fc.Name == "route_to_journal_entry" {
					var args map[string]any
					if err := json.Unmarshal([]byte(fc.Arguments), &args); err != nil {
						return nil, fmt.Errorf("failed to parse %s args: %w", fc.Name, err)
					}
					if fc.Name == "request_clarification" {
						question, _ := args["question"].(string)
						ctx2, _ := args["context"].(string)
						return &AgentDomainResult{
							Kind:     AgentDomainResultKindClarification,
							Question: question,
							Context:  ctx2,
						}, nil
					}
					// route_to_journal_entry
					desc, _ := args["event_description"].(string)
					if desc == "" {
						desc = userInput
					}
					return &AgentDomainResult{
						Kind:             AgentDomainResultKindJournalEntry,
						EventDescription: desc,
					}, nil
				}

				// Look up the tool in the registry.
				tool, ok := registry.Get(fc.Name)
				if !ok {
					return nil, fmt.Errorf("agent called unregistered tool: %s", fc.Name)
				}

				if !tool.IsReadTool {
					// Write tool — return as proposed action for human confirmation.
					var args map[string]any
					if err := json.Unmarshal([]byte(fc.Arguments), &args); err != nil {
						return nil, fmt.Errorf("failed to parse write tool args for %s: %w", fc.Name, err)
					}
					return &AgentDomainResult{
						Kind:     AgentDomainResultKindProposed,
						ToolName: fc.Name,
						ToolArgs: args,
					}, nil
				}

				// Read tool — execute autonomously and collect result.
				var args map[string]any
				if err := json.Unmarshal([]byte(fc.Arguments), &args); err != nil {
					return nil, fmt.Errorf("failed to parse read tool args for %s: %w", fc.Name, err)
				}
				resultStr, handlerErr := tool.Handler(ctx, args)
				if handlerErr != nil {
					resultStr = fmt.Sprintf(`{"error": %q}`, handlerErr.Error())
				}
				toolResults = append(toolResults,
					responses.ResponseInputItemParamOfFunctionCallOutput(fc.CallID, resultStr))
				hasReadToolCalls = true
			}
		}

		if !hasReadToolCalls {
			// No tool calls and no text message — unexpected.
			text := resp.OutputText()
			if text != "" {
				return &AgentDomainResult{Kind: AgentDomainResultKindAnswer, Answer: text}, nil
			}
			return nil, fmt.Errorf("agent returned no output in iteration %d", i+1)
		}

		// Feed all read tool results back for the next iteration.
		inputParam = responses.ResponseNewParamsInputUnion{
			OfInputItemList: toolResults,
		}
	}

	return nil, fmt.Errorf("agent tool loop exceeded maximum iterations (%d) without reaching a conclusion", maxLoops)
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
