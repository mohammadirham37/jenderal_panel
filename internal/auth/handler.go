package auth

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Handler handles authentication-related HTTP requests.
type Handler struct {
	auth     *Service
	rbac     *RBAC
	audit    *audit.Service
	throttle *loginThrottle
}

// NewHandler creates a new auth Handler.
func NewHandler(authSvc *Service, rbac *RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{throttle: newLoginThrottle(),
		auth:  authSvc,
		rbac:  rbac,
		audit: auditSvc,
	}
}

// Login authenticates a user and creates a session.
// If TOTP is enabled and no totp_code is provided, returns {requires_totp: true}.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.HandleError(w, model.NewValidationError("username and password are required"))
		return
	}

	// Brute-force protection: too many failures from one source for one
	// account triggers a cooldown.
	key := r.RemoteAddr + "|" + req.Username
	if blocked, remaining := h.throttle.blocked(key, time.Now()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
		httputil.JSONError(w, http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS",
			"too many failed login attempts; try again later")
		return
	}

	user, err := h.auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, model.ErrInvalidCredentials) {
			h.throttle.recordFailure(key, time.Now())
		}
		httputil.HandleError(w, err)
		return
	}

	// Check if TOTP is enabled for this user.
	totpEnabled, err := h.auth.IsTOTPEnabled(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	if totpEnabled {
		if req.TOTPCode == "" {
			// Prompt the client to supply a TOTP code.
			httputil.JSON(w, http.StatusOK, map[string]any{
				"requires_totp": true,
			})
			return
		}

		valid, err := h.auth.TOTPVerify(r.Context(), user.ID, req.TOTPCode)
		if err != nil || !valid {
			h.throttle.recordFailure(key, time.Now())
			httputil.HandleError(w, model.NewValidationError("invalid TOTP code"))
			return
		}
	}

	h.throttle.reset(key)

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

// TOTPSetup initiates TOTP enrolment for the authenticated user.
func (h *Handler) TOTPSetup(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	secret, url, err := h.auth.TOTPSetup(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"url":    url,
	})
}

// TOTPEnable verifies a TOTP code and activates TOTP for the authenticated user.
func (h *Handler) TOTPEnable(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if req.Code == "" {
		httputil.HandleError(w, model.NewValidationError("code is required"))
		return
	}

	if err := h.auth.TOTPEnable(r.Context(), user.ID, req.Code); err != nil {
		httputil.HandleError(w, model.NewValidationError("invalid TOTP code"))
		return
	}

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "totp_enable",
		Module: "auth",
		Target: user.Username,
		Detail: "TOTP enabled",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// TOTPDisable removes TOTP for the authenticated user.
func (h *Handler) TOTPDisable(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	if err := h.auth.TOTPDisable(r.Context(), user.ID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "totp_disable",
		Module: "auth",
		Target: user.Username,
		Detail: "TOTP disabled",
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
