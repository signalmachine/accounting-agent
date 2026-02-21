package ai

import (
	"accounting-agent/internal/core"
	"context"
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/openai/openai-go/shared/constant"
)

type AgentService interface {
	InterpretEvent(ctx context.Context, naturalLanguage string, chartOfAccounts string) (*core.Proposal, error)
}

type Agent struct {
	client *openai.Client
}

func NewAgent(apiKey string) *Agent {
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Agent{client: &client}
}

func (a *Agent) InterpretEvent(ctx context.Context, naturalLanguage string, chartOfAccounts string) (*core.Proposal, error) {
	prompt := fmt.Sprintf(`You are an expert accountant.
Your goal is to interpret a business event described in natural language and propose a double-entry journal entry.
You MUST use the provided Chart of Accounts.
Rules:
1. Use ONLY account codes from the list below.
2. Debits MUST equal Credits.
3. Amounts must be exact strings (e.g. "100.00").
4. Provide a confidence score (0.0-1.0).
5. Explain your reasoning.

Chart of Accounts:
%s

Event: %s`, chartOfAccounts, naturalLanguage)

	// Dynamically generate the JSON schema from the Go struct
	schemaStruct := generateSchema()
	schemaJSON, err := json.Marshal(schemaStruct)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(schemaJSON, &schemaMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema to map: %w", err)
	}

	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(shared.ChatModelGPT4o),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: param.NewOpt(prompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Type:        constant.JSONSchema("json_schema"),
					Name:        "journal_entry_proposal",
					Strict:      param.NewOpt(true),
					Schema:      schemaMap,
					Description: param.NewOpt("A proposal for a double-entry accounting journal entry"),
				},
			},
		},
	}

	resp, err := a.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai responses error: %w", err)
	}

	content := resp.OutputText()
	if content == "" {
		return nil, fmt.Errorf("empty response content")
	}

	var proposal core.Proposal
	if err := json.Unmarshal([]byte(content), &proposal); err != nil {
		return nil, fmt.Errorf("failed to parse completion: %w", err)
	}

	proposal.Normalize()
	if err := proposal.Validate(); err != nil {
		return nil, fmt.Errorf("proposal validation failed: %w", err)
	}

	return &proposal, nil
}

func generateSchema() interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v core.Proposal
	return reflector.Reflect(v)
}
