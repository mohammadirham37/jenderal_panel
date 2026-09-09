package trafficguard

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/security"
)

type Service struct {
	db      *sql.DB
	repo    *Repository
	nginx   *NginxManager
	updater *CloudflareUpdater
	now     func() time.Time
}

func NewService(db *sql.DB, repo *Repository, nginx *NginxManager, updater *CloudflareUpdater) *Service {
	return &Service{db: db, repo: repo, nginx: nginx, updater: updater, now: func() time.Time { return time.Now().UTC() }}
}
func (s *Service) Name() string { return "traffic_guard" }
func (s *Service) Check(ctx context.Context) security.ComponentStatus {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM traffic_guard_profiles`).Scan(&count)
	if err != nil {
		return security.ComponentStatus{Name: s.Name(), State: "error", Message: err.Error(), CheckedAt: s.now()}
	}
	state := "not_configured"
	if count > 0 {
		state = "observing"
	}
	return security.ComponentStatus{Name: s.Name(), State: state, Installed: true, Enabled: count > 0, Healthy: true, Message: "HTTP-layer traffic observation and rate-limit controls.", CheckedAt: s.now()}
}
func (s *Service) Profiles(ctx context.Context) ([]WebsiteProfile, error) {
	return s.repo.Profiles(ctx, s.now())
}
func (s *Service) Buckets(ctx context.Context, id string) ([]MinuteBucket, error) {
	return s.repo.ListBuckets(ctx, id, s.now().Add(-24*time.Hour))
}
func (s *Service) Apply(ctx context.Context, id string, p WebsiteProfile, confirm bool) error {
	exists, err := s.websiteExists(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return model.ErrNotFound
	}
	if err := s.repo.EnsureProfile(ctx, id, s.now()); err != nil {
		return err
	}
	current, err := s.repo.Profile(ctx, id, s.now())
	if err != nil {
		return err
	}
	p.WebsiteID = id
	if p.ObserveStartedAt.IsZero() {
		p.ObserveStartedAt = current.ObserveStartedAt
	}
	return s.nginx.ApplyWebsite(ctx, model.Website{ID: id}, p, confirm)
}
func (s *Service) ResetObserve(ctx context.Context, id string) error {
	p, err := s.repo.Profile(ctx, id, s.now())
	if err != nil {
		return err
	}
	p.Mode = "observe"
	p.ObserveStartedAt = s.now()
	return s.Apply(ctx, id, p, false)
}
func (s *Service) RefreshCloudflare(ctx context.Context) error {
	if s.updater == nil {
		return errors.New("Cloudflare updater is unavailable")
	}
	return s.updater.Refresh(ctx, s.now())
}
func (s *Service) websiteExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM websites WHERE id=? AND status IN ('active','suspended')`, id).Scan(&n)
	return n == 1, err
}
