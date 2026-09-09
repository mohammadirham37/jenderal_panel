package trafficguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFailedCloudflareRefreshKeepsLastSnapshot(t *testing.T) {
	repo := testRepo(t)
	now := time.Now().UTC()
	before := ProxySnapshot{IPv4: []string{"203.0.113.0/24"}, IPv6: []string{"2001:db8::/32"}, FetchedAt: now.Add(-time.Hour)}
	if err := repo.SaveSnapshot(context.Background(), before); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusServiceUnavailable) }))
	defer server.Close()
	u := NewCloudflareUpdater(repo, server.Client(), nil)
	u.v4URL, u.v6URL = server.URL+"/v4", server.URL+"/v6"
	if err := u.Refresh(context.Background(), now); err == nil {
		t.Fatal("expected refresh error")
	}
	after, err := repo.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.IPv4) != 1 || after.IPv4[0] != before.IPv4[0] || len(after.IPv6) != 1 || after.IPv6[0] != before.IPv6[0] || !after.FetchedAt.Equal(before.FetchedAt) {
		t.Fatalf("snapshot changed: %#v", after)
	}
}

func TestCloudflareRefreshRequiresHTTPSAndBothAddressFamilies(t *testing.T) {
	repo := testRepo(t)
	u := NewCloudflareUpdater(repo, http.DefaultClient, nil)
	u.v4URL, u.v6URL = "http://example.invalid/v4", "http://example.invalid/v6"
	if err := u.Refresh(context.Background(), time.Now()); err == nil {
		t.Fatal("accepted non-HTTPS endpoint")
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4" {
			_, _ = w.Write([]byte("203.0.113.0/24\n"))
			return
		}
		_, _ = w.Write([]byte("not-an-ip\n"))
	}))
	defer server.Close()
	u = NewCloudflareUpdater(repo, server.Client(), nil)
	u.v4URL, u.v6URL = server.URL+"/v4", server.URL+"/v6"
	if err := u.Refresh(context.Background(), time.Now()); err == nil {
		t.Fatal("accepted invalid IPv6 response")
	}
}
