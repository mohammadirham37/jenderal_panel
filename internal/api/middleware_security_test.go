package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func safeRealIPRequest(t *testing.T, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	SafeRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.RemoteAddr))
	})).ServeHTTP(rec, req)
	return rec
}

// A direct public client must not be able to rotate its apparent IP via
// X-Forwarded-For — that would defeat the login throttle key and forge
// audit log entries.
func TestSafeRealIPPublicPeerKeepsRemoteAddr(t *testing.T) {
	for _, hdrs := range []map[string]string{
		{"X-Forwarded-For": "1.2.3.4"},
		{"X-Real-IP": "1.2.3.4"},
		{"X-Forwarded-For": "1.2.3.4, 10.0.0.1"},
		{},
	} {
		rec := safeRealIPRequest(t, "203.0.113.50:44321", hdrs)
		if got := rec.Body.String(); got != "203.0.113.50:44321" {
			t.Errorf("public peer: expected RemoteAddr to stay untouched, got %q (headers %v)", got, hdrs)
		}
	}
}

// The panel-domain nginx vhost proxies from loopback, so its X-Forwarded-For
// / X-Real-IP must keep being honored.
func TestSafeRealIPHonorsForwardedFromLoopbackOnly(t *testing.T) {
	cases := []struct {
		remote   string
		headers  map[string]string
		expected string
	}{
		{"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		{"127.0.0.1:5555", map[string]string{"X-Forwarded-For": "198.51.100.7, 10.0.0.2"}, "198.51.100.7"},
		{"127.0.0.1:5555", map[string]string{"X-Real-IP": "198.51.100.9"}, "198.51.100.9"},
		{"127.0.0.1:5555", map[string]string{}, "127.0.0.1:5555"},
		// Only a proxy on this host is trusted; a private-network peer could
		// be another container and is not believed.
		{"192.168.1.10:5555", map[string]string{"X-Forwarded-For": "198.51.100.11"}, "192.168.1.10:5555"},
	}
	for _, c := range cases {
		rec := safeRealIPRequest(t, c.remote, c.headers)
		if got := rec.Body.String(); got != c.expected {
			t.Errorf("peer %s headers %v: expected %q, got %q", c.remote, c.headers, c.expected, got)
		}
	}
}

// Garbage forwarded values are ignored rather than crashing or being copied.
func TestSafeRealIPInvalidForwardedIgnored(t *testing.T) {
	rec := safeRealIPRequest(t, "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "not an ip"})
	if got := rec.Body.String(); got != "127.0.0.1:5555" {
		t.Errorf("expected untouched RemoteAddr on garbage XFF, got %q", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// Plain HTTP: no HSTS (the panel can run without TLS), but the rest set.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options nosniff")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("expected X-Frame-Options DENY")
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("expected Referrer-Policy no-referrer")
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("expected no HSTS on plain HTTP")
	}

	// TLS: HSTS added.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tlsConnStateStub
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("expected HSTS on TLS connections")
	}
}

var tlsConnStateStub = tls.ConnectionState{HandshakeComplete: true}

// Containers and other hosts on a private network are not the panel's
// proxy: trusting their X-Forwarded-For would let them forge the client IP
// the login throttle and the audit log record.
func TestSafeRealIPTrustsLoopbackOnly(t *testing.T) {
	cases := []struct {
		peer, want string
	}{
		{"127.0.0.1:5000", "203.0.113.7"},
		{"[::1]:5000", "203.0.113.7"},
		{"172.17.0.5:5000", "172.17.0.5:5000"},
		{"10.1.2.3:5000", "10.1.2.3:5000"},
		{"198.51.100.9:5000", "198.51.100.9:5000"},
	}
	for _, tc := range cases {
		var seen string
		handler := SafeRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.RemoteAddr
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("X-Forwarded-For", "203.0.113.7")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if seen != tc.want {
			t.Errorf("peer %s: RemoteAddr = %q, want %q", tc.peer, seen, tc.want)
		}
	}
}

func TestSecurityHeadersSetContentSecurityPolicy(t *testing.T) {
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"object-src 'none'",
		"form-action 'self'",
		// The panel's own bootstrap scripts and component styles are inline,
		// and QR codes and charts render from data:/blob: URLs.
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		// Live metrics, logs, and the terminal use WebSockets.
		"connect-src 'self' ws: wss:",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
}

// Behind a TLS-terminating proxy the panel sees plain HTTP, but the browser
// spoke HTTPS and should still be told to keep doing so.
func TestSecurityHeadersSendHSTSBehindATLSProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS header behind a TLS proxy")
	}

	rec = httptest.NewRecorder()
	SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must not be sent on a plain HTTP request")
	}
}
