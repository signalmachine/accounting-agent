# OpenAI Integration Remediation Plan

> Derived from the audit of `internal/ai/agent.go` against `.agents/skills/openai_integration/SKILL.md`.

## Why Two Phases?

The 4 gaps fall into two distinct concern categories:

| Category | Gaps | Phase |
|----------|------|-------|
| **Correctness & Reliability** | Schema helper refactor, API error inspection, retry config | Phase 1 |
| **Observability & Cost Tracking** | Token usage logging | Phase 2 |

Separating them keeps each PR focused, reviewable, and independently testable.

---

## Phase 1 — Correctness & Reliability

**Goal**: Bring `agent.go` fully in line with the SKILL.md's mandatory structural rules.

### Fix 1.1 — Replace `generateSchema()` with canonical `GenerateSchema[T]()` helper

**File**: `internal/ai/agent.go`

**Problem**: The current `generateSchema()` function:
- Returns `interface{}` instead of `map[string]any`
- Forces a redundant `json.Marshal` → `json.Unmarshal` round-trip in the caller (lines 63–71)
- Deletes `"$schema"` inline in the caller — fragile and inconsistent

**Canonical pattern** (from SKILL.md §7.1):

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
    delete(result, "$schema") // OpenAI strict mode rejects the $schema meta-field
    return result
}
```

**Call site** becomes a one-liner:

```go
schemaMap := GenerateSchema[core.Proposal]()
```

Remove lines 63–74 from `InterpretEvent` entirely.

---

### Fix 1.2 — Add `*openai.Error` typed inspection on API errors

**File**: `internal/ai/agent.go`

**Problem**: All API errors are uniformly wrapped with `fmt.Errorf`, losing the HTTP status code. A 429 rate-limit error and a 400 schema rejection are indistinguishable in logs.

**Required pattern** (SKILL.md §12):

```go
resp, err := a.client.Responses.New(ctx, params)
if err != nil {
    var apierr *openai.Error
    if errors.As(err, &apierr) {
        log.Printf("OpenAI API error %d: %s", apierr.StatusCode, apierr.DumpResponse(true))
    }
    return nil, fmt.Errorf("openai responses error: %w", err)
}
```

Add `"errors"` and `"log"` to the import block if not already present.

---

### Fix 1.3 — Add `option.WithMaxRetries(3)` to client construction

**File**: `internal/ai/agent.go`

**Problem**: The client is constructed with only an API key. The SDK defaults to 2 retries; the SKILL.md mandates explicitly setting 3.

```go
// Before
client := openai.NewClient(option.WithAPIKey(apiKey))

// After
client := openai.NewClient(
    option.WithAPIKey(apiKey),
    option.WithMaxRetries(3),
)
```

---

### Phase 1 — Acceptance Criteria

- [ ] `GenerateSchema[T]()` helper exists in `agent.go` (or a new `internal/ai/schema.go`)
- [ ] `InterpretEvent` calls `GenerateSchema[core.Proposal]()` directly — no manual marshal/unmarshal
- [ ] Error block on `Responses.New` uses `errors.As(err, &apierr)` before wrapping
- [ ] `NewAgent` passes `option.WithMaxRetries(3)`
- [ ] Code compiles: `go build ./...`
- [ ] Existing unit tests pass: `go test ./...`

---

## Phase 2 — Observability & Cost Tracking

**Goal**: Surface per-call token usage for future cost analysis and model routing decisions.

### Fix 2.1 — Log `resp.Usage` after every API call

**File**: `internal/ai/agent.go`

**Required pattern** (SKILL.md §6.4):

```go
// After successful resp, err := a.client.Responses.New(...)
if usage := resp.Usage; usage != nil {
    log.Printf("OpenAI usage — prompt: %d, completion: %d, total: %d tokens",
        usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
}
```

> [!TIP]
> This is a non-breaking, additive change. The log output can be silenced in tests by redirecting `log.SetOutput(io.Discard)` in test setup if needed.

---

### Phase 2 — Acceptance Criteria

- [ ] Token usage (prompt / completion / total) is logged on every successful `Responses.New` call
- [ ] Log output is visible in the REPL and CLI `propose` command during manual testing
- [ ] No new dependencies introduced

---

## File Scope Summary

All changes are confined to a **single file**:

| File | Changes |
|------|---------|
| `internal/ai/agent.go` | All 4 fixes — schema helper, error inspection, retry config, usage logging |

Optionally, extract `GenerateSchema[T]()` to `internal/ai/schema.go` if the file grows large.

---

## Order of Execution

```
Phase 1: Fix 1.3 (retry) → Fix 1.1 (schema) → Fix 1.2 (error)
Phase 2: Fix 2.1 (usage logging)
```

Fixing retry config first (1.3) is trivial and de-risks the API call before touching the schema logic.
