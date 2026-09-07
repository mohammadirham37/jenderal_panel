package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/logging"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
)

// UserFromContext extracts the authenticated user from the context.
func UserFromContext(ctx context.Context) (model.User, bool) {
	u, ok := ctx.Value(userContextKey).(model.User)
	return u, ok
}

// SessionFromContext extracts the session from the context.
func SessionFromContext(ctx context.Context) (model.Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(model.Session)
	return s, ok
}

// SessionMiddleware validates the session cookie, loads the user, and injects
// both into the request context. If a Bearer token is present in the
// Authorization header, API-token authentication is attempted first. Requests
// without a valid session or token receive a 401 Unauthorized response.
func SessionMiddleware(authSvc *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try Bearer token authentication first.
			if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				user, err := authSvc.ValidateAPIToken(r.Context(), token)
				if err != nil {
					httputil.HandleError(w, model.ErrUnauthorized)
					return
				}

				ctx := context.WithValue(r.Context(), userContextKey, user)
				ctx = logging.WithUserID(ctx, user.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Fall back to session cookie authentication.
			cookie, err := r.Cookie("session_id")
			if err != nil {
				httputil.HandleError(w, model.ErrUnauthorized)
				return
			}

			session, err := authSvc.GetSession(r.Context(), cookie.Value)
			if err != nil {
				httputil.HandleError(w, model.ErrUnauthorized)
				return
			}

			user, err := authSvc.GetUserByID(r.Context(), session.UserID)
			if err != nil {
				httputil.HandleError(w, model.ErrUnauthorized)
				return
			}

			if !user.IsActive {
				httputil.HandleError(w, model.ErrUserInactive)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, sessionContextKey, session)
			ctx = logging.WithUserID(ctx, user.ID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission returns middleware that checks if the authenticated user
// has the specified permission via RBAC. Returns 403 Forbidden if denied.
func RequirePermission(rbac *RBAC, permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				httputil.HandleError(w, model.ErrUnauthorized)
				return
			}

			allowed, err := rbac.HasPermission(r.Context(), user.ID, permission)
			if err != nil || !allowed {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFMiddleware validates that the X-CSRF-Token header matches the csrf_token
// cookie for state-changing requests (anything other than GET, HEAD, OPTIONS).
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("csrf_token")
		if err != nil {
			httputil.HandleError(w, model.ErrForbidden)
			return
		}

		headerToken := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(headerToken)) != 1 {
			httputil.HandleError(w, model.ErrForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GenerateCSRFToken returns a cryptographically random 32-byte hex-encoded token.
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
