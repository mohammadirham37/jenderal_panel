package filemanager

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// maxDownloadPaths caps how many entries one archive download may name.
const maxDownloadPaths = 200

// downloadTimeout bounds one archive download (the HTTP server's write
// timeout is 2 hours).
const downloadTimeout = 2 * time.Hour

// zipStreamScript writes a zip archive of the given files and directories to
// stdout without a temporary file (zipfile handles unseekable output with
// data descriptors). Every entry is stored under its own basename. Symlinks
// are skipped: the archive holds what the site owns, not what links point
// at. Unreadable entries are reported on stderr and skipped so one
// permission-denied cache file does not abort a large download.
const zipStreamScript = `import os, sys, zipfile
out = sys.stdout.buffer
skipped = 0
def add(archive, path, arcname):
    global skipped
    try:
        archive.write(path, arcname)
    except OSError as err:
        skipped += 1
        sys.stderr.write("skipped %s: %s\n" % (arcname, err.strerror))
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
    for source in sys.argv[1:]:
        source = source.rstrip(os.sep)
        if os.path.islink(source):
            continue
        base = os.path.dirname(source)
        if not os.path.isdir(source):
            add(archive, source, os.path.basename(source))
            continue
        add(archive, source, os.path.basename(source))
        for root, dirs, files in os.walk(source, onerror=lambda e: sys.stderr.write("skipped %s: %s\n" % (e.filename, e.strerror))):
            dirs.sort()
            for name in dirs + sorted(files):
                path = os.path.join(root, name)
                if os.path.islink(path):
                    continue
                add(archive, path, os.path.relpath(path, base))
out.flush()
`

// ArchiveDownloadName names the zip for a download: the entry's own name for
// a single entry, otherwise the directory the entries were picked from.
func ArchiveDownloadName(paths []string) string {
	if len(paths) == 1 {
		return filepath.Base(filepath.Clean("/"+paths[0])) + ".zip"
	}
	parent := filepath.Base(filepath.Dir(filepath.Clean("/" + paths[0])))
	if parent == "/" || parent == "." {
		parent = "files"
	}
	return parent + "-" + time.Now().UTC().Format("20060102-150405") + ".zip"
}

// ValidateDownloadPaths resolves every requested entry inside the website
// home before anything is streamed, so a bad path is a normal error
// response instead of a truncated download.
func (s *Service) ValidateDownloadPaths(ctx context.Context, basePath string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, model.NewValidationError("choose at least one file or folder")
	}
	if len(paths) > maxDownloadPaths {
		return nil, model.NewValidationError(fmt.Sprintf("at most %d entries can be downloaded at once", maxDownloadPaths))
	}
	seen := make(map[string]bool, len(paths))
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		if err := rejectWebsiteRoot(basePath, p); err != nil {
			return nil, model.NewValidationError("the website root cannot be downloaded as a whole; select the folders inside it")
		}
		target, err := s.resolveWebsitePath(ctx, basePath, p, false)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(target)
		if seen[name] {
			return nil, model.NewValidationError("two selected entries share the name " + name)
		}
		seen[name] = true
		resolved = append(resolved, target)
	}
	return resolved, nil
}

// StreamZip writes a zip of the resolved entries to w, built as the website
// user. Skipped (unreadable) entries are returned as a warning string; err
// is set when the archive could not be completed.
func (s *Service) StreamZip(ctx context.Context, basePath string, resolved []string, w io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	webUser := filepath.Base(filepath.Clean(basePath))
	args := append([]string{"-u", webUser, "--", "python3", "-c", zipStreamScript}, resolved...)
	var stderr bytes.Buffer
	code, err := s.exec.RunSudoStreamSplit(ctx, w, &limitedWriter{w: &stderr, n: 64 << 10}, args[0], args[1:]...)
	warnings := strings.TrimSpace(stderr.String())
	if err != nil {
		return warnings, fmt.Errorf("zip download: %w", err)
	}
	if code != 0 {
		return warnings, fmt.Errorf("zip download exited %d: %s", code, warnings)
	}
	return warnings, nil
}

// limitedWriter keeps at most n bytes and discards the rest.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
