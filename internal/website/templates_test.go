package website

import (
	"strings"
	"testing"
)

func TestRenderVhost_PHP(t *testing.T) {
	data := VhostData{
		Domain:       "example.com",
		Aliases:      "www.example.com",
		DocumentRoot: "/home/web_example_com/public",
		LogDir:       "/home/web_example_com/logs",
		PHPVersion:   "8.2",
		AppType:      "php",
	}

	output, err := RenderVhost(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name     string
		contains string
	}{
		{"server_name", "server_name example.com www.example.com;"},
		{"fastcgi_pass", "fastcgi_pass unix:/run/php/php8.2-fpm-example.com.sock;"},
		{"document_root", "root /home/web_example_com/public;"},
		{"deny_dotfiles", "deny all;"},
		{"try_files_php", "try_files $uri $uri/ /index.php?$query_string;"},
	}

	for _, tc := range checks {
		if !strings.Contains(output, tc.contains) {
			t.Errorf("%s: expected output to contain %q, got:\n%s", tc.name, tc.contains, output)
		}
	}
}

func TestRenderVhost_PHPUsesPackagedFastCGIParams(t *testing.T) {
	output, err := RenderVhost(VhostData{
		Domain:       "example.com",
		DocumentRoot: "/home/web_example_com/public",
		LogDir:       "/home/web_example_com/logs",
		PHPVersion:   "8.3",
		AppType:      "php",
	})
	if err != nil {
		t.Fatalf("RenderVhost() error = %v", err)
	}

	if !strings.Contains(output, "include fastcgi_params;") {
		t.Fatalf("PHP vhost does not include the packaged FastCGI params file:\n%s", output)
	}
	if strings.Contains(output, "snippets/fastcgi-params.conf") {
		t.Errorf("PHP vhost still includes the unavailable FastCGI params snippet:\n%s", output)
	}
	if !strings.Contains(output, "fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;") {
		t.Errorf("PHP vhost does not set SCRIPT_FILENAME explicitly:\n%s", output)
	}
}

func TestRenderVhostUsesDedicatedACMEChallengeRoot(t *testing.T) {
	output, err := RenderVhost(VhostData{
		Domain:            "example.com",
		DocumentRoot:      "/home/web_example/public",
		ACMEChallengeRoot: "/var/lib/jenderal/acme-challenges",
		LogDir:            "/home/web_example/logs",
		AppType:           "static",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "location ^~ /.well-known/acme-challenge/ {\n        root /var/lib/jenderal/acme-challenges;") {
		t.Fatalf("ACME location does not use the dedicated challenge root:\n%s", output)
	}
}

func TestRenderVhost_OmitsIPv6WhenUnavailable(t *testing.T) {
	output, err := RenderVhost(VhostData{
		Domain:       "example.com",
		DocumentRoot: "/srv/example",
		LogDir:       "/var/log/example",
		AppType:      "static",
		IPv6:         false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "listen [::]:80;") {
		t.Fatalf("unexpected IPv6 listener:\n%s", output)
	}
}

func TestRenderVhost_IncludesIPv6WhenAvailable(t *testing.T) {
	output, err := RenderVhost(VhostData{
		Domain:       "example.com",
		DocumentRoot: "/srv/example",
		LogDir:       "/var/log/example",
		AppType:      "static",
		IPv6:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "listen [::]:80;") {
		t.Fatalf("missing IPv6 listener:\n%s", output)
	}
}

func TestRenderTLSVhost_RoutesAliasThroughPrimaryPHPPool(t *testing.T) {
	output, err := RenderTLSVhost(TLSVhostData{
		VhostData: VhostData{
			Domain:       "example.com",
			DocumentRoot: "/srv/example",
			LogDir:       "/var/log/example",
			PHPVersion:   "8.3",
			AppType:      "php",
			IPv6:         false,
		},
		TLSDomain:       "www.example.com",
		CertificatePath: "/etc/jenderal/ssl/www.example.com/cert.pem",
		PrivateKeyPath:  "/etc/jenderal/ssl/www.example.com/key.pem",
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"listen 443 ssl;",
		"server_name www.example.com;",
		"php8.3-fpm-example.com.sock",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("missing %q:\n%s", check, output)
		}
	}
	if strings.Contains(output, "listen [::]:443") {
		t.Fatalf("unexpected IPv6 listener:\n%s", output)
	}
}

func TestRenderVhost_SeparatesHTTPSRedirectDomains(t *testing.T) {
	output, err := RenderVhost(VhostData{
		Domain:          "example.com",
		Aliases:         "www.example.com api.example.com",
		DocumentRoot:    "/srv/example",
		LogDir:          "/var/log/example",
		AppType:         "static",
		RedirectDomains: []string{"www.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"server_name example.com api.example.com;",
		"server_name www.example.com;",
		"location ^~ /.well-known/acme-challenge/",
		"return 301 https://$host$request_uri;",
	}
	for _, check := range checks {
		if !strings.Contains(output, check) {
			t.Errorf("missing %q:\n%s", check, output)
		}
	}
}

func TestRenderVhost_Static(t *testing.T) {
	data := VhostData{
		Domain:       "static.example.com",
		DocumentRoot: "/home/web_static_example_com/public",
		LogDir:       "/home/web_static_example_com/logs",
		AppType:      "static",
	}

	output, err := RenderVhost(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(output, "fastcgi") {
		t.Errorf("static vhost should not contain fastcgi block, got:\n%s", output)
	}

	if !strings.Contains(output, "try_files $uri $uri/ =404;") {
		t.Errorf("expected static try_files directive, got:\n%s", output)
	}

	if !strings.Contains(output, "server_name static.example.com;") {
		t.Errorf("expected server_name, got:\n%s", output)
	}
}

func TestRenderPool(t *testing.T) {
	data := PoolData{
		Domain:     "example.com",
		WebUser:    "web_example_com",
		PHPVersion: "8.2",
		HomeDir:    "/home/web_example_com",
		LogDir:     "/home/web_example_com/logs",
	}

	output, err := RenderPool(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name     string
		contains string
	}{
		{"pool_name", "[example.com]"},
		{"user", "user = web_example_com"},
		{"socket", "listen = /run/php/php8.2-fpm-example.com.sock"},
		{"open_basedir", "php_admin_value[open_basedir] = /home/web_example_com:/tmp:/usr/share/php"},
		{"session_save_path", "php_admin_value[session.save_path] = /home/web_example_com/tmp"},
		{"pm_dynamic", "pm = dynamic"},
		{"max_children", "pm.max_children = 5"},
	}

	for _, tc := range checks {
		if !strings.Contains(output, tc.contains) {
			t.Errorf("%s: expected output to contain %q, got:\n%s", tc.name, tc.contains, output)
		}
	}
}

func TestDomainToUser(t *testing.T) {
	tests := []struct {
		domain   string
		expected string
	}{
		{"example.com", "web_example_com"},
		{"sub.my-site.com", "web_sub_my_site_com"},
		{"test.org", "web_test_org"},
		{"my-app.dev", "web_my_app_dev"},
		{"a.b.c.d.com", "web_a_b_c_d_com"},
	}

	for _, tc := range tests {
		got := DomainToUser(tc.domain)
		if got != tc.expected {
			t.Errorf("DomainToUser(%q) = %q, want %q", tc.domain, got, tc.expected)
		}
	}
}
