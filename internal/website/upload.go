package website

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// UploadArchive saves an uploaded archive to a temporary location and extracts
// it into the website's document root in the background. Supported formats are
// .zip and .tar.gz/.tgz. Returns the background task ID.
func (s *Service) UploadArchive(ctx context.Context, websiteID string, file io.Reader, filename string) (string, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return "", err
	}

	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}

	// Determine archive type from filename.
	lowerName := strings.ToLower(filename)
	var archiveType string
	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		archiveType = "zip"
	case strings.HasSuffix(lowerName, ".tar.gz") || strings.HasSuffix(lowerName, ".tgz"):
		archiveType = "targz"
	default:
		return "", model.NewValidationError("unsupported archive format; use .zip or .tar.gz/.tgz")
	}

	// Stage the upload inside the website's own home, written as that
	// account: /tmp is shared with every other local account, which would
	// let another tenant read this archive while it is unpacked.
	tmpPath := "/home/" + w.WebUser + "/.jenderal-upload-" + ulid.Make().String()
	var stderr strings.Builder
	exit, err := s.exec.RunSudoWithInputStream(ctx, file, &stderr,
		"-u", w.WebUser, "--", "/usr/bin/tee", tmpPath)
	if err != nil {
		return "", fmt.Errorf("stage upload: %w", err)
	}
	if exit != 0 {
		return "", model.NewDomainError("UPLOAD_FAILED", strings.TrimSpace(stderr.String()), nil)
	}
	if _, err := s.exec.RunSudo(ctx, "-u", w.WebUser, "--", "chmod", "600", tmpPath); err != nil {
		_, _ = s.exec.RunSudo(ctx, "-u", w.WebUser, "--", "rm", "-f", tmpPath)
		return "", fmt.Errorf("restrict staged upload: %w", err)
	}

	// Build the extract + cleanup commands to run sequentially.
	var commands [][]string

	if w.AppType == "go" {
		// Go sites extract into the repository root (not the document root)
		// and then run the deploy hook so the app builds and restarts —
		// the same end state as a git deploy.
		taskID := tr.RunFuncWithOptions(
			taskrunner.Options{Name: "Upload archive to " + w.Domain, Module: "website", Timeout: 15 * time.Minute},
			func(taskCtx context.Context, write func(string)) error {
				return s.deployGoUpload(taskCtx, w, tmpPath, archiveType, write)
			},
		)
		return taskID, nil
	}

	switch archiveType {
	case "zip":
		commands = append(commands, []string{
			"sudo", "-u", w.WebUser, "unzip", "-o", tmpPath, "-d", w.DocumentRoot,
		})
	case "targz":
		commands = append(commands, []string{
			"sudo", "-u", w.WebUser, "tar", "-xzf", tmpPath, "-C", w.DocumentRoot,
		})
	}

	// Cleanup temp file after extraction.
	commands = append(commands, []string{"-u", w.WebUser, "--", "rm", "-f", tmpPath})

	taskID := tr.RunMultiple(
		"Upload archive to "+w.Domain,
		commands,
	)

	return taskID, nil
}

// deployGoUpload extracts an uploaded archive for a go site into the
// repository root and runs the deploy hook (build for go-build, restart +
// health check for go-binary).
func (s *Service) deployGoUpload(ctx context.Context, w model.Website, tmpPath, archiveType string, write func(string)) error {
	root := s.appWorkDir(ctx, w)
	var name string
	var args []string
	if archiveType == "targz" {
		name, args = "tar", []string{"-xzf", tmpPath, "-C", root}
	} else {
		name, args = "unzip", []string{"-o", tmpPath, "-d", root}
	}
	if _, err := s.exec.RunSudo(ctx, "-u", append([]string{w.WebUser, "--", name}, args...)...); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}
	write("archive extracted to " + root)
	_, _ = s.exec.RunSudo(ctx, "-u", w.WebUser, "--", "rm", "-f", tmpPath)
	return s.RunDeployHook(ctx, w.ID, write)
}
