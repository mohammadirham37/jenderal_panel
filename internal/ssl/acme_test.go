package ssl

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	legoacme "github.com/go-acme/lego/v4/acme"
	"github.com/go-acme/lego/v4/lego"
)

func TestNewHTTP01ProviderUsesWebsiteWebroot(t *testing.T) {
	root := t.TempDir()
	provider, err := newHTTP01Provider(root)
	if err != nil {
		t.Fatalf("newHTTP01Provider() error = %v", err)
	}
	if err := provider.Present("example.com", "challenge-token", "key-authorization"); err != nil {
		t.Fatalf("Present() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.CleanUp("example.com", "challenge-token", "key-authorization") })

	content, err := os.ReadFile(filepath.Join(root, ".well-known", "acme-challenge", "challenge-token"))
	if err != nil {
		t.Fatalf("read challenge file: %v", err)
	}
	if string(content) != "key-authorization" {
		t.Fatalf("challenge content = %q", content)
	}
}

func TestACMEDirectoryURLDefaultsToLetsEncryptProduction(t *testing.T) {
	t.Setenv("LEGO_CA_URL", "")
	if got := acmeDirectoryURL(); got != lego.LEDirectoryProduction {
		t.Fatalf("acmeDirectoryURL() = %q, want production URL", got)
	}
}

func TestACMEDirectoryURLAllowsExplicitOverride(t *testing.T) {
	t.Setenv("LEGO_CA_URL", lego.LEDirectoryStaging)
	if got := acmeDirectoryURL(); got != lego.LEDirectoryStaging {
		t.Fatalf("acmeDirectoryURL() = %q, want staging override", got)
	}
}

func TestNewHTTP01ProviderRejectsMissingWebroot(t *testing.T) {
	if _, err := newHTTP01Provider(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("newHTTP01Provider() accepted a missing webroot")
	}
}

func TestAlreadyRevokedACMEErrorIsIdempotent(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &legoacme.ProblemDetails{Type: "urn:ietf:params:acme:error:alreadyRevoked"})
	if !isAlreadyRevokedError(err) {
		t.Fatal("already-revoked ACME response was not recognized")
	}
	if isAlreadyRevokedError(fmt.Errorf("network unavailable")) {
		t.Fatal("unrelated ACME error was treated as already revoked")
	}
}
