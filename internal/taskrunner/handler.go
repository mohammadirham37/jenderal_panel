package taskrunner

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

type Handler struct {
	runner *Runner
}

func NewHandler(runner *Runner) *Handler {
	return &Handler{runner: runner}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	task, ok := h.runner.Get(id)
	if !ok {
		httputil.JSONError(w, http.StatusNotFound, "NOT_FOUND", "task not found")
		return
	}
	httputil.JSON(w, http.StatusOK, task)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tasks := h.runner.List()
	httputil.JSON(w, http.StatusOK, tasks)
}
