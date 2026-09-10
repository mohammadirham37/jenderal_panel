package terminal

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestCommandLineWrapsCommandForEval(t *testing.T) {
	command := "cd app && ls -la; echo \"done\""
	line := commandLine(command)

	// Must be exactly one stdin line.
	if strings.Contains(strings.TrimSuffix(line, "\n"), "\n") {
		t.Fatalf("command line spans multiple lines: %q", line)
	}

	// The command must travel base64-encoded so quotes/newlines survive.
	start := strings.Index(line, "printf %s '")
	if start < 0 {
		t.Fatalf("no base64 payload in line: %q", line)
	}
	start += len("printf %s '")
	end := strings.Index(line[start:], "'")
	if end < 0 {
		t.Fatalf("unterminated base64 payload: %q", line)
	}
	decoded, err := base64.StdEncoding.DecodeString(line[start : start+end])
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	if string(decoded) != command {
		t.Fatalf("decoded command = %q, want %q", string(decoded), command)
	}

	// A completion marker must be emitted after the command.
	if !strings.Contains(line, "\\001") {
		t.Fatalf("no marker printf in line: %q", line)
	}
}

func TestOutputParserSingleCommand(t *testing.T) {
	p := newTestParser()
	p.write([]byte("hello\n"))
	p.write([]byte(marker(0, "/tmp")))

	p.expectChunks(t, "hello\n")
	p.expectComplete(t, 0, "/tmp")
	p.expectNoMore(t)
}

func TestOutputParserMarkerSplitAcrossWrites(t *testing.T) {
	p := newTestParser()
	m := marker(3, "/var/www")
	p.write([]byte("out " + m[:4]))
	p.write([]byte(m[4:]))

	p.expectChunks(t, "out ")
	p.expectComplete(t, 3, "/var/www")
	p.expectNoMore(t)
}

func TestOutputParserStreamsOutputBeforeMarker(t *testing.T) {
	p := newTestParser()
	p.write([]byte(strings.Repeat("x", 500)))

	// Everything except the holdback tail must already have been emitted.
	p.expectChunks(t, strings.Repeat("x", 500-markerHoldback))
	p.expectNoMore(t)

	p.write([]byte(marker(0, "/")))
	p.expectChunks(t, strings.Repeat("x", markerHoldback))
	p.expectComplete(t, 0, "/")
	p.expectNoMore(t)
}

func TestOutputParserStrayMarkerByteInOutput(t *testing.T) {
	p := newTestParser()
	p.write([]byte("bin\x01junk\x01ary\n"))
	p.write([]byte(marker(0, "/root")))

	// Stray SOH bytes may split the stream into several chunks, but the
	// concatenated output must be passed through untouched.
	joined := strings.Join(p.chunks, "")
	if joined != "bin\x01junk\x01ary\n" {
		t.Fatalf("joined chunks = %q, want %q", joined, "bin\x01junk\x01ary\n")
	}
	p.chunks = nil
	p.expectComplete(t, 0, "/root")
	p.expectNoMore(t)
}

func TestOutputParserMultipleQueuedCommands(t *testing.T) {
	p := newTestParser()
	p.write([]byte(
		"first\n" + marker(0, "/a") +
			"second\n" + marker(2, "/a/b")))

	p.expectChunks(t, "first\n", "second\n")
	p.expectComplete(t, 0, "/a")
	p.expectComplete(t, 2, "/a/b")
	p.expectNoMore(t)
}

func TestParseMarker(t *testing.T) {
	if exit, cwd, ok := parseMarker("0:/tmp"); !ok || exit != 0 || cwd != "/tmp" {
		t.Errorf("parseMarker(0:/tmp) = %d, %q, %v", exit, cwd, ok)
	}
	// Colon inside the path must not break parsing.
	if exit, cwd, ok := parseMarker("127:/a:b"); !ok || exit != 127 || cwd != "/a:b" {
		t.Errorf("parseMarker(127:/a:b) = %d, %q, %v", exit, cwd, ok)
	}
	if _, _, ok := parseMarker("abc:/tmp"); ok {
		t.Error("non-numeric exit code must not parse")
	}
	if _, _, ok := parseMarker(""); ok {
		t.Error("empty body must not parse")
	}
}

