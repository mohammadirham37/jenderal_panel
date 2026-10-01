package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Notifier delivers a message to the panel's configured notification
// channels. Satisfied by *notification.Service; kept as an interface so
// auth does not depend on the notification package.
type Notifier interface {
	SendAll(ctx context.Context, message string) error
}

// Handler handles authentication-related HTTP requests.
type Handler struct {
	auth     *Service
	rbac     *RBAC
	audit    *audit.Service
	throttle *loginThrottle
	notifier Notifier
}

// NewHandler creates a new auth Handler.
func NewHandler(authSvc *Service, rbac *RBAC, auditSvc *audit.Service) *Handler {
	return &Handler{throttle: newLoginThrottle(),
		auth:  authSvc,
		rbac:  rbac,
		audit: auditSvc,
	}
}

// SetNotifier wires the notification service used to alert configured
// channels when a login arrives from an IP the account never used before.
func (h *Handler) SetNotifier(n Notifier) {
	h.notifier = n
}

// setSessionCookies issues the session and CSRF cookies used by login and
// login-as flows: HttpOnly session cookie plus a JS-readable CSRF token,
// both bound to the session's expiry.
func setSessionCookies(w http.ResponseWriter, sessionID string, expiresAt time.Time, csrfToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
	})
}

// SetSessionCookies is the exported form used by other modules that start or
// restore sessions (e.g. the users module's login-as handler).
func SetSessionCookies(w http.ResponseWriter, sessionID string, expiresAt time.Time, csrfToken string) {
	setSessionCookies(w, sessionID, expiresAt, csrfToken)
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
			_ = h.audit.Log(r.Context(), audit.LogEntry{
				Action: "login_failed",
				Module: "auth",
				Target: req.Username,
				Detail: "failed login attempt",
				IP:     r.RemoteAddr,
			})
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

	// Session + CSRF cookies (HttpOnly session, readable CSRF).
	csrfToken := GenerateCSRFToken()
	setSessionCookies(w, session.ID, session.ExpiresAt, csrfToken)

	permissions, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)
	roles, _ := h.rbac.GetUserRoles(r.Context(), user.ID)

	// Audit log.
	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "login",
		Module: "auth",
		Target: user.Username,
		Detail: "user logged in",
		IP:     r.RemoteAddr,
	})

	// Notify configured channels on the first successful login from an IP
	// this account has never used. Sent in the background so a slow SMTP or
	// webhook cannot delay the login response.
	if h.notifier != nil {
		if isNew, err := h.auth.MarkLogin(r.Context(), user.ID, r.RemoteAddr); err == nil && isNew {
			go func(username, ip, userAgent string) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				msg := "Jenderal Panel: new login for '" + username + "' from " + ip +
					" (" + userAgent + "). If this was not you, change your password and revoke sessions in Settings."
				_ = h.notifier.SendAll(ctx, msg)
			}(user.Username, r.RemoteAddr, r.UserAgent())
		}
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"user":        user,
		"roles":       roles,
		"permissions": permissions,
		"csrf_token":  csrfToken,
	})
}

// ListSessions returns the authenticated user's active sessions, marking
// which one the current request came from.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	current, _ := SessionFromContext(r.Context())

	sessions, err := h.auth.ListSessions(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	type sessionInfo struct {
		ID        string    `json:"id"`
		IPAddress string    `json:"ip_address"`
		UserAgent string    `json:"user_agent"`
		CreatedAt time.Time `json:"created_at"`
		ExpiresAt time.Time `json:"expires_at"`
		Current   bool      `json:"current"`
	}

	list := make([]sessionInfo, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, sessionInfo{
			ID:        s.ID,
			IPAddress: s.IPAddress,
			UserAgent: s.UserAgent,
			CreatedAt: s.CreatedAt,
			ExpiresAt: s.ExpiresAt,
			Current:   s.ID == current.ID,
		})
	}

	httputil.JSON(w, http.StatusOK, list)
}

// RevokeSession deletes one of the authenticated user's own sessions.
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		httputil.HandleError(w, model.NewValidationError("session id is required"))
		return
	}

	if _, err := h.auth.GetSessionForUser(r.Context(), sessionID, user.ID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	if err := h.auth.DeleteSession(r.Context(), sessionID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: user.ID,
		Action: "session_revoke",
		Module: "auth",
		Target: user.Username,
		Detail: "revoked session " + sessionID,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
// When the session was started by an admin through login-as, the response also
// carries the impersonation info so the UI can offer a switch back.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	roles, _ := h.rbac.GetUserRoles(r.Context(), user.ID)
	permissions, _ := h.rbac.GetUserPermissions(r.Context(), user.ID)

	resp := model.UserWithRoles{
		User:        user,
		Roles:       roles,
		Permissions: permissions,
	}
	if session, ok := SessionFromContext(r.Context()); ok && session.ImpersonatorSessionID != "" {
		if info, err := h.auth.ImpersonatedBy(r.Context(), session.ImpersonatorSessionID); err == nil {
			resp.Impersonation = info
		}
	}

	httputil.JSON(w, http.StatusOK, resp)
}

// StopImpersonation ends a login-as session and restores the admin session it
// was started from. The response mirrors the login shape so the frontend can
// swap its auth state in place.
func (h *Handler) StopImpersonation(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	if session.ImpersonatorSessionID == "" {
		httputil.HandleError(w, model.NewValidationError("this session is not impersonating anyone"))
		return
	}

	impersonated, err := h.auth.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	// The admin session may have been revoked or expired while impersonating;
	// in that case the only way back is a fresh login.
	adminSession, err := h.auth.GetSession(r.Context(), session.ImpersonatorSessionID)
	if err != nil {
		_ = h.auth.DeleteSession(r.Context(), session.ID)
		httputil.HandleError(w, model.NewDomainError("IMPERSONATION_UNAVAILABLE",
			"the original admin session is no longer active; please log in again", err))
		return
	}

	adminUser, err := h.auth.GetUserByID(r.Context(), adminSession.UserID)
	if err != nil || !adminUser.IsActive {
		_ = h.auth.DeleteSession(r.Context(), session.ID)
		httputil.HandleError(w, model.NewDomainError("IMPERSONATION_UNAVAILABLE",
			"the original admin account is no longer active; please log in again", err))
		return
	}

	if err := h.auth.DeleteSession(r.Context(), session.ID); err != nil {
		httputil.HandleError(w, err)
		return
	}

	csrfToken := GenerateCSRFToken()
	setSessionCookies(w, adminSession.ID, adminSession.ExpiresAt, csrfToken)

	roles, _ := h.rbac.GetUserRoles(r.Context(), adminUser.ID)
	permissions, _ := h.rbac.GetUserPermissions(r.Context(), adminUser.ID)

	_ = h.audit.Log(r.Context(), audit.LogEntry{
		UserID: adminUser.ID,
		Action: "impersonate_end",
		Module: "users",
		Target: impersonated.ID,
		Detail: "stopped impersonating " + impersonated.Username,
		IP:     r.RemoteAddr,
	})

	httputil.JSON(w, http.StatusOK, map[string]any{
		"user":        adminUser,
		"roles":       roles,
		"permissions": permissions,
		"csrf_token":  csrfToken,
	})
}

// TOTPStatus reports whether TOTP is enabled for the authenticated user.
func (h *Handler) TOTPStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httputil.HandleError(w, model.ErrUnauthorized)
		return
	}

	enabled, err := h.auth.IsTOTPEnabled(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
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
