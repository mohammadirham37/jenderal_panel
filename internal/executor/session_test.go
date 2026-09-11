package executor

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestStartSessionRoundTrip(t *testing.T) {
	e := NewExecutor(5 * time.Second)
	session, err := e.StartSession(context.Background(), "cat")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer session.Stop()

	if _, err := session.Stdin().Write([]byte("hello session")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	type readResult struct {
		n   int
		err error
	}
	got := make(chan readResult, 1)
	buf := make([]byte, 13)
	go func() {
		n, err := io.ReadFull(session.Stdout(), buf)
		got <- readResult{n, err}
	}()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("read stdout: %v", r.err)
		}
		if string(buf) != "hello session" {
			t.Fatalf("stdout = %q, want %q", string(buf), "hello session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading from session stdout")
	}
}

func TestRunStreamCapturesOutput(t *testing.T) {
	e := NewExecutor(5 * time.Second)
	var buf bytes.Buffer
	if _, err := e.RunStream(context.Background(), &buf, "sh", "-c", "echo hello-stream"); err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	if buf.String() != "hello-stream\n" {
		t.Fatalf("streamed output = %q", buf.String())
	}

	// A failing command surfaces its exit code.
	code, err := e.RunStream(context.Background(), &buf, "sh", "-c", "exit 3")
	if err != nil {
		t.Fatalf("RunStream error: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
}

func TestSessionStopTerminatesProcess(t *testing.T) {
	e := NewExecutor(5 * time.Second)
	session, err := e.StartSession(context.Background(), "sleep", "60")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if err := session.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-session.Done():
		// expected: process exited
	case <-time.After(5 * time.Second):
		t.Fatal("session still running after Stop")
	}
}
