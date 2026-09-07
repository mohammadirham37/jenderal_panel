package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type RBAC struct {
	db *sql.DB
}

func NewRBAC(db *sql.DB) *RBAC {
	return &RBAC{db: db}
}

// Seed creates default roles and permissions and assigns them.
func (r *RBAC) Seed(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Create roles
	roles := []struct {
		name string
		desc string
	}{
		{"admin", "Administrator with full access"},
		{"user", "Regular user with limited access"},
	}

	roleIDs := make(map[string]string)
	for _, role := range roles {
		id := ulid.Make().String()
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO roles (id, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
			id, role.name, role.desc, now, now,
		)
		if err != nil {
			return fmt.Errorf("insert role %s: %w", role.name, err)
		}
		// Fetch the actual ID (might already exist)
		var rid string
		err = r.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, role.name).Scan(&rid)
		if err != nil {
			return fmt.Errorf("get role id %s: %w", role.name, err)
		}
		roleIDs[role.name] = rid
	}

	// Create permissions
	permissions := []struct {
		name   string
		module string
	}{
		{"dashboard.view", "dashboard"},
		{"server.view", "server"},
		{"server.reboot", "server"},
		{"server.hostname", "server"},
		{"server.timezone", "server"},
		{"services.view", "services"},
		{"services.manage", "services"},
		{"users.view", "users"},
		{"users.create", "users"},
		{"users.update", "users"},
		{"users.delete", "users"},
		{"audit.view", "audit"},
		{"settings.view", "settings"},
		{"settings.update", "settings"},
		{"nginx.view", "nginx"},
		{"nginx.manage", "nginx"},
		{"nginx.config", "nginx"},
		{"firewall.view", "firewall"},
		{"firewall.manage", "firewall"},
		{"processes.view", "processes"},
		{"processes.kill", "processes"},
		{"logs.view", "logs"},
		{"websites.view", "websites"},
		{"websites.create", "websites"},
		{"websites.update", "websites"},
		{"websites.delete", "websites"},
		{"websites.suspend", "websites"},
		{"php.view", "php"},
		{"php.manage", "php"},
		{"php.config", "php"},
		{"ssl.view", "ssl"},
		{"ssl.manage", "ssl"},
		{"deployments.view", "deployments"},
		{"deployments.deploy", "deployments"},
		{"cron.view", "cron"},
		{"cron.manage", "cron"},
		{"queue.view", "queue"},
		{"queue.manage", "queue"},
		{"nodejs.view", "nodejs"},
		{"nodejs.manage", "nodejs"},
		{"databases.view", "databases"},
		{"databases.create", "databases"},
		{"databases.delete", "databases"},
		{"databases.users", "databases"},
	}

	permIDs := make(map[string]string)
	for _, perm := range permissions {
		id := ulid.Make().String()
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO permissions (id, name, module) VALUES (?, ?, ?)`,
			id, perm.name, perm.module,
		)
		if err != nil {
			return fmt.Errorf("insert permission %s: %w", perm.name, err)
		}
		var pid string
		err = r.db.QueryRowContext(ctx, `SELECT id FROM permissions WHERE name = ?`, perm.name).Scan(&pid)
		if err != nil {
			return fmt.Errorf("get permission id %s: %w", perm.name, err)
		}
		permIDs[perm.name] = pid
	}

	// Assign ALL permissions to admin role
	for _, perm := range permissions {
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)`,
			roleIDs["admin"], permIDs[perm.name],
		)
		if err != nil {
			return fmt.Errorf("assign permission %s to admin: %w", perm.name, err)
		}
	}

	// Assign limited permissions to user role
	userPerms := []string{
		"dashboard.view", "server.view", "services.view",
		"nginx.view", "firewall.view", "processes.view", "logs.view",
		"websites.view", "php.view", "ssl.view",
		"deployments.view", "cron.view", "queue.view", "nodejs.view",
		"databases.view",
	}
	for _, name := range userPerms {
		_, err := r.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)`,
			roleIDs["user"], permIDs[name],
		)
		if err != nil {
			return fmt.Errorf("assign permission %s to user: %w", name, err)
		}
	}

	return nil
}

// AssignRole assigns a role to a user by role name.
func (r *RBAC) AssignRole(ctx context.Context, userID, roleName string) error {
	var roleID string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, roleName).Scan(&roleID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("get role: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`,
		userID, roleID,
	)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}

	return nil
}

// HasPermission checks if a user has a specific permission through their roles.
func (r *RBAC) HasPermission(ctx context.Context, userID, permName string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_roles ur
		 JOIN role_permissions rp ON ur.role_id = rp.role_id
		 JOIN permissions p ON rp.permission_id = p.id
		 WHERE ur.user_id = ? AND p.name = ?`,
		userID, permName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check permission: %w", err)
	}

	return count > 0, nil
}

// GetUserPermissions returns all permissions for a user through their roles.
func (r *RBAC) GetUserPermissions(ctx context.Context, userID string) ([]model.Permission, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT p.id, p.name, p.module
		 FROM user_roles ur
		 JOIN role_permissions rp ON ur.role_id = rp.role_id
		 JOIN permissions p ON rp.permission_id = p.id
		 WHERE ur.user_id = ?
		 ORDER BY p.name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var perms []model.Permission
	for rows.Next() {
		var p model.Permission
		if err := rows.Scan(&p.ID, &p.Name, &p.Module); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, rows.Err()
}

// GetUserRoles returns all roles assigned to a user.
func (r *RBAC) GetUserRoles(ctx context.Context, userID string) ([]model.Role, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT r.id, r.name, r.description, r.created_at, r.updated_at
		 FROM user_roles ur
		 JOIN roles r ON ur.role_id = r.id
		 WHERE ur.user_id = ?
		 ORDER BY r.name`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []model.Role
	for rows.Next() {
		var role model.Role
		var createdStr, updatedStr string
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		role.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		role.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		roles = append(roles, role)
	}

	return roles, rows.Err()
}
