package terminal

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
)

// The persistent shell executes every command wrapped in a single stdin line
// and finishes it with a printf that emits a completion marker carrying the
// exit code and the new working directory. The marker uses SOH (0x01) bytes,
// which never appear in ordinary command output, so the output parser can
// detect command completion in the byte stream.

// markerHoldback is the number of trailing bytes withheld while streaming
// output, so a completion marker split across pipe reads is never forwarded
// to the client as output.
const markerHoldback = 64

// commandLine renders one shell stdin line that executes command via eval
// and emits the completion marker. The command travels base64-encoded so
// quotes, dollar signs, and newlines survive the round trip; </dev/null
// keeps commands from consuming the shell's stdin.
func commandLine(command string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	return "eval \"$(printf %s '" + encoded + "' | base64 -d)\" </dev/null; " +
		"__panel_ec=$?; printf '\\001%s:%s\\001' \"$__panel_ec\" \"$PWD\"\n"
}

// cdLine renders a raw stdin line changing into dir, falling back to the
// home directory when dir does not exist. Used once when a scoped session
// starts. dir must be single-quote-escaped by quoteShellArg.
func cdLine(dir string) string {
	return "cd -- " + quoteShellArg(dir) + " 2>/dev/null || cd --\n"
}

// quoteShellArg single-quote-escapes a value for safe use in a shell line.
func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// outputParser assembles streamed shell output into websocket messages.
// Complete output chunks are forwarded as they arrive; on a completion
// marker it reports the exit code and working directory.
type outputParser struct {
	buf        []byte
	onChunk    func(chunk string)
	onComplete func(exitCode int, cwd string)
}

// Write consumes a chunk of shell output. It satisfies io.Writer so it can
// be fed directly from pipe readers.
func (p *outputParser) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	p.parse()
	return len(b), nil
}

func (p *outputParser) parse() {
	for {
		i := bytes.IndexByte(p.buf, 0x01)
		if i < 0 {
			p.emitStreaming()
			return
		}

		// Candidate marker starting at i: look for the closing SOH byte.
		j := bytes.IndexByte(p.buf[i+1:], 0x01)
		if j < 0 {
			// Marker may still be incomplete; everything before it is safe
			// to emit as output.
			p.emitBefore(i)
			return
		}
		end := i + 1 + j
		exit, cwd, ok := parseMarker(string(p.buf[i+1 : end]))
		if !ok {
			// Stray SOH byte in the output: pass it through untouched and
			// keep scanning after it.
			p.emitBefore(i + 1)
			continue
		}
		// Slice the buffer before emitting: emitBefore mutates p.buf and
		// would invalidate end.
		output := p.buf[:i]
		p.buf = p.buf[end+1:]
		if len(output) > 0 {
			p.onChunk(string(output))
		}
		p.onComplete(exit, cwd)
	}
}

// emitStreaming forwards output as long as a holdback tail is kept back.
func (p *outputParser) emitStreaming() {
	if len(p.buf) > markerHoldback {
		chunk := string(p.buf[:len(p.buf)-markerHoldback])
		p.buf = p.buf[len(p.buf)-markerHoldback:]
		p.onChunk(chunk)
	}
}

func (p *outputParser) emitBefore(n int) {
	if n > 0 {
		p.onChunk(string(p.buf[:n]))
		p.buf = p.buf[n:]
	}
}

// parseMarker parses "<exit>:<cwd>". The path may itself contain colons, so
// only the first colon separates the fields.
func parseMarker(body string) (exitCode int, cwd string, ok bool) {
	colon := -1
	for i := 0; i < len(body); i++ {
		if body[i] == ':' {
			colon = i
			break
		}
	}
	if colon <= 0 {
		return 0, "", false
	}
	exit, err := strconv.Atoi(body[:colon])
	if err != nil {
		return 0, "", false
	}
	return exit, body[colon+1:], true
}
