package httputil

import (
	"net/http"
	"net/url"
)

// SameOriginCheckOrigin is a websocket Upgrader CheckOrigin that only accepts
// browser connections whose Origin host matches the Host the request was
// addressed to (direct IP:8443 or the panel-domain vhost, which proxies
// Host through). Non-browser clients send no Origin header and are allowed.
// Browsers always send Origin on cross-site websocket handshakes, so this
// blocks other sites from opening authenticated sockets in a visitor's
// browser even if cookie policy ever changes.
func SameOriginCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Host == r.Host
}
