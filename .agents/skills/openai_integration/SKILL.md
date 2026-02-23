---
name: OpenAI SDK Integration
description: Strict guidelines, constraints, and reference patterns for using the OpenAI Go SDK Responses API in this project.
---
# OpenAI Go SDK Integration Guide

## 1. Mandate: Use the 'Responses API'
This application **strictly enforces** the use of the new **Responses API** (`client.Responses.New`) provided by the official `github.com/openai/openai-go` SDK.

### Why?
The Responses API is designed for **structured outputs** and robust schema enforcement, which is critical for our application's "Code is Law" philosophy. It allows us to define a strict JSON schema for the accounting `Proposal`, ensuring the AI always returns valid, parseable data.

### ❌ FORBIDDEN: Chat Completions API
Do **NOT** use the legacy `client.Chat.Completions.New` API.
- It is being deprecated for complex structured tasks.
- It lacks the strict schema enforcement guarantees of the Responses API.
- It often leads to "hallucinated" structures that fail Go's strict unmarshaling.

---

## Quick Reference: Canonical Patterns

Four patterns that **every agent in this codebase must follow**. These are the non-negotiable building blocks.

| Pattern | Rule |
|---------|------|
| **Strict JSON schema** | Always set `Strict: openai.Bool(true)`. Use `GenerateSchema[T]()` for **simple flat structs** (no pointer fields, no omitempty). For **union/variant types** (e.g. `AgentResponse` with `*Proposal` or `*ClarificationRequest`), you **must** hand-build a `map[string]any` — the reflector cannot produce a valid strict schema for these. |
| **Tool union construction** | Use `responses.ToolUnionParam{{OfFunction: &responses.FunctionToolParam{...}}}`. Never use the removed `openai.F()` or the old `ToolFunctionParam` struct. |
| **Multi-turn context** | Pass `PreviousResponseID: openai.String(resp.ID)` to chain turns. Never build raw message history arrays manually. |
| **API error inspection** | Wrap errors with `errors.As(err, &apierr)` on `*openai.Error` to read `StatusCode` and `DumpResponse()`. |

---

## 2. Minimum SDK Version
This integration targets **`github.com/openai/openai-go v1.12.0`** (stable — graduated from the alpha/beta era).
- The `go.mod` module path stays as `github.com/openai/openai-go` (no `/v3` suffix in application imports — standard Go v1 module semantics).
- Upgrading the SDK requires re-validating struct shapes and streaming events; consult the `MIGRATION.md` in the reference copy for breaking changes from earlier alpha versions.
- **Reference Code**: A copy of the OpenAI Go SDK is available in the `examples/openai-go-sdk-reference` folder. This copy is provided strictly for **reference purposes only** and should not be used as an active dependency.

## 3. Implementation Pattern
Refer to `internal/ai/agent.go` for the canonical implementation.

**Key Pattern:**
```go
params := responses.ResponseNewParams{
    Model: openai.ChatModelGPT4o,
    Input: responses.ResponseNewParamsInputUnion{
        OfString: openai.String(prompt),
    },
    Text: responses.ResponseTextConfigParam{
        Format: responses.ResponseFormatTextConfigUnionParam{
            OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
                Type:   constant.JSONSchema("json_schema"),
                Name:   "journal_entry_proposal",
                Strict: openai.Bool(true), // CRITICAL: Enforces schema strictly
                Schema: schemaMap,
                // ...
            },
        },
    },
}
```

## 4. Integration Challenges & Solutions

### Challenge 1: Infinite Loops / Retries
- **Issue**: The Beta SDK would sometimes enter infinite retry loops when the API response didn't perfectly match the expected Go types.
- **Solution**: We implemented **Strict Structured Outputs** (`Strict: openai.Bool(true)`) and ensured the JSON schema provided to the API exactly matched the Go `Proposal` struct tags.

### Challenge 2: Type Safety & Pointers
- **Issue**: The SDK requires properly boxed optional types for fields to omit zero values safely.
- **Solution**: We standardize on the `github.com/openai/openai-go` package helpers (`openai.String()`, `openai.Int()`, `openai.Bool()`). These are cleaner and safer than manipulating raw pointers or using mixed parameters. Do not mix with `param.NewOpt()`.

### Challenge 3: Normalization of Non-Standard Outputs
- **Issue**: Despite strict schemas, models may occasionally output empty strings `""` or string literals `"null"` for zero values, which causes parsing errors in strict numeric types.
- **Solution**: We implemented a `Normalize()` method on the `Proposal` struct that sanitizes these inputs (converting them to `"0.00"`) *before* strict validation logic runs. This significantly improves robustness.

