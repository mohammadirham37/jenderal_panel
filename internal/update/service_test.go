package update

import (
	"testing"
)

func TestNewService(t *testing.T) {
	svc := NewService(nil, "0.1.0", nil)
	if svc.currentVer != "0.1.0" {
		t.Errorf("currentVer = %q, want 0.1.0", svc.currentVer)
	}
}
