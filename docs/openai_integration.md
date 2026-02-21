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

## 2. Implementation Pattern
Refer to `internal/ai/agent.go` for the canonical implementation.

**Key Pattern:**
```go
params := responses.ResponseNewParams{
    Model: shared.ResponsesModel(shared.ChatModelGPT4o),
    Input: responses.ResponseNewParamsInputUnion{
        OfString: param.NewOpt(prompt),
    },
    Text: responses.ResponseTextConfigParam{
        Format: responses.ResponseFormatTextConfigUnionParam{
            OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
                Type:   constant.JSONSchema("json_schema"),
                Name:   "journal_entry_proposal",
                Strict: param.NewOpt(true), // CRITICAL: Enforces schema strictly
                Schema: schemaMap,
                // ...
            },
        },
    },
}
```

## 3. Integration Challenges & Solutions
During the initial integration of `openai-go v1.12.0+`, we encountered several challenges:

### Challenge 1: Infinite Loops / Retries
- **Issue**: The Beta SDK would sometimes enter infinite retry loops when the API response didn't perfectly match the expected Go types.
- **Solution**: We implemented **Strict Structured Outputs** (`Strict: param.NewOpt(true)`) and ensured the JSON schema provided to the API exactly matched the Go `Proposal` struct tags.

### Challenge 2: Type Safety & Pointers
- **Issue**: The SDK uses a lot of pointer types (`*string`, `*int`) for optional fields, which made the code verbose and prone to nil-pointer panics if not handled carefully.
- **Solution**: We adopted the `github.com/openai/openai-go/packages/param` helper package (`param.NewOpt`) to handle optional parameters cleanly.

### Challenge 3: Normalization of Non-Standard Outputs
- **Issue**: Despite strict schemas, models may occasionally output empty strings `""` or string literals `"null"` for zero values, which causes parsing errors in strict numeric types.
- **Solution**: We implemented a `Normalize()` method on the `Proposal` struct that sanitizes these inputs (converting them to `"0.00"`) *before* strict validation logic runs. This significantly improves robustness.

## 4. Future Roadmap: MCP & Tool Use
We plan to significantly expand the agent's capabilities using advanced features of the SDK.

### Model Context Protocol (MCP)
We will integrate MCP to allow the AI Agent to query external data sources dynamically.
- **Goal**: Instead of stuffing the entire Chart of Accounts into the prompt (which is limited), the Agent will use MCP tools to "search" for relevant accounts.

### Tool Use (Function Calling)
We will implement the `Tools` parameter in the Responses API.
- **Current State**: The Agent is passive (Input -> Output).
- **Future State**: The Agent will be active.
    - *Example*: "Check the balance of the Bank account before proposing this expense."

## 5. Advanced Architecture Considerations
As the application scales, the following patterns must be adopted to ensure reliability, compliance, and performance.

### 5.1 Retrieval Augmented Generation (RAG)
**Problem**: The Chart of Accounts or Customer/Vendor list may grow to thousands of items, exceeding the context window or causing "lost in the middle" phenomena.
**Solution**: Do NOT iterate over all accounts in the prompt.
- **Pattern**: Use a RAG pipeline (via MCP or internal search).
    1.  User Input: "Paid invoice #9923 for Acme Corp"
    2.  Retrieval: Search DB for "Acme Corp" (Vendor) and open invoices.
    3.  Augmentation: Inject *only* the relevant Vendor Account and Invoice details into the prompt context.
    4.  Generation: AI proposes the entry using the retrieved specific context.

### 5.2 Evaluation Framework (Evals)
**Problem**: How do we know a prompt change didn't break the accounting logic?
**Solution**: Treat the Agent as software that requires regression testing.
1.  **Golden Datasets**: Maintain a JSONL file of `{ "input": "...", "expected_lines": [...] }`.
2.  **Automated Runs**: Before every deployment, run the Agent against 100+ examples.
3.  **Metrics**: Track "Exact Match" rate on account codes and amounts. Assert that `Confidence > 0.9` for standard cases.

### 5.3 Data Privacy & PII Scrubbing
**Problem**: Sending sensitive customer names or descriptions to a public LLM.
**Solution**: Implement a PII Sanitation Middleware.
- **Before Request**: Replace "John Doe (SSN: 123-45...)" with "<PERSON_1> (<ID_REDACTED>)".
- **After Response**: Re-hydrate the proposal if necessary, or simply store the sanitized narration in the ledger.
- **Strict Rule**: Never send PII unless necessary for the specific accounting decision.

### 5.4 Cost Management
**Problem**: High-volume transaction processing can become expensive.
**Solution**:
- **Token Tracking**: Log usage metadata (`usage.total_tokens`) for every call.
- **Caching**: Cache identical queries (e.g., recurring monthly subscriptions) to avoid hitting the LLM.
- **Model Routing**: Use lighter models (e.g., `gpt-4o-mini`) for simple "categorization" tasks and save `gpt-4o` for complex reasoning.

## 6. SDK Best Practices
Based on the official `openai-go` repository, the following patterns are recommended for production systems:

### 6.1 Schema Generation
While manual JSON schema construction works for simple cases, `github.com/invopop/jsonschema` is recommended for complex types to ensure the Go struct and JSON schema stay in sync.
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

### 6.2 Streaming Responses
For lower latency processing, use the `Responses.NewStreaming` API.
```go
stream := client.Responses.NewStreaming(ctx, params)
for stream.Next() {
    event := stream.Current()
    // Process incremental updates
}
if err := stream.Err(); err != nil {
    // Handle error
}
```

### 6.3 Reliability & Configuration
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

