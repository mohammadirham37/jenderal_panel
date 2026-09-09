package trafficguard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/security"
)

const (
	defaultCloudflareV4 = "https://www.cloudflare.com/ips-v4"
	defaultCloudflareV6 = "https://www.cloudflare.com/ips-v6"
	maxProxyResponse    = 1 << 20
)

type CloudflareUpdater struct {
	repo         *Repository
	client       *http.Client
	events       *security.EventService
	v4URL, v6URL string
	failures     int
	mu           sync.Mutex
}

func NewCloudflareUpdater(repo *Repository, client *http.Client, events *security.EventService) *CloudflareUpdater {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &CloudflareUpdater{repo: repo, client: client, events: events, v4URL: defaultCloudflareV4, v6URL: defaultCloudflareV6}
}
func (u *CloudflareUpdater) Refresh(ctx context.Context, now time.Time) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	v4, err := u.fetch(ctx, u.v4URL, true)
	if err != nil {
		return u.failed(ctx, now, err)
	}
	v6, err := u.fetch(ctx, u.v6URL, false)
	if err != nil {
		return u.failed(ctx, now, err)
	}
	if err = u.repo.SaveSnapshot(ctx, ProxySnapshot{IPv4: v4, IPv6: v6, FetchedAt: now}); err != nil {
		return err
	}
	u.failures = 0
	return nil
}
func (u *CloudflareUpdater) fetch(ctx context.Context, endpointURL string, want4 bool) ([]string, error) {
	parsed, err := neturl.Parse(endpointURL)
	if err != nil || parsed.Scheme != "https" {
		return nil, fmt.Errorf("Cloudflare CIDR endpoint must use HTTPS")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Cloudflare CIDR endpoint returned %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxProxyResponse+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxProxyResponse {
		return nil, fmt.Errorf("Cloudflare CIDR response is too large")
	}
	var values []string
	for _, line := range strings.Fields(string(data)) {
		p, e := netip.ParsePrefix(line)
		if e != nil || p.Addr().Is4() != want4 {
			return nil, fmt.Errorf("invalid Cloudflare CIDR response")
		}
		values = append(values, p.Masked().String())
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("empty Cloudflare CIDR response")
	}
	return values, nil
}

func (u *CloudflareUpdater) failed(ctx context.Context, now time.Time, cause error) error {
	snapshot, err := u.repo.Snapshot(ctx)
	if err == nil && snapshot.ConsecutiveFailures >= u.failures {
		u.failures = snapshot.ConsecutiveFailures + 1
	} else {
		u.failures++
	}
	if err == nil && len(snapshot.IPv4) > 0 && len(snapshot.IPv6) > 0 {
		snapshot.ConsecutiveFailures = u.failures
		_ = u.repo.SaveSnapshot(ctx, snapshot)
	}
	if u.failures >= 3 && u.events != nil {
		_, _, _ = u.events.Record(ctx, security.EventInput{Fingerprint: "cloudflare-cidr-refresh", Category: "traffic", Severity: security.SeverityMedium, Component: "traffic_guard", Resource: "cloudflare", Evidence: fmt.Sprintf(`{"error":%q}`, cause.Error()), RecommendedAction: "Check outbound HTTPS access; the previous trusted Cloudflare CIDR snapshot remains active."}, now)
	}
	return cause
}
func SnapshotPrefixes(s ProxySnapshot) ([]netip.Prefix, error) {
	return normalizePrefixes(append(append([]string{}, s.IPv4...), s.IPv6...))
}
