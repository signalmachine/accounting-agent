# Token Streaming for AI Chat

## Is This Standard Practice?

Yes. The hybrid pattern described here is the industry standard:

- **OpenAI's own documentation** recommends streaming only the final generation, not tool call arguments (which must be buffered whole before execution).
- **LangChain / LangGraph** follow the same model: tool calls are synchronous, final answer is streamed.
- **Anthropic's Claude API** uses the same design — `tool_use` blocks arrive complete, `text` deltas stream token-by-token.
- **Vercel AI SDK**, **Haystack**, **LlamaIndex** — all implement this pattern.

The key insight: tool calls require complete, valid JSON arguments before execution. Streaming partial tool arguments is not useful. Streaming the human-readable final answer is where the perceived speed gain comes from.

---

## Current Behaviour

```
User message
    → POST /chat
        → InterpretDomainAction() — blocks until all tool iterations complete
            → (up to 5 OpenAI round-trips, each non-streaming)
        → single SSE "answer" event sent all at once
    → frontend renders full text in one paint
```

The user sees nothing for 2–6 seconds, then the full answer appears instantly.

---

## Target Behaviour (Phase 1 — Recommended Starting Point)

```
User message
    → POST /chat
        → InterpretDomainAction() — tool loop runs as today (non-streaming)
            → (up to 5 OpenAI round-trips, each non-streaming)
        → when result.Kind == DomainActionKindAnswer:
            → open a *streaming* OpenAI call for the final generation
            → emit SSE "token" events as each text delta arrives
    → frontend appends each token to the message bubble in real time
```

Tool resolution time is unchanged. The final answer appears word-by-word instead of all at once. This delivers ~80% of the perceived speed improvement with low implementation risk.

---

## What Needs to Change

### Backend — `internal/ai/agent.go`

1. Add a new streaming method to `AgentService`:

```go
// InterpretDomainActionStream runs the agentic tool loop (non-streaming),
// then streams the final answer token-by-token via the provided callback.
// The callback is invoked once per text delta. For non-answer results
// (action cards, proposals, clarifications) it behaves identically to
// InterpretDomainAction — the callback is never called.
InterpretDomainActionStream(
    ctx context.Context,
    userText string,
    companyCode string,
    onToken func(delta string),
    attachments ...app.Attachment,
) (app.DomainActionResult, error)
```

2. Inside the implementation:
   - Run the existing tool loop exactly as today (non-streaming).
   - When the loop produces a final text answer, instead of returning it directly, make a **second streaming call** to OpenAI using `client.Responses.NewStreaming(...)`.
   - For each `ResponseTextDeltaEvent` received, call `onToken(delta.Delta)`.
   - Return the complete assembled text as `result.Answer` so callers that don't use streaming still work.

3. Use the `openai-go` SDK streaming pattern:
```go
stream := client.Responses.NewStreaming(ctx, openai.ResponseNewParams{...})
for stream.Next() {
    event := stream.Current()
    if delta, ok := event.AsResponseTextDeltaEvent(); ok {
        onToken(delta.Delta)
    }
}
if err := stream.Err(); err != nil {
    return err
}
```

> Consult the `openai-integration` skill before writing any OpenAI SDK code.

### Backend — `internal/adapters/web/chat.go`

Replace the `DomainActionKindAnswer` branch in `chatMessage`:

```go
// Before (non-streaming):
case app.DomainActionKindAnswer:
    sendSSE(w, flusher, "answer", map[string]any{"text": result.Answer})

// After (streaming):
case app.DomainActionKindAnswer:
    // result.Answer is empty here; tokens arrive via onToken callback.
    // The handler calls InterpretDomainActionStream instead of InterpretDomainAction.
```

The handler calls `InterpretDomainActionStream` with an `onToken` callback that calls `sendSSE(w, flusher, "token", map[string]any{"delta": delta})`.

Change the SSE event name from `"answer"` to `"token"` for streaming deltas, and emit a final `"answer"` event (with empty text) or just rely on `"done"` to signal completion. Both approaches are valid — choose one and be consistent.

### Frontend — `chat_home.templ` and `app_layout.templ` (slide-over)

The frontend already reads SSE events in a loop. Add handling for the new `"token"` event:

```js
} else if (event === 'token') {
    // Streaming delta — append to the live message bubble.
    if (!aiMsg) {
        aiMsg = { role: 'ai', type: 'text', text: '' };
        this.messages.push(aiMsg);
    }
    aiMsg.text += (d.delta || '');
    this.saveHistory();
    this.$nextTick(() => this.scrollToBottom());
}
```

The existing `"answer"` event handler can be kept for backward compatibility or removed once the streaming path is stable.

---

## What Does NOT Change

- The tool loop in `InterpretDomainAction` — no modifications.
- `InterpretEvent` (journal entry path) — untouched, per the AI gradualism policy.
- Action cards, proposals, clarification events — all unchanged.
- The `pendingStore`, confirm/cancel flow — unchanged.
- All 70 integration tests — no test changes required.
- `ApplicationService` interface — add `InterpretDomainActionStream` alongside the existing method.

---

## Phase 2 — Full Streaming (Future, Higher Complexity)

This is the approach where every token from every OpenAI call in the tool loop is streamed, and tool execution status is shown in real time (e.g., "Searching accounts…", "Fetching stock levels…").

**Additional challenges:**

1. `InterpretDomainAction` must become a streaming iterator (channel-based or callback-based) rather than a blocking function returning a single result.
2. Tool call arguments arrive in chunks during streaming — you must buffer and parse them before execution.
3. The SSE event model needs a richer vocabulary: `tool_start`, `tool_result`, `token`, `done`.
4. The frontend needs to render intermediate tool states (progress indicators per tool call).

