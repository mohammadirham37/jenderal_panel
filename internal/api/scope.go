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

		pattern := chi.RouteContext(ctx).RoutePattern()
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
		case pattern == "/deployments/{id}":
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
			// Website-scoped sessions require owning the website; the
			// unscoped root shell is admin-only.
			websiteID := r.URL.Query().Get("website_id")
			webUser := r.URL.Query().Get("web_user")
			if websiteID == "" && webUser != "" {
				website, err := d.WebsiteSvc.GetByWebUser(ctx, webUser)
				if err != nil {
					httputil.HandleError(w, err)
					return
				}
				websiteID = website.ID
			}
			if websiteID == "" {
				if !admin {
					httputil.HandleError(w, model.ErrForbidden)
					return
				}
			} else if !d.canManageWebsite(ctx, admin, user.ID, websiteID) {
				httputil.HandleError(w, model.ErrForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
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
