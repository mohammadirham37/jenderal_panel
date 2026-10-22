package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/httputil"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// ScopeMiddleware resolves the caller's admin flag once per request and
// enforces row-level ownership on resource routes: the admin role acts on
// everything, the user role only on the websites and databases it created.
// It must run after SessionMiddleware.
func (d *Dependencies) ScopeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			httputil.HandleError(w, model.ErrUnauthorized)
			return
		}

		admin, err := d.RBAC.HasRole(r.Context(), user.ID, "admin")
		if err != nil {
			httputil.HandleError(w, model.NewDomainError("SCOPE_CHECK_FAILED",
				"could not resolve user roles", err))
			return
		}
		r = r.WithContext(auth.WithAdminFlag(r.Context(), admin))
		ctx := r.Context()

		// chi reports the full pattern including the mount prefix
		// ("/api/v1/websites/{id}"); match on the resource path so the
		// ownership checks below apply to the API routes.
		pattern := strings.TrimPrefix(chi.RouteContext(ctx).RoutePattern(), "/api/v1")
		switch {
		case pattern == "/websites/{id}" || strings.HasPrefix(pattern, "/websites/{id}/"):
			if !d.canManageWebsite(ctx, admin, user.ID, chi.URLParam(r, "id")) {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}
		case pattern == "/databases/{id}" || strings.HasPrefix(pattern, "/databases/{id}/"):
			if !d.canManageDatabase(ctx, admin, user.ID, chi.URLParam(r, "id")) {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}
		case pattern == "/security/waf/sites/{id}":
			// Per-site WAF/DoS protection is website-scoped: a non-admin may
			// only read and toggle the protections of sites it owns.
			if !d.canManageWebsite(ctx, admin, user.ID, chi.URLParam(r, "id")) {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}
		case pattern == "/deployments/{id}" || strings.HasPrefix(pattern, "/deployments/{id}/"):
			deployment, err := d.DeploymentSvc.GetDeployment(ctx, chi.URLParam(r, "id"))
			if err != nil {
				httputil.HandleError(w, err)
				return
			}
			if !d.canManageWebsite(ctx, admin, user.ID, deployment.WebsiteID) {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}
		case pattern == "/ws/terminal":
			// The shell account is derived from the website record; the
			// client's web_user is only a selector and must match it. The
			// unscoped root shell is admin-only.
			shellUser, err := d.resolveTerminalUser(ctx, admin, user.ID,
				r.URL.Query().Get("website_id"), r.URL.Query().Get("web_user"))
			if err != nil {
				httputil.HandleError(w, err)
				return
			}
			r = r.WithContext(auth.WithTerminalUser(ctx, shellUser))
		}

		next.ServeHTTP(w, r)
	})
}

// resolveTerminalUser decides which account a terminal session runs as.
// A website-scoped session always runs as that website's web user, and only
// for callers who may manage the website; "" (root) is admin-only and only
// when no website was requested.
func (d *Dependencies) resolveTerminalUser(ctx context.Context, admin bool, userID, websiteID, webUser string) (string, error) {
	if websiteID == "" && webUser == "" {
		if !admin {
			return "", model.ErrForbidden
		}
		return "", nil
	}
	var site model.Website
	var err error
	if websiteID != "" {
		site, err = d.WebsiteSvc.Get(ctx, websiteID)
	} else {
		site, err = d.WebsiteSvc.GetByWebUser(ctx, webUser)
	}
	if err != nil || site.WebUser == "" {
		return "", model.ErrForbidden
	}
	if webUser != "" && webUser != site.WebUser {
		return "", model.ErrForbidden
	}
	if !auth.CanManageResource(admin, userID, site.CreatedBy) {
		return "", model.ErrForbidden
	}
	return site.WebUser, nil
}

func (d *Dependencies) canManageWebsite(ctx context.Context, admin bool, userID, websiteID string) bool {
	website, err := d.WebsiteSvc.Get(ctx, websiteID)
	if err != nil {
		return false
	}
	return auth.CanManageResource(admin, userID, website.CreatedBy)
}

func (d *Dependencies) canManageDatabase(ctx context.Context, admin bool, userID, databaseID string) bool {
	database, err := d.DBManagerSvc.GetDatabase(ctx, databaseID)
	if err != nil {
		return false
	}
	return auth.CanManageResource(admin, userID, database.CreatedBy)
}