**Recommended prerequisite:** Phase 1 above must be stable in production before starting Phase 2.

---

## Implementation Order

| Step | File | Change |
|------|------|--------|
| 1 | `internal/ai/agent.go` | Add `InterpretDomainActionStream` method |
| 2 | `internal/app/service.go` | Add `InterpretDomainActionStream` to `ApplicationService` interface |
| 3 | `internal/app/app_service.go` | Implement delegation to AI layer |
| 4 | `internal/adapters/web/chat.go` | Use streaming method in `chatMessage` handler |
| 5 | `web/templates/pages/chat_home.templ` | Handle `token` SSE event |
| 6 | `web/templates/layouts/app_layout.templ` | Same token handler in slide-over |
| 7 | Manual test | Verify word-by-word rendering, action cards, proposals still work |

---

## Prerequisites

- Read the `openai-integration` skill before writing any SDK code (`/openai-integration`).
- Confirm `openai-go` SDK version supports `Responses.NewStreaming` — check `go.mod`.
- Domain tests must continue to pass (`go test ./internal/core -v`).
- The existing `InterpretEvent` path must remain completely untouched.

---

## Bug Fix — Markdown Not Rendering in Chat Bubbles

### Problem

The AI returns markdown-formatted responses (e.g. `### Assets`, `**Cash:**`). The chat bubble
displays raw markdown as plain text instead of rendering it as formatted HTML.

**Root cause:** `chat_home.templ` line 50 uses `x-html="msg.html || msg.text"`. The `x-html`
directive renders HTML correctly, but `msg.html` is never populated — the frontend only stores
`msg.text` (the raw markdown string). Alpine.js's `x-html` does not parse markdown, so the
raw symbols (`###`, `**`) appear verbatim.

The same issue exists in the slide-over chat in `app_layout.templ`, which uses `div.textContent`
(plain text assignment) — it also never renders markdown.

### Fix — Vendor `marked.js`

`marked.js` is a lightweight (~23 KB minified), zero-dependency markdown-to-HTML parser.
It is the standard choice for this pattern and consistent with the project's approach of
vendoring JS libraries (Alpine.js, HTMX, Chart.js are already vendored).

**Step 1 — Download and vendor**

```bash
curl -L https://cdn.jsdelivr.net/npm/marked/marked.min.js -o web/static/js/marked.min.js
```

Verify the file is served at `/static/js/marked.min.js`.

**Step 2 — Add script tag to `app_layout.templ`**

Add alongside the other vendored scripts in the `<head>`:

```html
<script src="/static/js/marked.min.js"></script>
```

This makes `marked` available globally on every page that uses `AppLayout`, including
`chat_home` and the slide-over.

**Step 3 — Populate `msg.html` in `chat_home.templ`**

In the `answer` SSE event handler inside `sendMessage()`, convert markdown before storing:

```js
// Before:
aiMsg = { role: 'ai', type: 'text', text: d.text || '' };

// After:
const raw = d.text || '';
aiMsg = { role: 'ai', type: 'text', text: raw, html: marked.parse(raw) };
```

For the streaming path (Phase 1), update `msg.html` incrementally as tokens arrive:

```js
} else if (event === 'token') {
    if (!aiMsg) {
        aiMsg = { role: 'ai', type: 'text', text: '' };
        this.messages.push(aiMsg);
    }
    aiMsg.text += (d.delta || '');
    aiMsg.html = marked.parse(aiMsg.text);   // re-parse on each token
    this.saveHistory();
    this.$nextTick(() => this.scrollToBottom());
}
```

Re-parsing on every token is acceptable at typical token speeds (~30–50 tokens/sec).
`marked.parse` is synchronous and very fast (<1 ms for typical response lengths).

**Step 4 — Fix `app_layout.templ` slide-over**

The slide-over uses DOM manipulation (`div.textContent`) instead of Alpine.js bindings.
Replace `textContent` assignment with `innerHTML` + `marked.parse()` for AI text bubbles:

```js
// In makeBubble(), the default AI text case:
// Before:
div.textContent = msg.text || '';

// After:
div.innerHTML = marked.parse(msg.text || '');
```

Apply the same to the streaming `answer` event handler in the slide-over's SSE loop:

```js
// Before:
if (live) live.textContent = aiMsg.text;

// After:
if (live) live.innerHTML = marked.parse(aiMsg.text);
```

**Step 5 — History replay**

When chat history is restored from `sessionStorage`, `msg.html` may be absent for older
entries. Ensure the `makeBubble` / Alpine template falls back gracefully:

- `chat_home.templ` already handles this: `x-html="msg.html || msg.text"` — raw markdown
  shows as text, which is acceptable for replayed history.
- Optionally, re-parse on load: `msg.html = msg.html || marked.parse(msg.text || '')`.

### What Does NOT Change

- No backend changes required — this is purely a frontend rendering fix.
- The `msg.text` field is always stored as the raw markdown string (source of truth for
  history serialisation and for the streaming token-append logic).
- Action cards and proposal cards are unaffected — they use structured data fields, not
  free-form markdown text.
- All 70 integration tests unaffected.

### Implementation Order

| Step | File | Change |
|------|------|--------|
| 1 | `web/static/js/marked.min.js` | Download and vendor |
| 2 | `web/templates/layouts/app_layout.templ` | Add `<script>` tag for marked.min.js |
| 3 | `web/templates/pages/chat_home.templ` | Populate `msg.html` via `marked.parse()` in `answer` handler |
| 4 | `web/templates/layouts/app_layout.templ` | Use `innerHTML + marked.parse()` in slide-over `makeBubble` and SSE handlers |
| 5 | Manual test | Send "show trial balance", verify headings and bold text render correctly |
