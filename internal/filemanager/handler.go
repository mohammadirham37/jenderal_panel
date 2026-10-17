package filemanager

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// maxUploadBytes caps one file-manager upload. Parts beyond
// uploadMemoryBytes spill to a temporary file, so large archives never sit
// in memory. A var so tests can lower it.
var maxUploadBytes int64 = 2 << 30

const uploadMemoryBytes int64 = 32 << 20

// Handler handles file manager HTTP requests.
type Handler struct {
	svc   *Service
	db    *sql.DB
	audit *audit.Service
}

// NewHandler creates a new file manager Handler.
func NewHandler(svc *Service, db *sql.DB, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, db: db, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating file operation.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "filemanager",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// getWebsite loads the website record and returns its document root base path.
func (h *Handler) getWebsite(r *http.Request) (basePath string, err error) {
	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		return "", model.NewValidationError("website ID is required")
	}

	var docRoot, webUser string
	err = h.db.QueryRowContext(r.Context(),
		`SELECT document_root, web_user FROM websites WHERE id = ?`, websiteID,
	).Scan(&docRoot, &webUser)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", model.ErrNotFound
		}
		return "", fmt.Errorf("query website: %w", err)
	}

	// Use the home directory of the web user as the base path so the file
	// manager allows access to the entire website home.
	basePath = "/home/" + webUser
	return basePath, nil
}

// Browse handles GET /api/websites/{websiteID}/files?path=...
func (h *Handler) Browse(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	subPath := r.URL.Query().Get("path")
	entries, err := h.svc.Browse(r.Context(), basePath, subPath)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, entries)
}

// ReadFile handles GET /api/websites/{websiteID}/files/read?path=...
func (h *Handler) ReadFile(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	content, err := h.svc.ReadFile(r.Context(), basePath, filePath)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{"content": content})
}

// WriteFile handles POST /api/websites/{websiteID}/files/write
func (h *Handler) WriteFile(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	if err := h.svc.WriteFile(r.Context(), basePath, req.Path, req.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "write_file", req.Path, "wrote file "+req.Path)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteFile handles DELETE /api/websites/{websiteID}/files?path=...
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	if err := h.svc.DeleteFile(r.Context(), basePath, filePath); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "delete_file", filePath, "deleted "+filePath)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Rename handles POST /api/websites/{websiteID}/files/rename
func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.OldPath == "" || req.NewPath == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "old_path and new_path are required")
		return
	}

	if err := h.svc.Rename(r.Context(), basePath, req.OldPath, req.NewPath); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "rename_file", req.OldPath, fmt.Sprintf("renamed %s to %s", req.OldPath, req.NewPath))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Copy handles POST /api/websites/{websiteID}/files/copy. The target is the
// full destination path; an existing target is rejected.
func (h *Handler) Copy(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" || req.Target == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path and target are required")
		return
	}

	if err := h.svc.Copy(r.Context(), basePath, req.Path, req.Target); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "copy_file", req.Path, fmt.Sprintf("copied %s to %s", req.Path, req.Target))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Move handles POST /api/websites/{websiteID}/files/move. The target is the
// full destination path; an existing target is rejected.
func (h *Handler) Move(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" || req.Target == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path and target are required")
		return
	}

	if err := h.svc.Move(r.Context(), basePath, req.Path, req.Target); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "move_file", req.Path, fmt.Sprintf("moved %s to %s", req.Path, req.Target))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateDir handles POST /api/websites/{websiteID}/files/mkdir
func (h *Handler) CreateDir(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	if err := h.svc.CreateDir(r.Context(), basePath, req.Path); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "create_dir", req.Path, "created directory "+req.Path)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Chmod handles POST /api/websites/{websiteID}/files/chmod
func (h *Handler) Chmod(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Recursive bool   `json:"recursive"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" || req.Mode == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path and mode are required")
		return
	}

	if err := h.svc.Chmod(r.Context(), basePath, req.Path, req.Mode, req.Recursive); err != nil {
		httputil.HandleError(w, err)
		return
	}

	detail := fmt.Sprintf("chmod %s %s", req.Mode, req.Path)
	if req.Recursive {
		detail += " (recursive)"
	}
	h.logAction(r, "chmod_file", req.Path, detail)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ZipRequest carries the JSON body for creating a zip archive.
type ZipRequest struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}

// Zip handles POST /api/websites/{websiteID}/files/zip. It archives a file
// or directory into a .zip archive; an empty target defaults to a sibling
// <basename>.zip.
func (h *Handler) Zip(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req ZipRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	created, err := h.svc.Zip(r.Context(), basePath, req.Path, req.Target)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "zip_path", req.Path, fmt.Sprintf("archived %s into %s", req.Path, created))
	httputil.JSON(w, http.StatusOK, map[string]string{"archive": created})
}

// Unzip handles POST /api/websites/{websiteID}/files/unzip. It extracts a
// .zip archive into dest (its own directory when empty), overwriting
// existing files.
func (h *Handler) Unzip(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	var req ZipRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if req.Path == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	dest, err := h.svc.Unzip(r.Context(), basePath, req.Path, req.Target)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "unzip_path", req.Path, fmt.Sprintf("extracted %s into %s", req.Path, dest))
	httputil.JSON(w, http.StatusOK, map[string]string{"dest": dest})
}

// Upload handles POST /api/websites/{websiteID}/files/upload
// Expects multipart form with "file" field and "path" field for the target directory.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Enforce the request size before multipart parsing; file parts larger
	// than uploadMemoryBytes are buffered on disk, not in memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(uploadMemoryBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httputil.JSONError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE",
				fmt.Sprintf("upload exceeds the %d MB limit", maxUploadBytes>>20))
			return
		}
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "file field is required")
		return
	}
	defer file.Close()

	filePath := filepath.Join(r.FormValue("path"), header.Filename)

	if err := h.svc.WriteFileStream(r.Context(), basePath, filePath, file); err != nil {
		httputil.HandleError(w, err)
		return
	}

	h.logAction(r, "upload_file", filePath, fmt.Sprintf("uploaded %s (%d bytes)", header.Filename, header.Size))
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Download handles GET /api/websites/{websiteID}/files/download?path=...
// Serves the file content with a Content-Disposition header for download.
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	basePath, err := h.getWebsite(r)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		httputil.JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "path is required")
		return
	}

	content, err := h.svc.ReadFile(r.Context(), basePath, filePath)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	filename := filepath.Base(filePath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}
