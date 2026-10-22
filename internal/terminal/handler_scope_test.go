package terminal

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Without a shell account resolved by the scope middleware the handler must
// refuse before upgrading, whatever the query string says.
func TestHandleWSRefusesWithoutResolvedShellUser(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ws/terminal?web_user=root", nil)
	req = req.WithContext(auth.WithUserContext(req.Context(), model.User{ID: "u1"}))
	rec := httptest.NewRecorder()
	h.HandleWS(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestShellCommandUsesResolvedAccount(t *testing.T) {
	if got := shellCommand("web_site"); len(got) != 5 || got[1] != "-u" || got[2] != "web_site" {
		t.Errorf("website shell = %v", got)
	}
	if got := shellCommand(""); len(got) != 3 || got[1] != "bash" {
		t.Errorf("admin root shell = %v", got)
	}
}
