package website

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// GenerateDeployKey creates an ed25519 SSH deploy key for the website's system
// user and returns the public key. If a deploy key already exists it is
// overwritten.
func (s *Service) GenerateDeployKey(ctx context.Context, websiteID string) (string, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", err
	}

	sshDir := "/home/" + w.WebUser + "/.ssh"
	keyPath := sshDir + "/deploy_key"

	// Ensure .ssh directory exists.
	if err := s.runSudoOK(ctx, "mkdir", "-p", sshDir); err != nil {
		return "", fmt.Errorf("create .ssh directory: %w", err)
	}

	// Remove existing key to avoid ssh-keygen interactive overwrite prompt.
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", keyPath, keyPath+".pub")

	// Generate ed25519 deploy key.
	if err := s.runSudoOK(ctx, "ssh-keygen", "-t", "ed25519",
		"-f", keyPath,
		"-N", "",
		"-C", "jenderal-"+w.Domain,
	); err != nil {
		return "", fmt.Errorf("generate deploy key: %w", err)
	}

	// Write SSH config.
	sshConfig := "Host *\n" +
		"    IdentityFile " + keyPath + "\n" +
		"    StrictHostKeyChecking no\n" +
		"    UserKnownHostsFile /dev/null\n"

	configPath := sshDir + "/config"
	result, err := s.exec.RunSudoWithInput(ctx, sshConfig, "tee", configPath)
	if err != nil {
		return "", fmt.Errorf("write SSH config: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("write SSH config: %s", strings.TrimSpace(result.Stderr))
	}

	// Set correct permissions.
	if err := s.runSudoOK(ctx, "chmod", "700", sshDir); err != nil {
		return "", fmt.Errorf("chmod .ssh directory: %w", err)
	}
	if err := s.runSudoOK(ctx, "bash", "-c", "chmod 600 "+sshDir+"/*"); err != nil {
		return "", fmt.Errorf("chmod .ssh files: %w", err)
	}

	// Set ownership.
	if err := s.runSudoOK(ctx, "chown", "-R", w.WebUser+":"+w.WebUser, sshDir); err != nil {
		return "", fmt.Errorf("chown .ssh directory: %w", err)
	}

	// Read and return the public key.
	pubResult, err := s.exec.RunSudo(ctx, "cat", keyPath+".pub")
	if err != nil {
		return "", fmt.Errorf("read deploy key: %w", err)
	}
	if pubResult.ExitCode != 0 {
		return "", fmt.Errorf("read deploy key: %s", strings.TrimSpace(pubResult.Stderr))
	}

	return strings.TrimSpace(pubResult.Stdout), nil
}

// GetDeployKey returns the public deploy key for a website. The second return
// value indicates whether a deploy key exists.
func (s *Service) GetDeployKey(ctx context.Context, websiteID string) (string, bool, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", false, err
	}

	keyPath := "/home/" + w.WebUser + "/.ssh/deploy_key.pub"
	result, err := s.exec.RunSudo(ctx, "cat", keyPath)
	if err != nil {
		return "", false, fmt.Errorf("read deploy key: %w", err)
	}
	if result.ExitCode != 0 {
		return "", false, nil
	}

	return strings.TrimSpace(result.Stdout), true, nil
}

// DeleteDeployKey removes the deploy key and SSH config for a website.
func (s *Service) DeleteDeployKey(ctx context.Context, websiteID string) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if w.ID == "" {
		return model.ErrNotFound
	}

	sshDir := "/home/" + w.WebUser + "/.ssh"
	if err := s.runSudoOK(ctx, "rm", "-f",
		sshDir+"/deploy_key",
		sshDir+"/deploy_key.pub",
		sshDir+"/config",
	); err != nil {
		return fmt.Errorf("delete deploy key: %w", err)
	}

	return nil
}
