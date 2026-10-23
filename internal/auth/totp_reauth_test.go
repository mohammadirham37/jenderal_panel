package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func totpHandlerFixture(t *testing.T) (*Handler, *Service, model.User, context.Context) {
	t.Helper()
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	h := NewHandler(svc, NewRBAC(db), audit.NewService(db))
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "alice", "alice@test.com", "Correct-Horse-1")
	if err != nil {
		t.Fatal(err)
	}
	return h, svc, user, ctx
}

func totpRequest(t *testing.T, user model.User, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp", strings.NewReader(body))
	return req.WithContext(WithUserContext(req.Context(), user))
}

// Turning the second factor off, or re-enrolling (which silently disables
// it), must prove the account password first.
func TestTOTPDisableAndSetupRequirePassword(t *testing.T) {
	h, svc, user, ctx := totpHandlerFixture(t)
	secret, _, err := svc.TOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCodeCustom(secret, time.Now().UTC(), totp.ValidateOpts{
		Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.TOTPEnable(ctx, user.ID, code); err != nil {
		t.Fatal(err)
	}

	for name, handler := range map[string]http.HandlerFunc{
		"disable": h.TOTPDisable,
		"setup":   h.TOTPSetup,
	} {
		for _, body := range []string{`{}`, `{"password":"wrong"}`} {
			rec := httptest.NewRecorder()
			handler(rec, totpRequest(t, user, body))
			if rec.Code == http.StatusOK {
				t.Errorf("%s with body %s succeeded without the correct password", name, body)
			}
		}
		enabled, err := svc.IsTOTPEnabled(ctx, user.ID)
		if err != nil || !enabled {
			t.Fatalf("%s left TOTP disabled: enabled=%v err=%v", name, enabled, err)
		}
	}

	// With the password, disabling works.
	rec := httptest.NewRecorder()
	h.TOTPDisable(rec, totpRequest(t, user, `{"password":"Correct-Horse-1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable with the right password = %d: %s", rec.Code, rec.Body.String())
	}
	if enabled, _ := svc.IsTOTPEnabled(ctx, user.ID); enabled {
		t.Error("TOTP is still enabled after a confirmed disable")
	}
}

// First-time enrolment stays a one-step flow: no password is required while
// TOTP is off.
func TestTOTPSetupWithoutExistingSecretNeedsNoPassword(t *testing.T) {
	h, _, user, _ := totpHandlerFixture(t)
	rec := httptest.NewRecorder()
	h.TOTPSetup(rec, totpRequest(t, user, `{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("initial setup = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Data.Secret == "" {
		t.Fatalf("setup response = %s (%v)", rec.Body.String(), err)
	}
}