## 5. Future Roadmap: MCP & Tool Use
We plan to significantly expand the agent's capabilities using advanced features of the SDK.

### Model Context Protocol (MCP)
We will integrate MCP to allow the AI Agent to query external data sources dynamically.
- **Goal**: Instead of stuffing the entire Chart of Accounts into the prompt (which is limited), the Agent will use MCP tools to "search" for relevant accounts.

### Tool Use (Function Calling)
We will implement the `Tools` parameter in the Responses API.
- **Current State**: The Agent is passive (Input -> Output).
- **Future State**: The Agent will be active.
    - *Example*: "Check the balance of the Bank account before proposing this expense."

## 6. Advanced Architecture Considerations
As the application scales, the following patterns must be adopted to ensure reliability, compliance, and performance.

### 6.1 Retrieval Augmented Generation (RAG)
**Problem**: The Chart of Accounts or Customer/Vendor list may grow to thousands of items, exceeding the context window or causing "lost in the middle" phenomena.
**Solution**: Do NOT iterate over all accounts in the prompt.
- **Pattern**: Use a RAG pipeline (via MCP or internal search).
    1.  User Input: "Paid invoice #9923 for Acme Corp"
    2.  Retrieval: Search DB for "Acme Corp" (Vendor) and open invoices.
    3.  Augmentation: Inject *only* the relevant Vendor Account and Invoice details into the prompt context.
    4.  Generation: AI proposes the entry using the retrieved specific context.

### 6.2 Evaluation Framework (Evals)
**Problem**: How do we know a prompt change didn't break the accounting logic?
**Solution**: Treat the Agent as software that requires regression testing.
1.  **Golden Datasets**: Maintain a JSONL file of `{ "input": "...", "expected_lines": [...] }`.
2.  **Automated Runs**: Before every deployment, run the Agent against 100+ examples.
3.  **Metrics**: Track "Exact Match" rate on account codes and amounts. Assert that `Confidence > 0.9` for standard cases.

### 6.3 Data Privacy & PII Scrubbing
**Problem**: Sending sensitive customer names or descriptions to a public LLM.
**Solution**: Implement a PII Sanitation Middleware.
- **Before Request**: Replace "John Doe (SSN: 123-45...)" with "<PERSON_1> (<ID_REDACTED>)".
- **After Response**: Re-hydrate the proposal if necessary, or simply store the sanitized narration in the ledger.
- **Strict Rule**: Never send PII unless necessary for the specific accounting decision.

### 6.4 Cost Management
**Problem**: High-volume transaction processing can become expensive.
**Solution**:
- **Token Tracking**: Log usage metadata (`usage.total_tokens`) for every call.
- **Caching**: Cache identical queries (e.g., recurring monthly subscriptions) to avoid hitting the LLM.
- **Model Routing**: Use lighter models (e.g., `gpt-4o-mini`) for simple "categorization" tasks and save `gpt-4o` for complex reasoning.

## 7. SDK Best Practices
Based on the official `openai-go` repository, the following patterns are recommended for production systems:

### 7.1 Schema Generation

OpenAI strict mode enforces two hard rules on every schema:
1. **Every property must be in the `required` array** — no exceptions.
2. **Nullable (pointer) fields must use `anyOf: [{schema}, {type: "null"}]`** — not omission from `required`.

There are two approaches depending on the response type:

#### Simple flat structs — Use `GenerateSchema[T]()`
For types where **all fields are non-pointer and have no `omitempty`**, the reflector works:

```go
func GenerateSchema[T any]() map[string]any {
    reflector := jsonschema.Reflector{
        AllowAdditionalProperties: false,
        DoNotReference:            true,
    }
    var v T
    schema := reflector.Reflect(v)
    data, _ := json.Marshal(schema)
    var result map[string]any
    json.Unmarshal(data, &result)
    delete(result, "$schema") // OpenAI strict mode rejects the $schema field
    return result
}
```

> [!CAUTION]
> **Do NOT use `omitempty` in JSON struct tags for Structured Outputs.** The reflector interprets `omitempty` as optional, excluding the field from `required`. This causes OpenAI to reject the schema with HTTP 400: `'required' must include every key in properties`.

#### Union / variant response types — Hand-build the schema

