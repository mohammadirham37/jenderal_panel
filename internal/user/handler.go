package user

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/sshaccount"
)

// Handler handles user management HTTP requests.
type Handler struct {
	auth  *auth.Service
	rbac  *auth.RBAC
	audit *audit.Service
	ssh   *sshaccount.Service
}

// NewHandler creates a new user Handler. The SSH account service is optional;
// when it is nil, SSH access flags are persisted without provisioning.
func NewHandler(authSvc *auth.Service, rbac *auth.RBAC, auditSvc *audit.Service, sshSvc *sshaccount.Service) *Handler {
	return &Handler{
		auth:  authSvc,
		rbac:  rbac,
		audit: auditSvc,
		ssh:   sshSvc,
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
		Username   string `json:"username"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		Role       string `json:"role"`
		SSHEnabled bool   `json:"ssh_enabled"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		httputil.HandleError(w, model.NewValidationError("username, email, and password are required"))
		return
	}
	if req.SSHEnabled && !sshaccount.ValidateUsername(req.Username) {
		httputil.HandleError(w, sshaccount.ErrInvalidUsername)
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

	if req.SSHEnabled {
		if err := h.provisionSSH(r, user.ID); err != nil {
			httputil.HandleError(w, err)
			return
		}
	}

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

// provisionSSH flips the panel flag first — the site ACL sync reads it —
// then creates the Linux account with access to the user's websites.
func (h *Handler) provisionSSH(r *http.Request, userID string) error {
	if err := h.auth.SetSSHEnabled(r.Context(), userID, true); err != nil {
		return err
	}
	if h.ssh != nil {
		if err := h.ssh.Provision(r.Context(), userID); err != nil {
			return err
		}
	}
	return nil
}

// Update updates a user's profile fields.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Username   string `json:"username"`
		Email      string `json:"email"`
		IsActive   *bool  `json:"is_active"`
		SSHEnabled *bool  `json:"ssh_enabled"`
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

	// A Linux account cannot be renamed: keep panel and system names in lockstep.
	if existing.SSHEnabled && req.Username != "" && req.Username != existing.Username {
		httputil.HandleError(w, model.NewValidationError("username cannot be changed while SSH access is enabled"))
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

	// SSH access lifecycle follows the panel flag and activation state.
	if req.SSHEnabled != nil && *req.SSHEnabled != existing.SSHEnabled {
		if *req.SSHEnabled {
			if err := h.provisionSSH(r, id); err != nil {
				httputil.HandleError(w, err)
				return
			}
		} else {
			if h.ssh != nil {
				if err := h.ssh.Lock(r.Context(), id); err != nil {
					httputil.HandleError(w, err)
					return
				}
			}
			if err := h.auth.SetSSHEnabled(r.Context(), id, false); err != nil {
				httputil.HandleError(w, err)
				return
			}
		}
	}
	if existing.SSHEnabled && h.ssh != nil {
		if req.IsActive != nil {
			if !*req.IsActive {
				if err := h.ssh.Lock(r.Context(), id); err != nil {
					httputil.HandleError(w, err)
					return
				}
			} else if !existing.IsActive {
				if err := h.ssh.Resume(r.Context(), id); err != nil {
					httputil.HandleError(w, err)
					return
				}
			}
		}
		if user.SSHEnabled {
			// Ownership may have changed elsewhere; converge the site ACLs.
			if err := h.ssh.SyncOwnedWebsites(r.Context(), id); err != nil {
				httputil.HandleError(w, err)
				return
			}
		}
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

// Delete deletes a user. Prevents self-deletion. The Linux SSH account is
// removed together with its home directory.
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

	// The panel row is gone; remove the system account best effort.
	if h.ssh != nil {
		if err := h.ssh.Delete(r.Context(), id); err != nil {
			httputil.HandleError(w, model.NewDomainError("SSH_ACCOUNT_DELETE_FAILED",
				"panel user deleted but the Linux account could not be removed: "+err.Error(), err))
			return
		}
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
