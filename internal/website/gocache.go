package website

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Building a Go site on the server fills two caches in the website home:
// the module cache (~/go/pkg/mod, every dependency's source) and the build
// cache (~/.cache/go-build, compiled packages). Together they reach several
// hundred MB for a project whose binary is a few MB, and nothing cleared
// them.

// goCacheUserPattern matches the website users the cache script may run as.
var goCacheUserPattern = regexp.MustCompile(`^web_[a-z0-9_]{1,40}$`)

// goBuildEnv is the environment Go commands run with for a website user.
// HOME pins both caches inside the website home whatever sudo passes, and
// -modcacherw keeps the module cache deletable: by default Go writes it
// read-only, so removing a home or the cache by hand fails part way.
func goBuildEnv(webUser string) string {
	return goToolchainPath + " HOME=/home/" + webUser + ` GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"`
}

// goCacheScript prints the bytes held by the build cache (and the module
// cache when withModules is set), clears them, and prints the bytes left.
func goCacheScript(webUser string, withModules bool) string {
	flags := "-cache"
	paths := `"$(go env GOCACHE)"`
	if withModules {
		flags += " -modcache"
		paths += ` "$(go env GOMODCACHE)"`
	}
	measure := `du -scb ` + paths + ` 2>/dev/null | tail -n 1 | cut -f1`
	return "export " + goBuildEnv(webUser) +
		" && command -v go >/dev/null || exit 0" +
		" && before=$(" + measure + ")" +
		" && go clean " + flags +
		" && after=$(" + measure + ")" +
		` && echo "${before:-0} ${after:-0}"`
}

// clearGoCache runs goCacheScript as the website user and returns the bytes
// it freed.
func (s *Service) clearGoCache(ctx context.Context, webUser string, withModules bool) (int64, error) {
	if !goCacheUserPattern.MatchString(webUser) {
		return 0, model.NewValidationError("invalid website user")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := s.exec.RunSudo(ctx, "-u", webUser, "--", "/bin/bash", "-c", goCacheScript(webUser, withModules))
	if err != nil {
		return 0, fmt.Errorf("clear Go cache: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		detail := "no result"
		if res != nil {
			detail = strings.TrimSpace(res.Stderr)
		}
		return 0, fmt.Errorf("clear Go cache: %s", detail)
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) < 2 {
		return 0, nil
	}
	before, _ := strconv.ParseInt(fields[len(fields)-2], 10, 64)
	after, _ := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if before > after {
		return before - after, nil
	}
	return 0, nil
}

// trimGoBuildCache clears the build cache after a successful build. The
// module cache is kept so the next deploy does not download every
// dependency again. A failure is reported but never fails the build.
func (s *Service) trimGoBuildCache(ctx context.Context, w model.Website, write func(string)) {
	freed, err := s.clearGoCache(ctx, w.WebUser, false)
	if err != nil {
		write("WARNING: " + err.Error())
		return
	}
	if freed > 0 {
		write(fmt.Sprintf("Cleared the Go build cache (%d MB)", freed>>20))
	}
}

// ClearGoCache removes a Go website's build cache on request, and the
// module cache too when withModules is set.
func (s *Service) ClearGoCache(ctx context.Context, websiteID string, withModules bool) (int64, error) {
	w, err := s.goWebsite(ctx, websiteID)
	if err != nil {
		return 0, err
	}
	return s.clearGoCache(ctx, w.WebUser, withModules)
}

// GoCacheSize is how much a Go website's caches currently hold.
type GoCacheSize struct {
	BuildBytes  int64 `json:"build_bytes"`
	ModuleBytes int64 `json:"module_bytes"`
}

// goCacheSizeScript prints the build cache and module cache sizes in
// bytes. Without a Go toolchain it measures the default locations, so a
// leftover cache is still reported.
func goCacheSizeScript(webUser string) string {
	size := func(dir string) string {
		return `$(du -sb "` + dir + `" 2>/dev/null | cut -f1)`
	}
	return "export " + goBuildEnv(webUser) +
		` && if command -v go >/dev/null; then build=$(go env GOCACHE); mods=$(go env GOMODCACHE);` +
		` else build="$HOME/.cache/go-build"; mods="$HOME/go/pkg/mod"; fi` +
		` && b=` + size("$build") + ` && m=` + size("$mods") +
		` && echo "${b:-0} ${m:-0}"`
}

// GoCacheSize reports the size of a Go website's build and module caches.
func (s *Service) GoCacheSize(ctx context.Context, websiteID string) (GoCacheSize, error) {
	var size GoCacheSize
	w, err := s.goWebsite(ctx, websiteID)
	if err != nil {
		return size, err
	}
	if !goCacheUserPattern.MatchString(w.WebUser) {
		return size, model.NewValidationError("invalid website user")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	res, err := s.exec.RunSudo(ctx, "-u", w.WebUser, "--", "/bin/bash", "-c", goCacheSizeScript(w.WebUser))
	if err != nil {
		return size, fmt.Errorf("measure Go cache: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		return size, nil // nothing measurable: report empty caches
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) >= 2 {
		size.BuildBytes, _ = strconv.ParseInt(fields[len(fields)-2], 10, 64)
		size.ModuleBytes, _ = strconv.ParseInt(fields[len(fields)-1], 10, 64)
	}
	return size, nil
}

func (s *Service) goWebsite(ctx context.Context, websiteID string) (model.Website, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return model.Website{}, err
	}
	if w.AppType != "go" {
		return model.Website{}, model.NewValidationError("the Go cache only exists for Go websites")
	}
	return w, nil
}