When your response type has **pointer fields** (`*Proposal`, `*ClarificationRequest`) that can be null, the reflector **cannot produce a valid strict schema**. It will either omit those fields from `required`, or not emit the `anyOf: [{schema}, {type: "null"}]` pattern that OpenAI requires.

**Proven failure case:** `AgentResponse` with `*Proposal` and `*ClarificationRequest` — reflector generated a schema that was immediately rejected by OpenAI with:
> `'required' is required to be supplied and to be an array including every key in properties. Missing 'clarification'.`

**Solution:** Build the schema as a `map[string]any` directly:

```go
// All three properties are in required.
// Nullable pointer fields use anyOf with {type: "null"}.
func generateSchema() map[string]any {
    return map[string]any{
        "type":                 "object",
        "additionalProperties": false,
        "required":             []string{"is_clarification_request", "clarification", "proposal"},
        "properties": map[string]any{
            "is_clarification_request": map[string]any{
                "type": "boolean",
            },
            "clarification": map[string]any{
                "anyOf": []any{
                    map[string]any{
                        "type":                 "object",
                        "additionalProperties": false,
                        "required":             []string{"message"},
                        "properties": map[string]any{
                            "message": map[string]any{"type": "string"},
                        },
                    },
                    map[string]any{"type": "null"},
                },
            },
            "proposal": map[string]any{
                "anyOf": []any{
                    proposalSchema(), // inline the full Proposal schema
                    map[string]any{"type": "null"},
                },
            },
        },
    }
}
```

Key rules for hand-built schemas:
- `additionalProperties: false` on **every** nested object, not just the root
- `required` must list **every** key in `properties` at every level
- Pointer/optional fields: use `anyOf: [{full schema}, {"type": "null"}]`

> [!NOTE]
> The SDK also provides a convenience constructor `responses.ResponseFormatTextConfigParamOfJSONSchema(name, schema)` that can further simplify the `Text.Format` field. However, it does **not** expose `Strict: openai.Bool(true)` — so for this project the verbose struct literal must be used to ensure strict enforcement.

### 7.2 Streaming Responses
For lower latency processing, use the `Responses.NewStreaming` API. Check `event.JSON.Text.Valid()` to know when the full text payload has materialised.
*Note: The field names shown (`event.Delta`, `event.JSON.Text.Valid()`, `event.Text`) are confirmed against the stable SDK reference copy. Re-verify them after any SDK upgrade by checking `examples/openai-go-sdk-reference/examples/responses-streaming/main.go`.*

```go
stream := client.Responses.NewStreaming(ctx, params)

var completeText string

for stream.Next() {
    event := stream.Current()
    
    // Process incremental stream delta updates
    print(event.Delta)
    
    // Check if the output payload has fully materialized
    if event.JSON.Text.Valid() {
        completeText = event.Text
        break
    }
}

if err := stream.Err(); err != nil {
    // Handle error (e.g. context timeout, API abort)
}
```

### 7.3 Reliability & Configuration
Configure the client with timeouts and retry limits to handle transient network issues gracefully.
```go
client := openai.NewClient(
    option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    option.WithMaxRetries(3), // Default is usually 2
)
// Always use context with timeout for requests
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

---

## 8. Minimal Complete Working Example

The following standalone Go program demonstrates initializing the client properly, using the `GenerateSchema[T]()` helper, enforcing strict JSON parsing via the `Responses API`, and decoding the result.

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
)

type SimpleResponse struct {
	Greeting    string `json:"greeting" jsonschema_description:"A friendly greeting"`
	LuckyNumber int    `json:"lucky_number" jsonschema_description:"A random lucky number"`
}

// GenerateSchema is defined in Section 7.1. Include it (or import it) in your package.
func GenerateSchema[T any]() map[string]any {
	reflector := jsonschema.Reflector{AllowAdditionalProperties: false, DoNotReference: true}
	var v T
	schema := reflector.Reflect(v)
	data, _ := json.Marshal(schema)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result
}

func main() {
	client := openai.NewClient(
		option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		option.WithMaxRetries(3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// One-liner schema generation — no manual marshal/unmarshal needed here.
	schemaMap := GenerateSchema[SimpleResponse]()

	params := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4o,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Say hello and give me a lucky number"),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Type:   constant.JSONSchema("json_schema"),
					Name:   "simple_response",
					Strict: openai.Bool(true),
					Schema: schemaMap,
				},
			},
		},
	}

	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		log.Fatalf("OpenAI API error: %v", err)
	}

	content := resp.OutputText()
	if content == "" {
		log.Fatalf("Empty response content")
	}

	var parsed SimpleResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		log.Fatalf("JSON parse error: %v\nRaw Content: %s", err, content)
	}

	fmt.Printf("Greeting: %s\n", parsed.Greeting)
	fmt.Printf("Lucky Number: %d\n", parsed.LuckyNumber)

	// Log usage metadata for cost tracking (see §6.4)
	if usage := resp.Usage; usage != nil {
		log.Printf("Usage: %d total tokens", usage.TotalTokens)
	}
}
```

