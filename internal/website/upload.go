package website

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
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

	// Save uploaded file to a temp location.
	tmpName := "jenderal-upload-" + ulid.Make().String()
	tmpPath := "/tmp/" + tmpName
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// Build the extract + cleanup commands to run sequentially.
	var commands [][]string

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
	commands = append(commands, []string{"rm", "-f", tmpPath})

	taskID := tr.RunMultiple(
		"Upload archive to "+w.Domain,
		commands,
	)

	return taskID, nil
}
