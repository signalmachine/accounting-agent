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

## 2. Minimum Supported SDK Version
This integration targets `github.com/openai/openai-go v0.1.0-alpha.55` (or newer implementations of the Responses API). 
- Upgrading the SDK requires re-validating struct shapes and streaming events, as beta fields occasionally change names.
- **Reference Code**: A copy of the OpenAI Go SDK is available in the `examples` folder. This copy is provided strictly for **reference purposes only** and should not be used as an active dependency.

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
While manual JSON schema construction works for simple cases, `github.com/invopop/jsonschema` is recommended for complex types to ensure the Go struct and JSON schema stay in sync.

> [!WARNING]
> **CRITICAL: Avoid using `omitempty` in JSON struct tags for Structured Outputs.**
> The `jsonschema` library interprets `omitempty` as an "optional" field, excluding it from the schema's `required` array. This causes `openai-go` API rejections when `Strict: true` is enabled, or causes the model to silently skip generating those fields. Always define all expected fields structurally.

```go
import "github.com/invopop/jsonschema"

func GenerateSchema[T any]() interface{} {
    reflector := jsonschema.Reflector{
        AllowAdditionalProperties: false,
        DoNotReference:            true,
    }
    var v T
    return reflector.Reflect(v)
}
```

### 7.2 Streaming Responses
For lower latency processing, use the `Responses.NewStreaming` API. Carefully check the structs `Valid()` flags to verify chunk completion across events.
*Note: Streaming structs and internal fields (like `JSON.Text.Valid()`) frequently evolve in Beta versions. Always verify field names against your installed SDK version.*

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

The following standalone Go program demonstrates initializing the client properly, creating a schema with `github.com/invopop/jsonschema`, enforcing strict JSON parsing via the `Responses API`, and decoding the result.

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

func main() {
	client := openai.NewClient(
		option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
		option.WithMaxRetries(3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v SimpleResponse
	schemaStruct := reflector.Reflect(v)

	schemaJSON, err := json.Marshal(schemaStruct)
	if err != nil {
		log.Fatalf("Failed to marshal schema: %v", err)
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(schemaJSON, &schemaMap); err != nil {
		log.Fatalf("Failed to decode schema map: %v", err)
	}

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

	// Logging Usage
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
	"github.com/openai/openai-go/shared/constant"
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
		Tools: []responses.ToolUnionParam{
			responses.ToolFunctionParam{
				Type: constant.ToolTypeFunction("function"),
				Function: responses.FunctionDefinitionParam{
					Name:        openai.String("get_weather"),
					Description: openai.String("Get the current weather for a location."),
					Parameters: openai.F(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type": "string",
							},
						},
						"required": []string{"location"},
					}),
				},
			},
		},
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

---

## 13. AI Agent Implementation Constraints

These rules are strict, assertive, and must be followed relentlessly.

* **Only use `client.Responses.New` or `NewStreaming`**: Agents must strictly abide by Response boundaries.
* **Never use Chat Completions**: Bypassing the Beta `Responses` struct creates fragmented behavior across the repo and forfeits schema guarantees.
* **Never construct raw JSON manually**: Always generate JSON schemas dynamically via `github.com/invopop/jsonschema` (`Reflector{...}`).
* **Always use `openai.String()` / `openai.Bool()` for optional fields**: SDK Pointers will panic. Protect boundaries with designated `openai` helpers. Never mix with `param.NewOpt()`.
* **Always use Strict JSON schema**: When structured output is required, `Strict: openai.Bool(true)` must be enforced so the model complies aggressively with types.
* **Remove `omitempty` from Structs**: Structured Outputs schemas explicitly enforce static field mappings. `omitempty` breaks Strict checks.
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
