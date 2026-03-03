# Voice Input & Multilingual Support

**Date:** 2026-03-03
**Status:** Future — not started

---

## Overview

The AI agent can be made to accept voice instructions, including instructions in languages other than English (e.g. Kannada), with translation to English for processing.

---

## Why This Is Feasible

The chat UI captures text in a `<textarea>` and POSTs it as `{ text: "..." }` to `/chat`. The backend passes that string to `InterpretDomainAction(ctx, userInput string, ...)` → GPT-4o. The AI layer is entirely text-in / text-out — it has no concept of how the string arrived.

Any mechanism that converts voice → text string can plug directly into the existing pipeline without touching the AI agent, domain services, or backend at all (for the simplest approach).

---

## Option 1 — Browser Web Speech API (zero backend changes)

The browser's native `SpeechRecognition` API can transcribe voice to text entirely in the browser. A microphone button is added next to the current paperclip button in `chat_home.templ`:

```js
const recognition = new webkitSpeechRecognition();
recognition.lang = 'kn-IN';      // Kannada
recognition.interimResults = true;
recognition.onresult = (e) => {
    this.input = e.results[0][0].transcript;
};
recognition.start();
```

- The transcription (in Kannada text) goes into the `<textarea>`
- The user can review/edit it before submitting
- No backend changes
- GPT-4o understands Kannada and can process accounting descriptions in it

**Limitations:** Chrome has good Kannada support; Firefox does not implement the API. Accuracy for domain-specific accounting terms (e.g. "ಖಾತೆ ಪಾವತಿ" — accounts payable) can be inconsistent.

---

## Option 2 — OpenAI Whisper API (accurate, language-controlled)

For production-quality Kannada transcription:

1. **Frontend**: Add a record button → captures audio as WebM/OGG Blob
2. **Backend**: New `POST /chat/transcribe` endpoint receives the audio file, calls the Whisper API:
   ```go
   // Task "translate" gives English output directly from Kannada speech
   // Task "transcribe" gives Kannada text (then GPT-4o handles it)
   client.Audio.Transcriptions.New(ctx, audio.TranscriptionNewParams{
       Model:    "whisper-1",
       Language: "kn",       // or omit for auto-detect
   })
   ```
3. Backend returns the transcribed (or translated) text to the frontend
4. Frontend populates the textarea — user reviews → submits normally

**This requires only:**
- A new HTTP handler in `internal/adapters/web/` (10–20 lines)
- A new method on the `Agent` struct (does not touch `InterpretEvent` or `InterpretDomainAction`)
- A new route in `cmd/server/main.go`

The existing AI agent, all domain services, and all integration tests remain completely untouched.

---

## Kannada → English Translation Strategy

| Strategy | How | Tradeoff |
|---|---|---|
| **Whisper `translate` task** | Whisper transcribes Kannada speech and outputs English text directly | Single API call, no GPT-4o prompt changes; works for speech only |
| **Send Kannada text to GPT-4o as-is** | GPT-4o understands Kannada natively (multilingual model) | Works for both typed and spoken input; accounting terms may occasionally be misinterpreted |
| **Two-step: translate then process** | First call GPT-4o with "Translate to English:", then pass result to normal pipeline | Most reliable but costs 2 API calls |

The simplest reliable path for voice: **Whisper with `task=translate`** — clean English text out, goes straight into the existing pipeline unchanged.

---

## Summary of Changes Required

| Feature | Frontend | Backend | AI Agent |
|---|---|---|---|
| Voice (English, browser STT) | +mic button, Web Speech API | None | None |
| Voice (Kannada, browser STT) | +mic button, `lang='kn-IN'` | None | None |
| Voice (Kannada, Whisper transcribe) | +record button, audio capture | New `/chat/transcribe` endpoint | None |
| Voice (Kannada → English, Whisper translate) | +record button, audio capture | New `/chat/transcribe` endpoint | None |
| Typed Kannada input | None | None | None (GPT-4o already multilingual) |

---

## Recommendation

**Quick win:** Add the Web Speech API mic button (frontend only, Kannada locale) — a few lines of JavaScript in `chat_home.templ`. Users on Chrome (desktop/Android) get Kannada voice input immediately, no backend work needed.

**Production quality:** Add the Whisper `/chat/transcribe` endpoint with `task=translate` — clean English output, works accurately for accounting vocabulary in Kannada, and the existing pipeline is completely unaffected.

---

## Architectural Note

The AI Agent Change Policy (CLAUDE.md) is not at risk. The input boundary is `userInput string` — anything that produces that string is a frontend/adapter concern. `InterpretEvent` and `InterpretDomainAction` do not need to change.
