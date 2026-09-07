package settings

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.GetAll(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, settings)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	for k, v := range req {
		if err := h.svc.Set(r.Context(), k, v); err != nil {
			httputil.HandleError(w, err)
			return
		}
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
