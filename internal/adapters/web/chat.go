package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"accounting-agent/internal/app"
	"accounting-agent/internal/core"

	"github.com/google/uuid"
)

// ── Pending action store ──────────────────────────────────────────────────────

// pendingKind identifies what type of action awaits confirmation.
type pendingKind string

const (
	pendingKindJournalEntry pendingKind = "journal_entry"
	pendingKindWriteTool    pendingKind = "write_tool"
)

// pendingAction is stored server-side until the user confirms or cancels.
type pendingAction struct {
	Kind        pendingKind
	Proposal    *core.Proposal // populated for pendingKindJournalEntry
	ToolName    string         // populated for pendingKindWriteTool
	ToolArgs    map[string]any // populated for pendingKindWriteTool
	CompanyCode string         // needed by ExecuteWriteTool
	CreatedAt   time.Time
}

const pendingTTL = 15 * time.Minute

// pendingStore is a thread-safe in-memory store with TTL expiry.
type pendingStore struct {
	mu      sync.Mutex
	actions map[string]pendingAction
}

func newPendingStore() *pendingStore {
	return &pendingStore{actions: make(map[string]pendingAction)}
}

func (s *pendingStore) put(token string, a pendingAction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions[token] = a
}

func (s *pendingStore) get(token string) (pendingAction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.actions[token]
	if !ok {
		return pendingAction{}, false
	}
	if time.Since(a.CreatedAt) > pendingTTL {
		delete(s.actions, token)
		return pendingAction{}, false
	}
	return a, true
}

func (s *pendingStore) delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.actions, token)
}

// startPurge starts a background goroutine that evicts expired entries every 5 minutes.
func (s *pendingStore) startPurge(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				for token, action := range s.actions {
					if time.Since(action.CreatedAt) > pendingTTL {
						delete(s.actions, token)
					}
				}
				s.mu.Unlock()
			}
		}
	}()
}

// ── SSE helpers ───────────────────────────────────────────────────────────────

// sendSSE writes one SSE event and flushes. data is JSON-marshalled.
func sendSSE(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	f.Flush()
}

// ── Request / response types ──────────────────────────────────────────────────

type chatMessageRequest struct {
	Text          string   `json:"text"`
	CompanyCode   string   `json:"company_code"`
	AttachmentIDs []string `json:"attachment_ids"` // UUIDs returned by /chat/upload
}

type chatConfirmRequest struct {
	Token  string `json:"token"`
	Action string `json:"action"` // "confirm" or "cancel"
}

// ── chatMessage — POST /api/chat/message and POST /chat ───────────────────────

