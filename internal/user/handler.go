package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles user management HTTP requests.
type Handler struct {
	auth  *auth.Service
	rbac  *auth.RBAC
	audit *audit.Service
}

// NewHandler creates a new user Handler.
func NewHandler(authSvc *auth.Service, rbac *auth.RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{
		auth:  authSvc,
		rbac:  rbac,
		audit: auditSvc,
	}
}

// List returns all users.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.auth.ListUsers(r.Context())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, users)
}

// Get returns a single user by ID.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, user)
}

// Create creates a new user and assigns a role.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		httputil.HandleError(w, model.NewValidationError("username, email, and password are required"))
		return
	}

	user, err := h.auth.CreateUser(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Assign role (default to "user" if not specified).
	role := req.Role
	if role == "" {
		role = "user"
	}
	_ = h.rbac.AssignRole(r.Context(), user.ID, role)

	caller, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "create_user",
		Module: "users",
		Target: user.ID,
		Detail: "created user " + user.Username,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusCreated, user)
}

// Update updates a user's profile fields.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		IsActive *bool  `json:"is_active"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Fetch existing user to fill in defaults for missing fields.
	existing, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	username := req.Username
	if username == "" {
		username = existing.Username
	}
	email := req.Email
	if email == "" {
		email = existing.Email
	}
	isActive := existing.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	user, err := h.auth.UpdateUser(r.Context(), id, username, email, isActive)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	caller, _ := auth.UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "update_user",
		Module: "users",
		Target: user.ID,
		Detail: "updated user " + user.Username,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, user)
}

// Delete deletes a user. Prevents self-deletion.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	caller, ok := auth.UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	if caller.ID == id {
		httputil.HandleError(w, model.NewValidationError("cannot delete your own account"))
		return
	}

	// Fetch the target user before deleting for the audit log.
	target, err := h.auth.GetUserByID(r.Context(), id)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.auth.DeleteUser(r.Context(), id); err != nil {
		httputil.HandleError(w, err)
		return
	}

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: caller.ID,
		Action: "delete_user",
		Module: "users",
		Target: target.ID,
		Detail: "deleted user " + target.Username,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdatePassword updates a user's password.
func (h *Handler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Password string `json:"password"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Password == "" {
		httputil.HandleError(w, model.NewValidationError("password is required"))
		return
	}

	if err := h.auth.UpdatePassword(r.Context(), id, req.Password); err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
