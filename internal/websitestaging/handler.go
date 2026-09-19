package websitestaging

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler exposes staging clone operations over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler creates a new websitestaging Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Create starts a staging clone for the website in the URL.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")

	var req struct {
		Domain string `json:"domain"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	clone, err := h.svc.StartClone(r.Context(), websiteID, req.Domain, user.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusAccepted, clone)
}

// List returns the clone history of the website in the URL.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	websiteID := chi.URLParam(r, "id")

	clones, err := h.svc.ListClones(r.Context(), websiteID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, clones)
}
