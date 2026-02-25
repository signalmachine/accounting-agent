---
name: openai-integration
description: Strict guidelines for OpenAI Go SDK Responses API usage in this project. Use when modifying the AI agent, adding structured outputs, implementing tool calls, or debugging OpenAI API issues.
---

# OpenAI Go SDK Integration Guide

## 1. Mandate: Use the Responses API

This application **strictly enforces** the use of the **Responses API** (`client.Responses.New`) from `github.com/openai/openai-go`.

**FORBIDDEN:** Do NOT use `client.Chat.Completions.New`. It lacks strict schema enforcement and leads to hallucinated structures.

## Quick Reference: Canonical Patterns

| Pattern | Rule |
|---------|------|
| **Strict JSON schema** | Always set `Strict: openai.Bool(true)`. Use `GenerateSchema[T]()` for simple flat structs (no pointer fields, no omitempty). For union/variant types (e.g. `AgentResponse` with `*Proposal` or `*ClarificationRequest`), hand-build a `map[string]any`. |
| **Tool union construction** | Use `responses.ToolUnionParam{{OfFunction: &responses.FunctionToolParam{...}}}`. Never use the removed `openai.F()`. |
| **Multi-turn context** | Pass `PreviousResponseID: openai.String(resp.ID)` to chain turns. Never build raw message history arrays manually. |
| **API error inspection** | Wrap errors with `errors.As(err, &apierr)` on `*openai.Error` to read `StatusCode` and `DumpResponse()`. |

## 2. Minimum SDK Version

Target: **`github.com/openai/openai-go v1.12.0`**
- No `/v3` suffix in imports (standard Go v1 module semantics).
- A reference copy of the SDK is in `examples/openai-go-sdk-reference/` — for reference only, not an active dependency.

## 3. Implementation Pattern

See `internal/ai/agent.go` for the canonical implementation.

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
                Strict: openai.Bool(true), // CRITICAL
                Schema: schemaMap,
            },
        },
    },
}
```

## 4. Schema Generation

OpenAI strict mode enforces two hard rules:
1. **Every property must be in the `required` array** — no exceptions.
2. **Nullable fields must use `anyOf: [{schema}, {type: "null"}]`** — not omission from `required`.

### Simple flat structs — Use `GenerateSchema[T]()`

For types where **all fields are non-pointer and have no `omitempty`**:

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
    delete(result, "$schema") // OpenAI strict mode rejects $schema
    return result
}
```

> **CAUTION:** Do NOT use `omitempty` in JSON struct tags. The reflector interprets `omitempty` as optional, which causes OpenAI to reject the schema with HTTP 400.

### Union / variant response types — Hand-build the schema

When your response type has **pointer fields** (`*Proposal`, `*ClarificationRequest`), the reflector **cannot produce a valid strict schema**. Build it as `map[string]any`:

```go
func generateSchema() map[string]any {
    return map[string]any{
        "type":                 "object",
        "additionalProperties": false,
        "required":             []string{"is_clarification_request", "clarification", "proposal"},
        "properties": map[string]any{
            "is_clarification_request": map[string]any{"type": "boolean"},
            "clarification": map[string]any{
                "anyOf": []any{
                    map[string]any{
                        "type": "object", "additionalProperties": false,
                        "required": []string{"message"},
                        "properties": map[string]any{"message": map[string]any{"type": "string"}},
                    },
                    map[string]any{"type": "null"},
                },
            },
            "proposal": map[string]any{
                "anyOf": []any{
                    proposalSchema(),
                    map[string]any{"type": "null"},
                },
            },
        },
    }
}
```

Key rules for hand-built schemas:
- `additionalProperties: false` on **every** nested object
- `required` must list **every** key in `properties` at every level
- Pointer/optional fields: use `anyOf: [{full schema}, {"type": "null"}]`

## 5. Integration Challenges & Solutions

### Infinite Loops / Retries
Use `Strict: openai.Bool(true)` and ensure the JSON schema exactly matches the Go struct tags.

### Type Safety
Use `openai.String()`, `openai.Bool()` helpers. Never mix with `param.NewOpt()`.

### Non-Standard Outputs
Use `Proposal.Normalize()` to sanitize empty strings and `"null"` literals before validation.

## 6. Multi-Turn Conversation

```go
params := responses.ResponseNewParams{
    Model: openai.ChatModelGPT4o,
    PreviousResponseID: openai.String(previousResp.ID),
    Input: responses.ResponseNewParamsInputUnion{
        OfString: openai.String("Follow up question"),
    },
}
```

Never manually compose history arrays — use `PreviousResponseID`.

## 7. Tool Use (Function Calling)

```go
Tools: []responses.ToolUnionParam{{
    OfFunction: &responses.FunctionToolParam{
        Name:        "get_weather",
        Description: openai.String("Get the current weather for a location."),
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "location": map[string]any{"type": "string"},
            },
            "required": []string{"location"},
        },
    },
}},
```

Always implement a `maxLoops` cap (e.g. `5`) to prevent infinite tool call cycles.

## 8. Reliability & Configuration

```go
client := openai.NewClient(
    option.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    option.WithMaxRetries(3),
)
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

## 9. Error Handling

```go
if err != nil {
    var apierr *openai.Error
    if errors.As(err, &apierr) {
        log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
    }
    return err
}
```

Key failure modes:
- **Schema rejection (400):** Check `omitempty` tags and `required` arrays.
- **Context timeout:** Use `context.WithTimeout`, check for `context.DeadlineExceeded`.
- **Infinite tool loops:** Cap with `maxLoops`.
- **Malformed outputs:** Use `Normalize()` before `Validate()`.

## 10. Agent Implementation Constraints

- Only use `client.Responses.New` or `NewStreaming`.
- Never use Chat Completions API.
- Always `Strict: openai.Bool(true)` for structured outputs.
- No `omitempty` in structs used with the reflector.
- Always nil-check before accessing response fields (`resp.Usage != nil`).
- Always iterate `resp.Output` to detect tool calls before assuming final text output.

## 11. Model Selection

```go
Model: openai.ChatModelGPT4o       // complex multi-turn, tools, strict outputs
Model: openai.ChatModelGPT4oMini   // high-volume classification, simple formatting
```

Always use SDK constants — never hardcode `"gpt-4o"` as a string literal.
