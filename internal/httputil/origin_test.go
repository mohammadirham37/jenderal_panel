package httputil

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginCheckOrigin(t *testing.T) {
	makeReq := func(origin, host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "https://"+host+"/ws", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Host = host
		return r
	}

	if !SameOriginCheckOrigin(makeReq("https://panel.example.com", "panel.example.com")) {
		t.Error("same host must be allowed")
	}
	if !SameOriginCheckOrigin(makeReq("https://panel.example.com:8443", "panel.example.com:8443")) {
		t.Error("same host with port must be allowed")
	}
	// The nginx panel-domain vhost proxies Host through, so Origin and Host match.
	if !SameOriginCheckOrigin(makeReq("https://panel.example.com", "panel.example.com")) {
		t.Error("proxied same-origin must be allowed")
	}
	if !SameOriginCheckOrigin(makeReq("", "panel.example.com")) {
		t.Error("non-browser requests without Origin must be allowed")
	}
	if SameOriginCheckOrigin(makeReq("https://evil.example.net", "panel.example.com")) {
		t.Error("cross-site origin must be rejected")
	}
	if SameOriginCheckOrigin(makeReq("http://panel.example.com:9999", "panel.example.com")) {
		t.Error("different port origin must be rejected")
	}
}