---

## 9. Tool Use (Function Calling) Full Loop Example

When a tool logic evaluates to a result, it must be relayed back to the model correctly to yield a final output. Instead of manually pushing messages to an array, the Responses API leverages the `PreviousResponseID` conversation state for simple multi-turn context carrying.

**Safety Measure**: A defensive loop counter (`maxLoops`) is implemented to prevent infinite API exhaustion if the model gets stuck calling tools recursively.

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
)

// mockGoFunction simulates an external service
func mockGoFunction(location string) string {
	return fmt.Sprintf("The current weather in %s is 72°F.", location)
}

func runToolLoop(ctx context.Context, client *openai.Client) error {
	params := responses.ResponseNewParams{
		Model: openai.ChatModelGPT4o,
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("Fetch the current weather in Tokyo, and format it nicely."),
		},
		// Tool union: set OfFunction pointer — SDK infers type from the non-nil field.
		// Do NOT use openai.F() — it was removed in the stable SDK.
		Tools: []responses.ToolUnionParam{{
			OfFunction: &responses.FunctionToolParam{
				Name:        "get_weather",
				Description: openai.String("Get the current weather for a location."),
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"location"},
				},
			},
		}},
	}

	maxLoops := 5
	for i := 0; i < maxLoops; i++ {
		resp, err := client.Responses.New(ctx, params)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		if len(resp.Output) > 0 {
			// Iterate to find function calls if multiple tool calls run in parallel
			toolCalled := false
			for _, item := range resp.Output {
				if funcCall := item.AsFunctionCall(); funcCall != nil {
					toolCalled = true
					
					// 1. Extract arguments safely
					var args map[string]any
					if err := json.Unmarshal([]byte(funcCall.Arguments), &args); err != nil {
						log.Printf("Arguments parse failed: %v", err)
						continue
					}
					
					loc, _ := args["location"].(string)
					
					// 2. Execute Mock Go Function
					result := mockGoFunction(loc)
					
					// 3. Chain the next request context
					// The Responses API supports implicit history via PreviousResponseID
					params = responses.ResponseNewParams{
					    Model: openai.ChatModelGPT4o,
					    PreviousResponseID: openai.String(resp.ID),
					    Input: responses.ResponseNewParamsInputUnion{
					        // Passing logic result back to Responses conversational state
					        OfString: openai.String(fmt.Sprintf("Tool Result: %s. Combine this into your final answer.", result)),
					    },
					    // Always pass 'Tools' down again if loop expects subsequent calls
					    Tools: params.Tools, 
					}
					break // Break to trigger next client.Responses.New cycle
				}
			}

			// If no tool was requested, we assume we have our final OutputText!
			if !toolCalled {
				fmt.Println("Final Answer:")
				fmt.Println(resp.OutputText())
				return nil
			}
		} else {
             fmt.Println("Final Answer received empty loop termination.")
             return nil
        }
	}
	return fmt.Errorf("exceeded max tool loop iterations")
}
```

---

## 10. Multi-Turn Conversation Pattern

To accumulate prior conversation state properly without hallucinating nested SDK union types (which evolve and are highly complex to build manually), you should use the primary and safest API method: **`PreviousResponseID`**. 

```go
// Safely extending a multi-turn conversation relies on passing the previous ID.
// The SDK handles appending history internally via OpenAI's threaded state.
params := responses.ResponseNewParams{
    Model: openai.ChatModelGPT4o,
    PreviousResponseID: openai.String(previousResp.ID),
    Input: responses.ResponseNewParamsInputUnion{
        OfString: openai.String("Follow up: what was the total amount?"),
    },
}
```
*Note: Manually composing history arrays (`OfInputItemList`) via `ResponseInputItemMessageContentUnionParam` is highly verbose and prone to breaking changes. Use `PreviousResponseID` instead.*

---

## 11. Response Object Inspection

Because the `Responses API` structures complex workflows, you must defensively inspect fields. You will regularly need to extract text components, structured outputs, or nested tool logic safely.

```go
resp, err := client.Responses.New(ctx, params)
if err != nil {
    // Top-level API Transport / Go Error
    log.Fatal(err)
}

