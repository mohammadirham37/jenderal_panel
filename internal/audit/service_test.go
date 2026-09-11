package audit

import (
	"testing"
)

func TestNormalizeDateBound(t *testing.T) {
	// Date-only start becomes midnight UTC.
	got, ok := normalizeDateBound("2026-09-11", false)
	if !ok || got != "2026-09-11T00:00:00Z" {
		t.Fatalf("start = %q ok=%v, want 2026-09-11T00:00:00Z", got, ok)
	}

	// Date-only end covers the whole day.
	got, ok = normalizeDateBound("2026-09-11", true)
	if !ok || got != "2026-09-11T23:59:59Z" {
		t.Fatalf("end = %q ok=%v, want 2026-09-11T23:59:59Z", got, ok)
	}

	// RFC3339 passes through (normalized to UTC).
	got, ok = normalizeDateBound("2026-09-11T10:30:00+07:00", false)
	if !ok || got != "2026-09-11T03:30:00Z" {
		t.Fatalf("rfc3339 = %q ok=%v, want 2026-09-11T03:30:00Z", got, ok)
	}

	if _, ok := normalizeDateBound("not-a-date", false); ok {
		t.Error("unparsable value must not be ok")
	}
	if _, ok := normalizeDateBound("", false); ok {
		t.Error("empty value must not be ok")
	}
}
