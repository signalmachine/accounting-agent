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

// notImplemented is a stub handler that returns HTTP 501.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, "not implemented", "NOT_IMPLEMENTED", http.StatusNotImplemented)
}
