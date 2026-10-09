package website

import (
	"strings"
	"testing"
)

func TestResolveProfileDefaultsAndValidatesRequiredNodeRuntime(t *testing.T) {
	req := CreateRequest{
		Template: "laravel", FrameworkVersion: "13", PHPVersion: "8.3",
		FrontendStack: "inertia", InertiaAdapter: "svelte", ProjectVariant: "starter-kit", SetupMode: SetupAutomatic,
	}
	profile, err := ResolveProfile(req)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.RequiresNode || profile.NodeVersion != "24" {
		t.Fatalf("profile = %#v, want required Node 24 default", profile)
	}

	req.NodeVersion = "21"
	if _, err := ResolveProfile(req); err == nil || !strings.Contains(err.Error(), "unsupported Node") {
		t.Fatalf("ResolveProfile() error = %v, want unsupported Node validation", err)
	}
}

func TestResolveProfilePreservesOptionalNodeSelectionAndDefaultsToNone(t *testing.T) {
	withoutNode, err := ResolveProfile(CreateRequest{Template: "php", PHPVersion: "8.3", SetupMode: SetupConfigOnly})
	if err != nil {
		t.Fatal(err)
	}
	if withoutNode.NodeVersion != "" {
		t.Fatalf("NodeVersion = %q, want none", withoutNode.NodeVersion)
	}

	withNode, err := ResolveProfile(CreateRequest{Template: "php", PHPVersion: "8.3", SetupMode: SetupConfigOnly, NodeVersion: "22"})
	if err != nil {
		t.Fatal(err)
	}
	if withNode.NodeVersion != "22" {
		t.Fatalf("NodeVersion = %q, want explicit 22", withNode.NodeVersion)
	}
}

func TestResolveProfileDocumentRootsAndPHPCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateRequest
		wantDoc string
		wantErr string
	}{
		{"laravel 13 rejects php 8.2", CreateRequest{Template: "laravel", FrameworkVersion: "13", PHPVersion: "8.2"}, "", "PHP 8.3"},
		{"laravel inertia svelte config", CreateRequest{Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "svelte"}, "app/public", ""},
		{"codeigniter 4", CreateRequest{Template: "codeigniter4", PHPVersion: "8.3"}, "app/public", ""},
		{"plain php", CreateRequest{Template: "php", PHPVersion: "8.3"}, "public", ""},
		{"static", CreateRequest{Template: "static"}, "public", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := ResolveProfile(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResolveProfile() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProfile() error = %v", err)
			}
			if profile.RelativeDocumentRoot != tt.wantDoc {
				t.Fatalf("RelativeDocumentRoot = %q, want %q", profile.RelativeDocumentRoot, tt.wantDoc)
			}
		})
	}
}

func TestResolveProfileAutomaticLaravelSupportMatrix(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateRequest
		enabled bool
	}{
		{"laravel 11 empty blade", CreateRequest{Template: "laravel", FrameworkVersion: "11", PHPVersion: "8.2", FrontendStack: "blade", ProjectVariant: "empty", SetupMode: "automatic"}, true},
		{"laravel 11 react starter", CreateRequest{Template: "laravel", FrameworkVersion: "11", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "react", ProjectVariant: "starter-kit", SetupMode: "automatic"}, false},
		{"laravel 12 react starter", CreateRequest{Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "react", ProjectVariant: "starter-kit", SetupMode: "automatic"}, true},
		{"laravel 12 svelte starter", CreateRequest{Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "svelte", ProjectVariant: "starter-kit", SetupMode: "automatic"}, false},
		{"laravel 12 livewire starter", CreateRequest{Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "livewire", ProjectVariant: "starter-kit", SetupMode: "automatic"}, true},
		{"laravel 13 svelte starter", CreateRequest{Template: "laravel", FrameworkVersion: "13", PHPVersion: "8.3", FrontendStack: "inertia", InertiaAdapter: "svelte", ProjectVariant: "starter-kit", SetupMode: "automatic"}, true},
		{"empty inertia", CreateRequest{Template: "laravel", FrameworkVersion: "12", PHPVersion: "8.2", FrontendStack: "inertia", InertiaAdapter: "vue", ProjectVariant: "empty", SetupMode: "automatic"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveProfile(tt.req)
			if tt.enabled && err != nil {
				t.Fatalf("ResolveProfile() error = %v", err)
			}
			if !tt.enabled && (err == nil || strings.TrimSpace(err.Error()) == "") {
				t.Fatal("ResolveProfile() accepted disabled combination without a reason")
			}
		})
	}
}

func TestResolveProfileRejectsUnknownAllowlistValues(t *testing.T) {
	_, err := ResolveProfile(CreateRequest{Template: "laravel", FrameworkVersion: "99", PHPVersion: "8.4"})
	if err == nil {
		t.Fatal("ResolveProfile() accepted unknown Laravel version")
	}
}

func TestResolveProfileGoDefaults(t *testing.T) {
	p, err := ResolveProfile(CreateRequest{Template: "go-build", SetupMode: SetupConfigOnly})
	if err != nil {
		t.Fatalf("ResolveProfile(go-build) error = %v", err)
	}
	if p.AppType != "go" || p.AppRuntime != "go" || p.NginxProfile != "app-proxy" {
		t.Fatalf("go-build profile = %#v", p)
	}
	if p.AppBuildCommand != "go build -o app ." || p.AppStartCommand != "./app" {
		t.Fatalf("go-build commands = build %q start %q", p.AppBuildCommand, p.AppStartCommand)
	}
	if p.RelativeDocumentRoot != "app/public" {
		t.Fatalf("go-build docroot = %q, want app/public", p.RelativeDocumentRoot)
	}

	pb, err := ResolveProfile(CreateRequest{Template: "go-binary", SetupMode: SetupConfigOnly})
	if err != nil {
		t.Fatalf("ResolveProfile(go-binary) error = %v", err)
	}
	if pb.AppRuntime != "go-binary" || pb.AppBuildCommand != "" || pb.AppStartCommand != "./app" {
		t.Fatalf("go-binary profile = %#v", pb)
	}
	if pb.RelativeDocumentRoot != "app/public" {
		t.Fatalf("go-binary docroot = %q, want app/public", pb.RelativeDocumentRoot)
	}
}
