package sshaccount

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler serves SSH public key management for admins (any panel user) and
// for self-service (the authenticated user's own keys).
type Handler struct {
	svc *Service
}

// NewHandler creates a new sshaccount Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type addKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

// ListUserKeys lists the stored SSH keys of any panel user. Admin action.
func (h *Handler) ListUserKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.svc.ListKeys(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, keys)
}

// AddUserKey stores a public key for any panel user. Admin action.
func (h *Handler) AddUserKey(w http.ResponseWriter, r *http.Request) {
	var req addKeyRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	key, err := h.svc.AddKey(r.Context(), chi.URLParam(r, "id"), req.Name, req.PublicKey)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, key)
}

// DeleteUserKey removes a stored key of any panel user. Admin action.
func (h *Handler) DeleteUserKey(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteKey(r.Context(), chi.URLParam(r, "id"), chi.URLParam(r, "keyID")); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListOwnKeys lists the authenticated user's SSH keys.
func (h *Handler) ListOwnKeys(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}
	keys, err := h.svc.ListKeys(r.Context(), caller.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, keys)
}

// AddOwnKey stores a public key for the authenticated user.
func (h *Handler) AddOwnKey(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}
	var req addKeyRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}
	key, err := h.svc.AddKey(r.Context(), caller.ID, req.Name, req.PublicKey)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, key)
}

// DeleteOwnKey removes one of the authenticated user's SSH keys.
func (h *Handler) DeleteOwnKey(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}
	if err := h.svc.DeleteKey(r.Context(), caller.ID, chi.URLParam(r, "keyID")); err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
