package paneldomain

import (
	"strings"
	"testing"
)

func TestRenderHTTPVhost(t *testing.T) {
	out := renderHTTPVhost("panel.example.com", "/var/lib/jenderal/acme-challenges", "http://127.0.0.1:8443")
	for _, want := range []string{
		"server_name panel.example.com",
		"proxy_pass http://127.0.0.1:8443",
		"/var/lib/jenderal/acme-challenges",
		"$jenderal_ws_panel_example_com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("http vhost missing %q", want)
		}
	}
}

func TestRenderTLSVhost(t *testing.T) {
	out := renderTLSVhost("panel.example.com", "http://127.0.0.1:8443", "/certs/panel.crt", "/certs/panel.key", "/webroot")
	for _, want := range []string{
		"listen 443 ssl",
		"listen 80",
		"server_name panel.example.com",
		"ssl_certificate /certs/panel.crt",
		"ssl_certificate_key /certs/panel.key",
		"proxy_pass http://127.0.0.1:8443",
		"/webroot",
		"$jenderal_ws_panel_example_com_wss",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tls vhost missing %q", want)
		}
	}
	// The final vhost must keep the port-80 ACME listener so renewals do not
	// fall onto some other site's default server.
	if strings.Count(out, "server {") != 2 {
		t.Errorf("tls vhost must carry the :80 and :443 servers, got %d", strings.Count(out, "server {"))
	}
}

func TestRenderVhostsHTTPSUpstream(t *testing.T) {
	const upstream = "https://127.0.0.1:8443"
	httpOut := renderHTTPVhost("panel.example.com", "/webroot", upstream)
	if !strings.Contains(httpOut, "proxy_pass "+upstream) {
		t.Errorf("http vhost missing https upstream: %q", httpOut)
	}
	tlsOut := renderTLSVhost("panel.example.com", upstream, "/c.pem", "/k.pem", "/webroot")
	if !strings.Contains(tlsOut, "proxy_pass "+upstream) {
		t.Errorf("tls vhost missing https upstream: %q", tlsOut)
	}
	// The panel's self-signed certificate would fail nginx verification.
	for _, out := range []string{httpOut, tlsOut} {
		if !strings.Contains(out, "proxy_ssl_verify off") {
			t.Errorf("vhost missing proxy_ssl_verify off for https upstream: %q", out)
		}
	}
	// Plain-HTTP upstreams must not carry the verification bypass.
	if strings.Contains(renderHTTPVhost("panel.example.com", "/w", "http://127.0.0.1:8443"), "proxy_ssl_verify") {
		t.Error("http upstream must not set proxy_ssl_verify")
	}
}

func TestWSMapVarSeparatesSchemes(t *testing.T) {
	if wsMapVar("panel.example.com", false) == wsMapVar("panel.example.com", true) {
		t.Error("http and https map variables must differ")
	}
}

func TestPanelDomainRegex(t *testing.T) {
	valid := []string{"panel.example.com", "a.io", "my.panel.example.com"}
	invalid := []string{"", "-panel.example.com", "panel..example.com", "panel example.com"}
	for _, d := range valid {
		if !panelDomainRegex.MatchString(d) {
			t.Errorf("domain %q should be valid", d)
		}
	}
	for _, d := range invalid {
		if panelDomainRegex.MatchString(d) {
			t.Errorf("domain %q should be invalid", d)
		}
	}
}
