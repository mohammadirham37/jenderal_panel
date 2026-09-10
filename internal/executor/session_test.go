package executor

import (
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
