package auth

import (
	"net/http"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles authentication-related HTTP requests.
type Handler struct {
	auth  *Service
	rbac  *RBAC
	audit *audit.Service
}

// NewHandler creates a new auth Handler.
func NewHandler(authSvc *Service, rbac *RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{
		auth:  authSvc,
		rbac:  rbac,
		audit: auditSvc,
	}
}

// Login authenticates a user and creates a session.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.HandleError(w, model.NewValidationError("username and password are required"))
		return
	}

	user, err := h.auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	session, err := h.auth.CreateSession(r.Context(), user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	// Set session cookie (HttpOnly, Secure, SameSite Strict).
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
	})

	// Generate and set CSRF token cookie (NOT HttpOnly so JS can read it).
	csrfToken := GenerateCSRFToken()
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  session.ExpiresAt,
	})

	permissions, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)

	// Audit log.
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "login",
		Module: "auth",
		Target: user.Username,
		Detail: "user logged in",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]any{
		"user":        user,
		"permissions": permissions,
		"csrf_token":  csrfToken,
	})
}

// Logout destroys the current session and clears cookies.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	_ = h.auth.DeleteSession(r.Context(), session.ID)

	// Clear session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	// Clear CSRF cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	user, _ := UserFromContext(r.Context())
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "logout",
		Module: "auth",
		Target: user.Username,
		Detail: "user logged out",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Me returns the currently authenticated user with their roles and permissions.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	roles, _ := h.rbac.GetUserRoles(r.Context(), user.ID)
	permissions, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)

	httputil.JSON(w, http.StatusOK, model.UserWithRoles{
		User:        user,
		Roles:       roles,
		Permissions: permissions,
	})
}