// chatMessage accepts a user message and streams the AI response via SSE.
//
// SSE event types:
//
//	status       {"status":"thinking"}
//	answer       {"text":"..."}
//	clarification{"question":"...","context":"..."}
//	proposal     {"token":"uuid","proposal":{...}}   (journal entry awaiting confirm)
//	action_card  {"token":"uuid","tool":"...","args":{...}} (write tool awaiting confirm)
//	error        {"message":"...","code":"..."}
//	done         {}
func (h *Handler) chatMessage(w http.ResponseWriter, r *http.Request) {
	var req chatMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Text == "" || req.CompanyCode == "" {
		writeError(w, r, "text and company_code are required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if !h.requireCompanyAccess(w, r, req.CompanyCode) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, "streaming not supported", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present

	sendSSE(w, flusher, "status", map[string]any{"status": "thinking"})

	// Load any attached images from the upload directory.
	attachments := h.loadAttachments(req.AttachmentIDs)

	result, err := h.svc.InterpretDomainAction(r.Context(), req.Text, req.CompanyCode, attachments...)
	if err != nil {
		sendSSE(w, flusher, "error", map[string]any{"message": err.Error(), "code": "AI_ERROR"})
		sendSSE(w, flusher, "done", map[string]any{})
		return
	}

	switch result.Kind {

	case app.DomainActionKindAnswer:
		sendSSE(w, flusher, "answer", map[string]any{"text": result.Answer})

	case app.DomainActionKindClarification:
		sendSSE(w, flusher, "clarification", map[string]any{
			"question": result.Question,
			"context":  result.Context,
		})

	case app.DomainActionKindProposed:
		token := uuid.NewString()
		h.pending.put(token, pendingAction{
			Kind:        pendingKindWriteTool,
			ToolName:    result.ToolName,
			ToolArgs:    result.ToolArgs,
			CompanyCode: req.CompanyCode,
			CreatedAt:   time.Now(),
		})
		sendSSE(w, flusher, "action_card", map[string]any{
			"token": token,
			"tool":  result.ToolName,
			"args":  result.ToolArgs,
		})

	case app.DomainActionKindJournalEntry:
		// Route the financial event to InterpretEvent for a structured proposal.
		aiResult, err := h.svc.InterpretEvent(r.Context(), result.EventDescription, req.CompanyCode)
		if err != nil {
			sendSSE(w, flusher, "error", map[string]any{
				"message": err.Error(),
				"code":    "AI_ERROR",
			})
			break
		}
		if aiResult.IsClarification {
			sendSSE(w, flusher, "clarification", map[string]any{
				"question": aiResult.ClarificationMessage,
				"context":  "",
			})
			break
		}
		token := uuid.NewString()
		h.pending.put(token, pendingAction{
			Kind:        pendingKindJournalEntry,
			Proposal:    aiResult.Proposal,
			CompanyCode: req.CompanyCode,
			CreatedAt:   time.Now(),
		})
		sendSSE(w, flusher, "proposal", map[string]any{
			"token":    token,
			"proposal": aiResult.Proposal,
		})
	}

	sendSSE(w, flusher, "done", map[string]any{})
}

// loadAttachments reads uploaded image files by their UUIDs from the upload directory.
// Files that no longer exist (cleaned up) are silently skipped.
func (h *Handler) loadAttachments(ids []string) []app.Attachment {
	if len(ids) == 0 {
		return nil
	}
	var out []app.Attachment
	for _, id := range ids {
		path := h.uploadDir + "/" + id
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file expired or not found
		}
		// Re-detect MIME from the stored data.
		mime := http.DetectContentType(data)
		out = append(out, app.Attachment{MimeType: mime, Data: data})
	}
	return out
}

// ── chatConfirm — POST /api/chat/confirm and POST /chat/confirm ───────────────

// chatConfirm executes or cancels a pending action identified by its token.
func (h *Handler) chatConfirm(w http.ResponseWriter, r *http.Request) {
	var req chatConfirmRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Token == "" {
		writeError(w, r, "token is required", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if req.Action != "confirm" && req.Action != "cancel" {
		writeError(w, r, "action must be 'confirm' or 'cancel'", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	action, ok := h.pending.get(req.Token)
	if !ok {
		writeError(w, r, "token not found or expired", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Verify the confirming user still belongs to the company the action was created for.
	if !h.requireCompanyAccess(w, r, action.CompanyCode) {
		return
	}
	h.pending.delete(req.Token)

	if req.Action == "cancel" {
		writeJSON(w, map[string]any{"ok": true, "message": "Cancelled."})
		return
	}

	switch action.Kind {
	case pendingKindJournalEntry:
		if err := h.svc.CommitProposal(r.Context(), *action.Proposal); err != nil {
			writeError(w, r, "commit failed: "+err.Error(), "COMMIT_ERROR", http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "message": "Journal entry posted."})

	case pendingKindWriteTool:
		result, err := h.svc.ExecuteWriteTool(r.Context(), action.CompanyCode, action.ToolName, action.ToolArgs)
		if err != nil {
			writeError(w, r, err.Error(), "TOOL_ERROR", http.StatusUnprocessableEntity)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "result": json.RawMessage(result)})
	}
}