// TestPersistentShellRoundTrip runs the real protocol against a real bash:
// commands wrapped by commandLine must execute, the working directory must
// persist across commands, and completions must report exit code and cwd.
func TestPersistentShellRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	e := executor.NewExecutor(5 * time.Second)
	session, err := e.StartSession(context.Background(), "bash", "--norc")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer session.Stop()

	chunks := make(chan string, 64)
	completions := make(chan struct {
		exit int
		cwd  string
	}, 16)
	parser := &outputParser{
		onChunk:    func(chunk string) { chunks <- chunk },
		onComplete: func(exit int, cwd string) { completions <- struct { exit int; cwd string }{exit, cwd} },
	}
	var wg sync.WaitGroup
	wg.Add(2)
	for _, rd := range []io.Reader{session.Stdout(), session.Stderr()} {
		go func(rd io.Reader) {
			defer wg.Done()
			_, _ = io.Copy(parser, rd)
		}(rd)
	}

	expectCompletion := func(wantExit int, wantCwd string) {
		t.Helper()
		select {
		case got := <-completions:
			if got.exit != wantExit || got.cwd != wantCwd {
				t.Fatalf("completion = (exit=%d, cwd=%q), want (exit=%d, cwd=%q)", got.exit, got.cwd, wantExit, wantCwd)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for completion (exit=%d, cwd=%q)", wantExit, wantCwd)
		}
	}

	drainChunks := func() string {
		out := ""
		for {
			select {
			case chunk := <-chunks:
				out += chunk
			case <-time.After(200 * time.Millisecond):
				return out
			}
		}
	}

	if _, err := session.Stdin().Write([]byte(commandLine("cd /tmp"))); err != nil {
		t.Fatalf("write cd: %v", err)
	}
	expectCompletion(0, "/tmp")

	if _, err := session.Stdin().Write([]byte(commandLine("pwd"))); err != nil {
		t.Fatalf("write pwd: %v", err)
	}
	expectCompletion(0, "/tmp")
	if out := drainChunks(); strings.TrimSpace(out) != "/tmp" {
		t.Fatalf("pwd output = %q, want /tmp", out)
	}

	// Non-zero exit codes travel in the marker.
	if _, err := session.Stdin().Write([]byte(commandLine("false"))); err != nil {
		t.Fatalf("write false: %v", err)
	}
	expectCompletion(1, "/tmp")

	// Multi-line commands survive the base64 wrapping.
	if _, err := session.Stdin().Write([]byte(commandLine("printf 'a\nb\n'"))); err != nil {
		t.Fatalf("write multi-line: %v", err)
	}
	expectCompletion(0, "/tmp")
	if out := drainChunks(); out != "a\nb\n" {
		t.Fatalf("multi-line output = %q, want %q", out, "a\nb\n")
	}
}

// ─── test helpers ─────────────────────────────────────────────────

func marker(exit int, cwd string) string {
	return fmt.Sprintf("\x01%d:%s\x01", exit, cwd)
}

type parserRecorder struct {
	chunks    []string
	completions []struct {
		exit int
		cwd  string
	}
	p *outputParser
}

func newTestParser() *parserRecorder {
	r := &parserRecorder{}
	r.p = &outputParser{
		onChunk: func(chunk string) { r.chunks = append(r.chunks, chunk) },
		onComplete: func(exit int, cwd string) {
			r.completions = append(r.completions, struct {
				exit int
				cwd  string
			}{exit, cwd})
		},
	}
	return r
}

func (r *parserRecorder) write(b []byte) {
	if _, err := r.p.Write(b); err != nil {
		panic(err)
	}
}

func (r *parserRecorder) expectChunks(t *testing.T, want ...string) {
	if len(r.chunks) != len(want) {
		t.Fatalf("chunks = %#v, want %#v", r.chunks, want)
	}
	for i := range want {
		if r.chunks[i] != want[i] {
			t.Fatalf("chunk %d = %q, want %q (all: %#v)", i, r.chunks[i], want[i], r.chunks)
		}
	}
	r.chunks = nil
}

func (r *parserRecorder) expectComplete(t *testing.T, exit int, cwd string) {
	if len(r.completions) == 0 {
		t.Fatalf("expected completion (exit=%d, cwd=%q), got none", exit, cwd)
	}
	got := r.completions[0]
	r.completions = r.completions[1:]
	if got.exit != exit || got.cwd != cwd {
		t.Fatalf("completion = (exit=%d, cwd=%q), want (exit=%d, cwd=%q)", got.exit, got.cwd, exit, cwd)
	}
}

func (r *parserRecorder) expectNoMore(t *testing.T) {
	if len(r.chunks) != 0 {
		t.Fatalf("unexpected chunks: %#v", r.chunks)
	}
	if len(r.completions) != 0 {
		t.Fatalf("unexpected completions: %#v", r.completions)
	}
}
