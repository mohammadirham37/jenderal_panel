package api

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/logging"
)

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = ulid.Make().String()
		}
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SafeRealIP replaces RemoteAddr with the forwarded client IP, but only when
// the direct TCP peer is loopback or a private address — i.e. a trusted
// reverse proxy such as the panel-domain nginx vhost. Direct connections
// (public peers) keep their real RemoteAddr, so a client cannot forge
// X-Forwarded-For to rotate its login-throttle key or poison audit logs.
func SafeRealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peer, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && isTrustedProxyIP(peer) {
			if ip := net.ParseIP(strings.TrimSpace(headerClientIP(r))); ip != nil {
				r.RemoteAddr = ip.String()
			}
		}
		next.ServeHTTP(w, r)
	})
}

// headerClientIP returns the client IP the proxy reported: X-Real-IP if set,
// otherwise the first entry of X-Forwarded-For.
func headerClientIP(r *http.Request) string {
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	xff := r.Header.Get("X-Forwarded-For")
	if i := strings.IndexByte(xff, ','); i >= 0 {
		xff = xff[:i]
	}
	return strings.TrimSpace(xff)
}

func isTrustedProxyIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// SecurityHeaders sets conservative response headers on every panel response.
// They cannot affect the SPA or API behaviour but blunt common browser-side
// attacks: MIME sniffing, framing/clickjacking, and referrer leakage. HSTS is
// only sent on TLS connections because the panel can also run plain HTTP.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", logging.RequestID(r.Context()),
				"ip", r.RemoteAddr,
			)
		})
	}
}

func RecovererMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				slog.Error("panic recovered",
					"error", rvr,
					"path", r.URL.Path,
					"request_id", logging.RequestID(r.Context()),
				)
				JSONError(w, http.StatusInternalServerError,
					"INTERNAL_ERROR", "an internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack preserves WebSocket and other upgraded connections through the
// logging wrapper. Optional ResponseWriter interfaces are otherwise hidden by
// the wrapper, causing Gorilla WebSocket upgrades to fail before the handler
// can accept the connection.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, readWriter, err := http.NewResponseController(rw.ResponseWriter).Hijack()
	if err == nil {
		rw.status = http.StatusSwitchingProtocols
	}
	return conn, readWriter, err
}