// Inspect Text Output
text := resp.OutputText()
if text != "" {
    // Process text
}

// Defensive Tool Inspection
for _, item := range resp.Output {
    if funcCall := item.AsFunctionCall(); funcCall != nil {
        fmt.Printf("Model invoked function: %s\n", funcCall.Name)
    }
}

// Log tokens and Usage Metadata
if usage := resp.Usage; usage != nil {
    fmt.Printf("Tokens Prompt: %v\n", usage.PromptTokens)
    fmt.Printf("Tokens Completion: %v\n", usage.CompletionTokens)
    fmt.Printf("Tokens Total: %v\n", usage.TotalTokens)
}
```

---

## 12. Error Handling & Failure Modes

When working directly with Agents reading accounting events, fail safely.

* **Context Timeout Handling**: Because strict schema or complex Tool logic can cause the model execution to lag, ensure your context `context.WithTimeout(context.Background(), 60*time.Second)` is correctly checked for `context.DeadlineExceeded`.
* **Schema Validation Failures**: If `json.Unmarshal` fails parsing, do not retry blindly. The response might have been truncated. Log the raw unstructured string: `log.Printf("Raw Content: %s", resp.OutputText())`.
* **Infinite Tool Loops**: Cap your loop (e.g., `maxLoops := 5`). Returning to the user gracefully is safer than creating an infinite API spin logic.
* **Safe Retries**: Opt into the SDK's retry mechanisms using `option.WithMaxRetries(3)`. For errors returned by `client`, use Go's robust `errors.Is` to catch network specific issues over standard parsing glitches.
* **Malformed Structured Outputs**: Use `val.Normalize()` pattern mentioned earlier to sanitize `\n` or blank whitespace fields that accidentally got injected upstream.
* **API-Level Error Inspection**: Distinguish transport errors from API errors using the typed `*openai.Error`:

```go
if err != nil {
    var apierr *openai.Error
    if errors.As(err, &apierr) {
        // apierr.StatusCode holds the HTTP status (e.g. 400 schema rejection, 429 rate limit)
        log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
    }
    return err
}
```

---

## 13. AI Agent Implementation Constraints

These rules are strict, assertive, and must be followed relentlessly.

* **Only use `client.Responses.New` or `NewStreaming`**: Agents must strictly abide by Response boundaries.
* **Never use Chat Completions**: Bypassing the Beta `Responses` struct creates fragmented behavior across the repo and forfeits schema guarantees.
* **Schema generation strategy**: Use `GenerateSchema[T]()` for simple flat structs. For union/variant types (pointer fields, nullable branches), hand-build a `map[string]any` — the reflector cannot produce valid strict schemas for those. See §7.1.
* **Always use `openai.String()` / `openai.Bool()` for optional fields**: SDK Pointers will panic. Protect boundaries with designated `openai` helpers. Never mix with `param.NewOpt()`.
* **Always use Strict JSON schema**: When structured output is required, `Strict: openai.Bool(true)` must be enforced so the model complies aggressively with types.
* **Remove `omitempty` from Structs used with the reflector**: Structured Outputs schemas explicitly enforce static field mappings. `omitempty` breaks strict checks when using the reflector approach.
* **Never assume response fields exist without nil checks**: Responses API outputs arrays or interface structures; assert presence (`resp.Usage != nil`).
* **Always inspect tool calls before assuming final output**: Iterate `resp.Output` to avoid leaking ToolCall payloads down into internal application flow.

---

## 14. Model Selection Guidance

Select models effectively by leveraging the SDK Constants mapped globally to optimize performance, cost, and complexity limits.

**How to select Models:**
```go
// Always use standard openai package aliases
Model: openai.ChatModelGPT4o
```

**When to switch and substitute Light vs Heavy models:**
* Use **`gpt-4o`** for complex multi-turn logic involving Tools or Strict Output structures.
* Use **`gpt-4o-mini`** for high volume classification, single-turn lightweight validation payloads without tools, or simple data formatting.

**Avoid hardcoding literals**: Do not inline `"gpt-4o"`. Always use the aliases defined centrally like `openai.ChatModelGPT4o` ensuring versions bump alongside the SDK cleanly and safely.
