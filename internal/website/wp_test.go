package website

import (
	"strings"
	"testing"
	"time"
)

func TestResolveProfileWordPress(t *testing.T) {
	profile, err := ResolveProfile(CreateRequest{Template: "wordpress", PHPVersion: "8.2", SetupMode: SetupAutomatic})
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if profile.AppType != "wordpress" || profile.NginxProfile != "wordpress" {
		t.Fatalf("profile = %+v, want wordpress app type and nginx profile", profile)
	}
	if profile.RelativeDocumentRoot != "public" {
		t.Errorf("document root = %q, want public", profile.RelativeDocumentRoot)
	}
	if profile.Framework != "wordpress" {
		t.Errorf("framework = %q, want wordpress (auto-install requires a framework)", profile.Framework)
	}

	// Framework selections are rejected: WordPress has no such variants.
	if _, err := ResolveProfile(CreateRequest{Template: "wordpress", PHPVersion: "8.2", FrontendStack: "blade", SetupMode: SetupAutomatic}); err == nil {
		t.Error("frontend stack must be rejected for wordpress")
	}
}

func TestInstallationPlanWordPressUsesWpCliInsideHome(t *testing.T) {
	w := websiteRow{
		ID: "site-1", Domain: "example.com", AppType: "wordpress", PHPVersion: "8.2",
		DocumentRoot: "/home/web_example_com/public", WebUser: "web_example_com",
		Framework: "wordpress", NginxProfile: "wordpress", SetupMode: SetupAutomatic,
	}
	steps, err := installationPlan(w)
	if err != nil {
		t.Fatalf("installationPlan: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("expected installation steps for wordpress")
	}

	joined := ""
	for _, step := range steps {
		joined += step.Name + "\n" + strings.Join(step.Args, " ") + "\n"
		// The phar must live in the hidden home directory, never in the
		// staged web root that gets promoted to the document root.
		if strings.Contains(strings.Join(step.Args, " "), "/.jenderal-install-site-1/wp-cli.phar") {
			t.Errorf("wp-cli.phar must not be staged inside the web root: %v", step.Args)
		}
	}
	if !strings.Contains(joined, "wp-cli.phar") {
		t.Error("expected wp-cli.phar download step")
	}
	if !strings.Contains(joined, "core install") {
		t.Error("expected wp core install step")
	}
	if !strings.Contains(joined, "sqlite-database-integration") {
		t.Error("expected sqlite integration plugin steps")
	}
	_ = time.Now
}

func TestWordPressNginxDirectivesProtectSqliteAndXmlrpc(t *testing.T) {
	data := VhostData{Domain: "example.com", Profile: "wordpress", PHPVersion: "8.2"}
	d := directivesFor(data)
	if !strings.Contains(d.Server, "/wp-content/database/") {
		t.Errorf("sqlite database directory must be denied, server = %q", d.Server)
	}
	if !strings.Contains(d.Server, "xmlrpc.php") {
		t.Errorf("xmlrpc.php must be blocked, server directives = %q", d.Server)
	}
	if !strings.Contains(d.Location, "index.php?$query_string") {
		t.Errorf("permalinks require try_files fallback, location = %q", d.Location)
	}
}

func TestParseCurlLatency(t *testing.T) {
	if got := parseCurlLatency("200 0.123"); got != 123 {
		t.Fatalf("parseCurlLatency = %d, want 123", got)
	}
	if got := parseCurlLatency(""); got != 0 {
		t.Fatalf("parseCurlLatency(empty) = %d, want 0", got)
	}
}

func TestHealthCheckURL(t *testing.T) {
	if got := healthCheckURL("example.com", ""); got != "http://example.com" {
		t.Errorf("healthCheckURL default = %q", got)
	}
	if got := healthCheckURL("example.com", "https://example.com/health"); got != "https://example.com/health" {
		t.Errorf("healthCheckURL custom = %q", got)
	}
}
