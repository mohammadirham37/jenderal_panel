package auth

import (
	"context"
	"database/sql"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
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

func TestUserRoleHasDatabaseUsersPermission(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user, err := svc.CreateUser(ctx, "dbowner", "dbowner@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := rbac.AssignRole(ctx, user.ID, "user"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	has, err := rbac.HasPermission(ctx, user.ID, "databases.users")
	if err != nil {
		t.Fatalf("has permission: %v", err)
	}
	if !has {
		t.Error("expected user role to have databases.users permission")
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

	// The admin role must hold every seeded permission; deriving the count
	// from the table keeps this test stable as permissions are added.
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM permissions`).Scan(&seeded); err != nil {
		t.Fatalf("count permissions: %v", err)
	}
	if len(perms) != seeded {
		t.Errorf("expected %d permissions for admin (all seeded), got %d", seeded, len(perms))
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
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM permissions`).Scan(&count)
	if err != nil {
		t.Fatalf("count permissions: %v", err)
	}

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	// Verify the second seed adds nothing: the permission count is stable
	// no matter how many permissions new panel versions introduce.
	var countAfterSecond int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM permissions`).Scan(&countAfterSecond)
	if err != nil {
		t.Fatalf("count permissions after second seed: %v", err)
	}
	if countAfterSecond != count {
		t.Errorf("expected %d permissions after double seed, got %d", count, countAfterSecond)
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

func TestSecurityPermissionsSeededForAdminOnly(t *testing.T) {
	db := setupTestDB(t)
	rbac := NewRBAC(db)
	if err := rbac.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRolePermission(t, db, "admin", "security.manage", true)
	assertRolePermission(t, db, "admin", "security.quarantine", true)
	// Server-wide modules stay admin-only; the declarative user role set
	// revokes anything the older seed used to grant.
	assertRolePermission(t, db, "user", "security.view", false)
	assertRolePermission(t, db, "user", "security.manage", false)
	assertRolePermission(t, db, "user", "ssh.manage", false)
}

func assertRolePermission(t *testing.T, db interface {
	QueryRow(string, ...any) *sql.Row
}, role, permission string, want bool) {
	t.Helper()
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE r.name = ? AND p.name = ?`, role, permission).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if got := count > 0; got != want {
		t.Fatalf("role %q permission %q = %v, want %v", role, permission, got, want)
	}
}

func TestUserRoleSyncGrantsAndRevokes(t *testing.T) {
	db := setupTestDB(t)
	rbac := NewRBAC(db)
	ctx := context.Background()

	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The seed's declarative set grants websites.create...
	has, err := rbac.RoleHasPermission(ctx, "user", "websites.create")
	if err != nil {
		t.Fatalf("role permission check: %v", err)
	}
	if !has {
		t.Error("user role should have websites.create after seed sync")
	}

	// ...and revokes server-wide permissions the old seed used to grant.
	has, err = rbac.RoleHasPermission(ctx, "user", "nginx.view")
	if err != nil {
		t.Fatalf("role permission check: %v", err)
	}
	if has {
		t.Error("user role should no longer have nginx.view after seed sync")
	}

	// An explicit sync converges the set to exactly what was asked.
	if err := rbac.SyncRolePermissions(ctx, "user", []string{"dashboard.view"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	has, err = rbac.RoleHasPermission(ctx, "user", "dashboard.view")
	if err != nil {
		t.Fatalf("role permission check: %v", err)
	}
	if !has {
		t.Error("user role should keep dashboard.view after custom sync")
	}
	has, err = rbac.RoleHasPermission(ctx, "user", "websites.create")
	if err != nil {
		t.Fatalf("role permission check: %v", err)
	}
	if has {
		t.Error("user role should lose websites.create after custom sync")
	}

	if err := rbac.SyncRolePermissions(ctx, "no-such-role", []string{"dashboard.view"}); err != model.ErrNotFound {
		t.Fatalf("syncing a missing role should return ErrNotFound, got %v", err)
	}
}
