package web

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	RequestID string `json:"request_id,omitempty"`
}

// writeError writes a structured JSON error response.
func writeError(w http.ResponseWriter, r *http.Request, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := errorResponse{
		Error:     message,
		Code:      code,
		RequestID: requestIDFromContext(r.Context()),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// writeJSON writes a JSON response with status 200.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// notImplemented is a stub handler that returns HTTP 501 JSON.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, "not implemented", "NOT_IMPLEMENTED", http.StatusNotImplemented)
}

// notImplementedPage is a stub handler for browser page routes that returns a plain 501 HTML response.
func notImplementedPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotImplemented)
	_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;padding:2rem">
<h2>Coming Soon</h2><p>This screen will be available in a future phase.</p>
<a href="/dashboard" style="color:#1e293b">← Back to Dashboard</a>
</body></html>`))
}
