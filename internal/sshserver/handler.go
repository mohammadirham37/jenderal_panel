package sshserver

import (
	"net/http"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
)

// Handler serves the SSH port management endpoints.
type Handler struct {
	svc *Service
}

// NewHandler creates a new sshserver Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Status returns the effective SSH port and any pending change.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, status)
}

// BeginChange starts the two-phase port change.
func (h *Handler) BeginChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	if err := h.svc.BeginChange(r.Context(), req.Port); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{
		"status":  "pending",
		"message": "SSH is now listening on both ports; confirm you can log in on the new port, then finalize",
	})
}

// Finalize completes a pending port change and closes the old port.
func (h *Handler) Finalize(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Finalize(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Cancel rolls back a pending port change.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Cancel(r.Context()); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
