package auth

import (
	"context"
)

// adminKey is the context key for the per-request admin flag resolved by the
// API's scope middleware.
type adminKey struct{}

// WithAdminFlag records whether the authenticated user has the admin role.
func WithAdminFlag(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, adminKey{}, admin)
}

// AdminFromContext reports whether the request was flagged as admin by the
// scope middleware. Absent flag means not admin.
func AdminFromContext(ctx context.Context) bool {
	admin, _ := ctx.Value(adminKey{}).(bool)
	return admin
}

// CanManageResource reports whether a user may act on a resource created by
// createdBy: admins act on everything, users only on resources they created
// themselves. Resources with an empty creator are legacy/admin-owned.
func CanManageResource(isAdmin bool, userID, createdBy string) bool {
	if isAdmin {
		return true
	}
	return createdBy != "" && createdBy == userID
}

type terminalUserKey struct{}

// WithTerminalUser records the account a terminal session may run as, as
// resolved by the scope middleware from the website record: a website's
// web user, or "" for the admin-only root shell.
func WithTerminalUser(ctx context.Context, webUser string) context.Context {
	return context.WithValue(ctx, terminalUserKey{}, webUser)
}

// TerminalUserFromContext returns the resolved terminal account; ok is false
// when the scope middleware did not authorize a terminal session.
func TerminalUserFromContext(ctx context.Context) (webUser string, ok bool) {
	webUser, ok = ctx.Value(terminalUserKey{}).(string)
	return webUser, ok
}
