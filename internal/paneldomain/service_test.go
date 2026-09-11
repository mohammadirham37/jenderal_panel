package paneldomain

import (
	"strings"
	"testing"
)

func TestRenderHTTPVhost(t *testing.T) {
	out := renderHTTPVhost("panel.example.com", "/var/lib/jenderal/acme-challenges")
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
	out := renderTLSVhost("panel.example.com", "/certs/panel.crt", "/certs/panel.key", "/webroot")
	for _, want := range []string{
		"listen 443 ssl",
		"ssl_certificate /certs/panel.crt",
		"ssl_certificate_key /certs/panel.key",
		"proxy_pass http://127.0.0.1:8443",
		"$jenderal_ws_panel_example_com_wss",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tls vhost missing %q", want)
		}
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
