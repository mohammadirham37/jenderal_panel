package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestRequireRoleAllowsOnlyThatRole(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()
	if err := rbac.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := svc.CreateUser(ctx, "boss", "boss@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := rbac.AssignRole(ctx, admin.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateUser(ctx, "member", "member@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := rbac.AssignRole(ctx, member.ID, "user"); err != nil {
		t.Fatal(err)
	}

	handler := RequireRole(rbac, "admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		user *model.User
		want int
	}{
		{&admin, http.StatusNoContent},
		{&member, http.StatusForbidden},
		{nil, http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		if tc.user != nil {
			req = req.WithContext(WithUserContext(req.Context(), *tc.user))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("user %v: status %d, want %d", tc.user, rec.Code, tc.want)
		}
	}
}
