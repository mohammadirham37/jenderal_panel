package sshaccount

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestValidateUsername(t *testing.T) {
	cases := map[string]bool{
		"budi":        true,
		"a":           true,
		"user_1":      true,
		"a-b_c":       true,
		"_lead":       true,
		"":            false,
		"1abc":        false,
		"-abc":        false,
		"UpperCase":   false,
		"web_example": false,
		"has space":   false,
		"sixtyfourcharssixtyfourcharssixtyfourcharssixtyfourcharsoverflow": false,
	}
	for name, want := range cases {
		if got := ValidateUsername(name); got != want {
			t.Errorf("ValidateUsername(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestAuthorizedKeysContent(t *testing.T) {
	keys := []model.SSHKey{
		{PublicKey: "ssh-ed25519 AAAAC3Nza budi@laptop"},
		{PublicKey: "  ssh-rsa AAAAB3Nza budi@desktop  "},
	}
	content := authorizedKeysContent(keys)
	lines := strings.Split(content, "\n")
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("content = %q, want two keys with trailing newline", content)
	}
	if lines[0] != "ssh-ed25519 AAAAC3Nza budi@laptop" {
		t.Errorf("first line = %q, want trimmed key", lines[0])
	}
	if lines[1] != "ssh-rsa AAAAB3Nza budi@desktop" {
		t.Errorf("second line = %q, want trimmed key", lines[1])
	}
	if authorizedKeysContent(nil) != "" {
		t.Error("empty key list should render empty file")
	}
}

func TestFingerprintKeyRejectsMalformedInput(t *testing.T) {
	called := false
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		called = true
		return &executor.Result{ExitCode: 0, Stdout: "256 SHA256:abc comment (ED25519)"}, nil
	}}}

	for _, key := range []string{
		"",
		"not a key",
		"ssh-rsa",
		"command=\"rm -rf /\" ssh-rsa AAAAB3Nza",
		"no-pty ssh-ed25519 AAAAC3Nza",
		"ssh-ed25519 AAAAB2not/base64!! key",
		"ssh-ed25519 AAAAC3Nza extra1 extra2",
	} {
		if _, err := svc.fingerprintKey(context.Background(), key); err == nil {
			t.Errorf("fingerprintKey(%q) expected rejection", key)
		}
	}
	if called {
		t.Error("ssh-keygen should not run for structurally invalid keys")
	}
}

func TestFingerprintKeyUsesServerSideMetadata(t *testing.T) {
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name != "ssh-keygen" {
			t.Fatalf("expected ssh-keygen, got %s", name)
		}
		return &executor.Result{ExitCode: 0, Stdout: "3072 SHA256:ABCDEFG comment (RSA)\n"}, nil
	}}}

	key, err := svc.fingerprintKey(context.Background(), "ssh-rsa AAAAB3NzaC1 budi@host")
	if err != nil {
		t.Fatalf("fingerprintKey: %v", err)
	}
	if key.Fingerprint != "SHA256:ABCDEFG" {
		t.Errorf("fingerprint = %q", key.Fingerprint)
	}
	if key.Algo != "RSA" || key.Bits != 3072 {
		t.Errorf("algo/bits = %s/%d", key.Algo, key.Bits)
	}
}

func TestFingerprintKeyRejectsShortRSA(t *testing.T) {
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 0, Stdout: "1024 SHA256:SHORT k (RSA)"}, nil
	}}}
	if _, err := svc.fingerprintKey(context.Background(), "ssh-rsa AAAAB3NzaC1 short"); err == nil {
		t.Error("expected rejection of RSA keys below 2048 bits")
	}
}

func TestSanitizeKeyName(t *testing.T) {
	if got := sanitizeKeyName("  laptop \x07 key "); got != "laptop  key" {
		t.Errorf("sanitizeKeyName = %q", got)
	}
	long := strings.Repeat("x", 150)
	if got := sanitizeKeyName(long); len(got) != 100 {
		t.Errorf("sanitizeKeyName length = %d, want 100", len(got))
	}
}
