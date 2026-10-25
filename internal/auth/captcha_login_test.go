package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
)

type captchaFixture struct {
	t *testing.T
	h *Handler
}

func newCaptchaFixture(t *testing.T) captchaFixture {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	if err := rbac.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123"); err != nil {
		t.Fatal(err)
	}
	return captchaFixture{t: t, h: NewHandler(svc, rbac, audit.NewService(db))}
}

func (f captchaFixture) login(remote string, payload map[string]string) (int, string) {
	f.t.Helper()
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	f.h.Login(rec, req)
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp.Error.Code
}

func (f captchaFixture) captcha(remote, username string) (bool, string) {
	f.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/captcha?username="+username, nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	f.h.Captcha(rec, req)
	var resp struct {
		Data struct {
			Required bool `json:"required"`
			Captcha  struct {
				ID string `json:"id"`
			} `json:"captcha"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		f.t.Fatalf("captcha response: %v: %s", err, rec.Body.String())
	}
	return resp.Data.Required, resp.Data.Captcha.ID
}

func (f captchaFixture) answer(id string) string {
	return f.h.captchas.AnswerForTest(id)
}

func TestLoginNeedsNoCaptchaUntilRepeatedFailures(t *testing.T) {
	f := newCaptchaFixture(t)
	const ip = "203.0.113.5:4000"
	if required, _ := f.captcha(ip, "admin"); required {
		t.Fatal("a fresh client must not get a captcha")
	}
	for i := 0; i < captchaAfterFailures; i++ {
		if code, _ := f.login(ip, map[string]string{"username": "admin", "password": "wrong"}); code != http.StatusUnauthorized {
			t.Fatalf("failure %d: status %d", i, code)
		}
	}
	// The correct password alone is now refused until the captcha is solved.
	if code, errCode := f.login(ip, map[string]string{"username": "admin", "password": "password123"}); code != http.StatusBadRequest || errCode != "CAPTCHA_REQUIRED" {
		t.Fatalf("expected CAPTCHA_REQUIRED, got %d %s", code, errCode)
	}
	required, id := f.captcha(ip, "admin")
	if !required || id == "" {
		t.Fatal("captcha endpoint must issue a challenge now")
	}
	if code, errCode := f.login(ip, map[string]string{"username": "admin", "password": "password123", "captcha_id": id, "captcha_answer": "XXXXX"}); errCode != "CAPTCHA_INVALID" {
		t.Fatalf("wrong answer: %d %s", code, errCode)
	}
	_, id = f.captcha(ip, "admin")
	if code, _ := f.login(ip, map[string]string{"username": "admin", "password": "password123", "captcha_id": id, "captcha_answer": f.answer(id)}); code != http.StatusOK {
		t.Fatalf("solved captcha + right password: status %d", code)
	}
	// A successful login clears the counters.
	if required, _ := f.captcha(ip, "admin"); required {
		t.Error("captcha still required after a successful login")
	}
}

// Rotating addresses does not avoid the captcha: failures against one
// account count from anywhere.
func TestCaptchaFollowsTheAccountAcrossAddresses(t *testing.T) {
	f := newCaptchaFixture(t)
	f.login("198.51.100.1:1", map[string]string{"username": "admin", "password": "wrong"})
	f.login("198.51.100.2:1", map[string]string{"username": "admin", "password": "wrong"})
	if code, errCode := f.login("198.51.100.3:1", map[string]string{"username": "admin", "password": "password123"}); errCode != "CAPTCHA_REQUIRED" {
		t.Fatalf("expected CAPTCHA_REQUIRED from a new address, got %d %s", code, errCode)
	}
	// Another account from a clean address is unaffected.
	if required, _ := f.captcha("198.51.100.9:1", "someone-else"); required {
		t.Error("captcha leaked to an unrelated account")
	}
}
