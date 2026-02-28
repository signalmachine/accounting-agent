package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"accounting-agent/web/templates/pages"

	"github.com/google/uuid"
)

const (
	maxUploadSize    = 10 << 20 // 10 MB
	maxUploadFiles   = 5
	uploadCleanupAge = 30 * time.Minute
)

// allowedMIMETypes is the whitelist for uploaded attachments.
var allowedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

// chatHome serves GET / — the full-screen AI chat home page.
func (h *Handler) chatHome(w http.ResponseWriter, r *http.Request) {
	claims := authFromContext(r.Context())
	user, err := h.svc.GetUser(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}
	company, err := h.svc.LoadDefaultCompany(r.Context())
	if err != nil {
		http.Error(w, "failed to load company", http.StatusInternalServerError)
		return
	}
	companyName := ""
	companyCode := ""
	if company != nil {
		companyName = company.Name
		companyCode = company.CompanyCode
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	pages.ChatHome(user.Username, companyName, companyCode).Render(r.Context(), w)
}

// chatUpload handles POST /chat/upload — saves an image and returns its attachment ID.
func (h *Handler) chatUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize*maxUploadFiles)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, r, "request too large or malformed", "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeError(w, r, "no file provided", "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	if len(files) > maxUploadFiles {
		writeError(w, r, fmt.Sprintf("too many files (max %d)", maxUploadFiles), "BAD_REQUEST", http.StatusBadRequest)
		return
	}

	type attachmentInfo struct {
		AttachmentID string `json:"attachment_id"`
		Filename     string `json:"filename"`
		FileType     string `json:"file_type"`
		SizeBytes    int64  `json:"size_bytes"`
	}

	var results []attachmentInfo

	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			writeError(w, r, "failed to open uploaded file", "INTERNAL_ERROR", http.StatusInternalServerError)
			return
		}

		// Read header bytes for MIME detection.
		header := make([]byte, 512)
		n, _ := f.Read(header)
		mimeType := http.DetectContentType(header[:n])
		// Normalize webp detection (DetectContentType may return "image/webp" or similar).
		if strings.HasPrefix(mimeType, "image/") {
			mimeType = strings.ToLower(strings.TrimSpace(mimeType))
		}

		if !allowedMIMETypes[mimeType] {
			f.Close()
			writeError(w, r, fmt.Sprintf("file type %q not allowed; accepted: jpeg, png, webp", mimeType),
				"UNSUPPORTED_TYPE", http.StatusUnsupportedMediaType)
			return
		}

		// Seek back and read full content.
		if seeker, ok := f.(io.ReadSeeker); ok {
			seeker.Seek(0, io.SeekStart)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			writeError(w, r, "failed to read uploaded file", "INTERNAL_ERROR", http.StatusInternalServerError)
			return
		}
		if int64(len(data)) > maxUploadSize {
			writeError(w, r, fmt.Sprintf("file exceeds maximum size of %d MB", maxUploadSize>>20),
				"FILE_TOO_LARGE", http.StatusRequestEntityTooLarge)
			return
		}

		// Save to upload directory with UUID filename.
		attachmentID := uuid.NewString()
		destPath := filepath.Join(h.uploadDir, attachmentID)
		if err := os.WriteFile(destPath, data, 0600); err != nil {
			writeError(w, r, "failed to save uploaded file", "INTERNAL_ERROR", http.StatusInternalServerError)
			return
		}

		results = append(results, attachmentInfo{
			AttachmentID: attachmentID,
			Filename:     fh.Filename,
			FileType:     mimeType,
			SizeBytes:    int64(len(data)),
		})
	}

	writeJSON(w, results)
}

// chatClear handles POST /chat/clear — stateless server; client clears its sessionStorage.
func (h *Handler) chatClear(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true})
}

// startUploadCleanup runs a background goroutine that deletes uploaded files older than
// uploadCleanupAge every 10 minutes.
func (h *Handler) startUploadCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			entries, err := os.ReadDir(h.uploadDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					continue
				}
				if time.Since(info.ModTime()) > uploadCleanupAge {
					os.Remove(filepath.Join(h.uploadDir, entry.Name()))
				}
			}
		}
	}()
}
