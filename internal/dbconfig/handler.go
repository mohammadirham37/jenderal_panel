package dbconfig

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles database server configuration HTTP requests.
type Handler struct {
	svc   *Service
	audit *audit.Service
}

// NewHandler creates a new dbconfig Handler.
func NewHandler(svc *Service, auditSvc *audit.Service) *Handler {
	return &Handler{svc: svc, audit: auditSvc}
}

// logAction writes an audit log entry for a mutating action.
func (h *Handler) logAction(r *http.Request, action, target, detail string) {
	user, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: action,
		Module: "dbconfig",
		Target: target,
		Detail: detail,
		IP:     r.RemoteAddr,
	})
}

// GetConfig handles GET /api/db-config/{engine}.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.Get(r.Context(), chi.URLParam(r, "engine"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, cfg)
}

// SaveConfig handles PUT /api/db-config/{engine}.
func (h *Handler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	engine := chi.URLParam(r, "engine")
	var body struct {
		Content string `json:"content"`
	}
	if err := httputil.DecodeJSON(r, &body); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.svc.Apply(r.Context(), engine, body.Content); err != nil {
		httputil.HandleError(w, err)
		return
	}
	h.logAction(r, "save_db_config", engine, "applied database server configuration")
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
