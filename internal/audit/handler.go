package audit

import (
	"net/http"
	"strconv"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler handles audit log HTTP requests.
type Handler struct {
	svc *Service
}

// NewHandler creates a new audit Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// List returns paginated audit log entries, optionally filtered by module.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage < 1 {
		perPage = 20
	}

	module := q.Get("module")

	entries, total, err := h.svc.List(r.Context(), ListParams{
		Page:    page,
		PerPage: perPage,
		Module:  module,
		From:    q.Get("from"),
		To:      q.Get("to"),
		Search:  q.Get("search"),
	})
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSONList(w, entries, page, perPage, total)
}

// Modules handles GET /api/audit-logs/modules — distinct modules present in
// the log, for filter dropdowns.
func (h *Handler) Modules(w http.ResponseWriter, r *http.Request) {
	modules, err := h.svc.Modules(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	if modules == nil {
		modules = []string{}
	}
	httputil.JSON(w, http.StatusOK, modules)
}
