// Package sitesecurity owns the per-website nginx security include. Every
// website vhost includes exactly one file,
// /etc/nginx/jenderal/security/sites/<id>.conf. Two features write into it:
// Traffic Guard (rate and connection limits, real IP) and the WAF page
// (ModSecurity and DoS rate limiting). Each feature keeps its own fragment
// next to it (<id>.traffic.part, <id>.waf.part, never included by nginx)
// and the include is composed from both, so changing one no longer erases
// the other.
package sitesecurity

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Dir holds the per-website include files and their fragments.
const Dir = "/etc/nginx/jenderal/security/sites"

// Fragment owners.
const (
	PartTraffic = "traffic"
	PartWAF     = "waf"
)

var websiteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Files reads and writes root-owned nginx files.
type Files interface {
	Read(ctx context.Context, path string) (string, bool, error)
	Write(ctx context.Context, path, content string) error
	Remove(ctx context.Context, path string) error
}

// IncludePath is the file the website's vhost includes.
func IncludePath(websiteID string) string { return Dir + "/" + websiteID + ".conf" }

func fragmentPath(websiteID, part string) string {
	return Dir + "/" + websiteID + "." + part + ".part"
}

// Update stores part's fragment (empty content clears it) and rewrites the
// composed include. The returned undo restores every file it touched; call
// it when the following nginx validation fails.
func Update(ctx context.Context, files Files, websiteID, part, content string) (func(context.Context) error, error) {
	if !websiteIDPattern.MatchString(websiteID) {
		return nil, fmt.Errorf("invalid website id")
	}
	if part != PartTraffic && part != PartWAF {
		return nil, fmt.Errorf("unknown security fragment %q", part)
	}
	include := IncludePath(websiteID)
	previousInclude, includeExisted, err := files.Read(ctx, include)
	if err != nil {
		return nil, err
	}

	fragments := map[string]string{}
	existed := map[string]bool{}
	previous := map[string]string{}
	for _, p := range []string{PartTraffic, PartWAF} {
		body, ok, err := files.Read(ctx, fragmentPath(websiteID, p))
		if err != nil {
			return nil, err
		}
		fragments[p], existed[p], previous[p] = body, ok, body
	}
	// Includes written before fragments existed belong to one feature;
	// adopt that content as its fragment so it survives this change.
	if !existed[PartTraffic] && !existed[PartWAF] && includeExisted {
		if owner := legacyOwner(previousInclude); owner != "" && owner != part {
			fragments[owner] = previousInclude
		}
	}
	fragments[part] = content

	undo := func(ctx context.Context) error {
		var firstErr error
		keep := func(err error) {
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		for _, p := range []string{PartTraffic, PartWAF} {
			if existed[p] {
				keep(files.Write(ctx, fragmentPath(websiteID, p), previous[p]))
			} else {
				keep(files.Remove(ctx, fragmentPath(websiteID, p)))
			}
		}
		if includeExisted {
			keep(files.Write(ctx, include, previousInclude))
		} else {
			keep(files.Write(ctx, include, emptyInclude))
		}
		return firstErr
	}

	for _, p := range []string{PartTraffic, PartWAF} {
		if fragments[p] == "" {
			if existed[p] {
				if err := files.Remove(ctx, fragmentPath(websiteID, p)); err != nil {
					_ = undo(ctx)
					return nil, err
				}
			}
			continue
		}
		if err := files.Write(ctx, fragmentPath(websiteID, p), fragments[p]); err != nil {
			_ = undo(ctx)
			return nil, err
		}
	}
	if err := files.Write(ctx, include, Compose(fragments[PartTraffic], fragments[PartWAF])); err != nil {
		_ = undo(ctx)
		return nil, err
	}
	return undo, nil
}

// emptyInclude keeps the always-included file valid when nothing is on.
const emptyInclude = "# Managed by Jenderal Panel - no per-site protection enabled\n"

// legacyOwner tells which feature wrote a pre-fragment include.
func legacyOwner(content string) string {
	switch {
	case strings.Contains(content, "modsecurity on;"), strings.Contains(content, "zone=jenderal_dos"):
		return PartWAF
	case strings.Contains(content, "limit_req zone="), strings.Contains(content, "set_real_ip_from"),
		strings.Contains(content, "real_ip_header"):
		return PartTraffic
	default:
		return ""
	}
}

// Compose merges both fragments into one valid server-level include.
//
// nginx accepts several limit_req lines in one context but only one
// limit_req_status / limit_conn_status, so those are emitted once (429,
// Too Many Requests). limit_req_dry_run applies to every limit_req in the
// context: while DoS protection enforces, Traffic Guard's observe-mode
// limits are left out so they cannot turn DoS enforcement into a dry run.
func Compose(traffic, waf string) string {
	trafficLines := directives(traffic)
	wafLines := directives(waf)

	dosOn := false
	for _, line := range wafLines {
		if strings.HasPrefix(line, "limit_req ") {
			dosOn = true
		}
	}
	observe := false
	for _, line := range trafficLines {
		if strings.HasPrefix(line, "limit_req_dry_run") || strings.HasPrefix(line, "limit_conn_dry_run") {
			observe = true
		}
	}

	var b strings.Builder
	b.WriteString("# Managed by Jenderal Panel - composed from Traffic Guard and WAF settings\n")
	hasReq, hasConn := false, false
	write := func(line string) {
		switch {
		case strings.HasPrefix(line, "limit_req_status"), strings.HasPrefix(line, "limit_conn_status"):
			return
		case strings.HasPrefix(line, "limit_req "):
			hasReq = true
		case strings.HasPrefix(line, "limit_conn "):
			hasConn = true
		}
		b.WriteString(line + "\n")
	}

	if len(trafficLines) > 0 {
		b.WriteString("# Traffic Guard\n")
		for _, line := range trafficLines {
			if dosOn && observe && isLimitLine(line) {
				continue
			}
			write(line)
		}
		if dosOn && observe {
			b.WriteString("# Traffic Guard observe-mode limits are paused while DoS protection enforces.\n")
		}
	}
	if len(wafLines) > 0 {
		b.WriteString("# WAF and DoS protection\n")
		for _, line := range wafLines {
			write(line)
		}
	}
	if hasReq {
		b.WriteString("limit_req_status 429;\n")
	}
	if hasConn {
		b.WriteString("limit_conn_status 429;\n")
	}
	return b.String()
}

func isLimitLine(line string) bool {
	for _, prefix := range []string{"limit_req ", "limit_conn ", "limit_req_dry_run", "limit_conn_dry_run"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// directives returns the non-comment, non-empty lines of a fragment.
func directives(content string) []string {
	var lines []string
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
