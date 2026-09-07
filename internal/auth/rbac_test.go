package auth

import (
	"context"
	"testing"
)

func TestSeedAndAdminHasPermission(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := rbac.AssignRole(ctx, user.ID, "admin"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	has, err := rbac.HasPermission(ctx, user.ID, "server.reboot")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if !has {
		t.Error("expected admin to have server.reboot permission")
	}

	has, err = rbac.HasPermission(ctx, user.ID, "dashboard.view")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if !has {
		t.Error("expected admin to have dashboard.view permission")
	}
}

func TestUserRoleLacksServerReboot(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user, err := svc.CreateUser(ctx, "regular", "regular@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := rbac.AssignRole(ctx, user.ID, "user"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	has, err := rbac.HasPermission(ctx, user.ID, "server.reboot")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if has {
		t.Error("expected user role to NOT have server.reboot permission")
	}

	has, err = rbac.HasPermission(ctx, user.ID, "dashboard.view")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if !has {
		t.Error("expected user role to have dashboard.view permission")
	}
}

func TestGetUserPermissions(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := rbac.AssignRole(ctx, user.ID, "admin"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	perms, err := rbac.GetUserPermissions(ctx, user.ID)
	if err != nil {
		t.Fatalf("get permissions: %v", err)
	}

	if len(perms) != 46 {
		t.Errorf("expected 46 permissions for admin, got %d", len(perms))
	}
}

func TestGetUserRoles(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := rbac.AssignRole(ctx, user.ID, "admin"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	roles, err := rbac.GetUserRoles(ctx, user.ID)
	if err != nil {
		t.Fatalf("get roles: %v", err)
	}

	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}
	if roles[0].Name != "admin" {
		t.Errorf("expected role name admin, got %s", roles[0].Name)
	}
}

func TestSeedIdempotent(t *testing.T) {
	db := setupTestDB(t)
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	// Verify still correct number of permissions
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM permissions`).Scan(&count)
	if err != nil {
		t.Fatalf("count permissions: %v", err)
	}
	if count != 46 {
		t.Errorf("expected 46 permissions after double seed, got %d", count)
	}

	var roleCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles`).Scan(&roleCount)
	if err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if roleCount != 2 {
		t.Errorf("expected 2 roles after double seed, got %d", roleCount)
	}
}
