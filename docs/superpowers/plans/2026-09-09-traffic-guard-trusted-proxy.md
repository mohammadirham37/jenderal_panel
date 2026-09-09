# Traffic Guard and Trusted Proxy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect HTTP traffic anomalies per website and safely progress from observation to Nginx request/connection limiting with correct client IPs behind trusted proxies.

**Architecture:** Tail existing per-site Nginx logs with persisted inode/offset cursors, aggregate bounded minute/hour buckets, and emit evidence-backed security events. Generate panel-owned Nginx snippets for real-IP and limiting configuration; every candidate is tested, rolled back on failure, and begins in dry-run/Observe Mode.

**Tech Stack:** Go 1.26, SQLite, Nginx real-IP/limit_req/limit_conn modules, existing website/nginx/security/taskrunner packages, Svelte 5, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-09-security-center-design.md`

## Global Constraints

- Complete the foundation and malware plans first.
- Traffic Guard covers HTTP-layer abuse only; UI copy must not promise volumetric DDoS mitigation.
- Observe Mode lasts at least 24 hours and rejects no request.
- Enforcement requires explicit confirmation and returns HTTP 429 for excess requests.
- Safe mode never converts traffic anomalies into automatic UFW bans.
- Forwarded IP headers are accepted only from exact trusted CIDRs.
- A failed Cloudflare CIDR refresh retains the last valid snapshot.
- Nginx candidate configuration must pass `nginx -t`, reload, and health checks or roll back.

---

## File Map

- `internal/database/migrations/024_security_traffic.sql`: profiles, cursors, buckets, and baselines.
- `internal/trafficguard/parser.go`: Nginx combined-log parsing.
- `internal/trafficguard/collector.go`: cursor-based aggregation.
- `internal/trafficguard/baseline.go`: sufficiency and anomaly decisions.
- `internal/trafficguard/proxy.go`: Direct, Cloudflare, and custom proxy validation.
- `internal/trafficguard/nginx.go`: panel snippets and rollback.
- `internal/trafficguard/status.go`: optional local-only Nginx connection metrics.
- `internal/trafficguard/service.go`, `worker.go`, `handler.go`: orchestration and API.
- `internal/website/templates.go`: stable panel security include marker for generated sites.
- `web/src/lib/traffic-guard.js`, `web/src/routes/security/+page.svelte`: UI.

### Task 1: Add traffic persistence and robust log parsing

**Files:**
- Create: `internal/database/migrations/024_security_traffic.sql`
- Create: `internal/trafficguard/models.go`
- Create: `internal/trafficguard/parser.go`
- Create: `internal/trafficguard/parser_test.go`
- Create: `internal/trafficguard/repository.go`
- Create: `internal/trafficguard/repository_test.go`
- Modify: `internal/database/migrations_test.go`

**Interfaces:**
- Produces: `WebsiteProfile`, `LogCursor`, `MinuteBucket`, `HourlyBucket`, and `Baseline`.
- Produces: `ParseCombinedLog(string) (LogEntry, error)` and repository upsert/query/retention methods.

- [ ] **Step 1: Write failing parser and cursor tests**

```go
func TestParseCombinedLogPreservesRequestAndStatus(t *testing.T) {
	line := `203.0.113.7 - - [09/Sep/2026:10:00:01 +0700] "GET /login?q=a HTTP/1.1" 429 123 "-" "Agent/1.0"`
	got, err := ParseCombinedLog(line)
	if err != nil || got.IP.String() != "203.0.113.7" || got.Path != "/login?q=a" || got.Status != 429 { t.Fatalf("entry=%#v err=%v", got, err) }
}

func TestCursorRotationStartsNewInodeAtZero(t *testing.T) {
	repo := newTrafficRepo(t)
	repo.SaveCursor(context.Background(), LogCursor{WebsiteID: "site", Inode: 10, Offset: 1000})
	if got := NextOffset(repo.Cursor("site"), 11, 200); got != 0 { t.Fatalf("offset=%d", got) }
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run: `go test ./internal/trafficguard ./internal/database -run 'ParseCombined|Cursor|Migrate' -count=1`

Expected: FAIL because migration 024 and the package do not exist.

- [ ] **Step 3: Implement additive schema and bounded parsing**

Create `traffic_guard_profiles`, `traffic_log_cursors`, `traffic_minute_buckets`, `traffic_hour_buckets`, `traffic_baselines`, and `trusted_proxy_snapshots`. Profile mode is `observe`, `balanced`, `strict`, or `custom`; proxy mode is `direct`, `cloudflare`, or `custom`.

Parse IP, timestamp, method, request target, protocol, status, bytes, referrer, and user-agent with a scanner that respects quoted/escaped fields rather than splitting on spaces. Reject malformed timestamps, invalid IPs, status outside 100-599, and lines over 64 KiB. Cursors are committed in the same transaction as buckets so a crash cannot advance past uncounted traffic. Retain minute buckets 24 hours and hour buckets 30 days.

- [ ] **Step 4: Verify parsers, migrations, and retention**

Run: `go test ./internal/trafficguard ./internal/database -count=1`

Expected: PASS for IPv4/IPv6, escapes, rotation, truncation, malformed input, cursor atomicity, and retention.

- [ ] **Step 5: Commit traffic data foundation**

```bash
git add internal/database/migrations/024_security_traffic.sql internal/database/migrations_test.go internal/trafficguard
git commit -m "feat(security): add traffic guard data model"
```

### Task 2: Collect aggregates and emit evidence-backed anomalies

**Files:**
- Create: `internal/trafficguard/collector.go`
- Create: `internal/trafficguard/collector_test.go`
- Create: `internal/trafficguard/baseline.go`
- Create: `internal/trafficguard/baseline_test.go`

**Interfaces:**
- Consumes: website IDs/log paths from SQLite and `security.EventService`.
- Produces: `Collector.Collect(context.Context, time.Time) error`.
- Produces: `Evaluate(Baseline, MinuteBucket) []Anomaly` and `UpdateBaseline(Baseline, MinuteBucket) Baseline`.

- [ ] **Step 1: Write failing aggregation and baseline tests**

```go
func TestCollectorCountsStatusesAndLimitsTopLists(t *testing.T) {
	h := collectorHarness(t, 1200LogLines())
	if err := h.collector.Collect(context.Background(), h.now); err != nil { t.Fatal(err) }
	b := h.bucket()
	if b.Requests != 1200 || b.Status4xx != 300 || len(b.TopIPs) > 20 || len(b.TopPaths) > 20 { t.Fatalf("bucket=%#v", b) }
}

func TestBaselineCannotAlertBeforeSufficientSamples(t *testing.T) {
	baseline := Baseline{SampleCount: 10, MeanRPM: 10, M2RPM: 9}
	if got := Evaluate(baseline, MinuteBucket{Requests: 1000}); len(got) != 0 { t.Fatalf("premature anomalies=%v", got) }
}
```

- [ ] **Step 2: Run collector tests and verify RED**

Run: `go test ./internal/trafficguard -run 'Collector|Baseline' -count=1`

Expected: FAIL on missing collection and evaluation.

- [ ] **Step 3: Implement bounded collector and deterministic thresholds**

Read at most 10 MiB or 100,000 lines per website per tick. Count requests, status classes, bytes, estimated peak RPS, and rejected/dry-run statuses. Keep only the top 20 IP/path/agent entries using bounded maps and persist JSON summaries. Update hourly rollups transactionally.

Build baseline with Welford's online mean/variance using only completed non-anomalous minutes. Require 1,440 valid minutes spanning at least 24 hours before adaptive alerts. Emit Warning at greater of Safe absolute threshold or mean plus 4 standard deviations; emit Critical only when high request rate correlates with 5xx/load pressure. Use fingerprints `traffic:<website>:<signal>` and a 15-minute event cooldown. Never call UFW.

- [ ] **Step 4: Verify aggregation, baseline, and event behavior**

Run: `go test ./internal/trafficguard ./internal/security -count=1`

Expected: PASS for bounded cardinality, insufficient samples, sustained spikes, legitimate short bursts, correlated critical state, and deduplication.

- [ ] **Step 5: Commit anomaly collection**

```bash
git add internal/trafficguard
git commit -m "feat(security): detect HTTP traffic anomalies"
```

### Task 3: Implement trusted proxy profiles and safe Cloudflare refresh

**Files:**
- Create: `internal/trafficguard/proxy.go`
- Create: `internal/trafficguard/proxy_test.go`
- Create: `internal/trafficguard/cloudflare.go`
- Create: `internal/trafficguard/cloudflare_test.go`

**Interfaces:**
- Produces: `ValidateProxy(Profile) error`, `RenderRealIP(Profile, []netip.Prefix) string`.
- Produces: `CloudflareUpdater.Refresh(context.Context, time.Time) error` using injected HTTP client and repository.

- [ ] **Step 1: Write failing spoofing and refresh tests**

```go
func TestRenderRealIPTrustsHeaderOnlyFromConfiguredCIDR(t *testing.T) {
	got, err := RenderRealIP(Profile{ProxyMode: "custom", ProxyHeader: "X-Forwarded-For", ProxyCIDRs: []string{"192.0.2.0/24"}})
	if err != nil { t.Fatal(err) }
	if !strings.Contains(got, "set_real_ip_from 192.0.2.0/24;") || !strings.Contains(got, "real_ip_header X-Forwarded-For;") { t.Fatalf("config=%s", got) }
}

func TestFailedCloudflareRefreshKeepsLastSnapshot(t *testing.T) {
	h := cloudflareHarness(t, http.StatusServiceUnavailable, "")
	before := h.repo.CurrentSnapshot()
	if err := h.updater.Refresh(context.Background(), h.now); err == nil { t.Fatal("expected refresh error") }
	if diff := cmp.Diff(before, h.repo.CurrentSnapshot()); diff != "" { t.Fatalf("snapshot changed: %s", diff) }
}
```

- [ ] **Step 2: Run proxy tests and verify RED**

Run: `go test ./internal/trafficguard -run 'RealIP|Cloudflare|Proxy' -count=1`

Expected: FAIL because proxy rendering and updater are absent.

- [ ] **Step 3: Implement closed proxy inputs and atomic snapshots**

Direct mode renders no real-IP directives. Cloudflare mode uses only `CF-Connecting-IP` and the official IPv4/IPv6 endpoints. Custom mode accepts `X-Forwarded-For`, `X-Real-IP`, or `CF-Connecting-IP` plus non-empty validated CIDRs. Normalize/deduplicate prefixes and reject loopback, multicast, unspecified, and link-local networks for internet proxy profiles.

Use a 10-second HTTP timeout, 1 MiB response cap, HTTPS only, non-empty IPv4 and IPv6 results, and `netip` parsing. Commit a snapshot only after both lists validate. Keep the prior snapshot on any error and emit a medium event after three consecutive refresh failures.

- [ ] **Step 4: Verify proxy security tests**

Run: `go test ./internal/trafficguard -run 'RealIP|Cloudflare|Proxy' -count=1`

Expected: PASS for direct, custom, Cloudflare, forged header boundaries, malformed/empty/oversized responses, and retained snapshots.

- [ ] **Step 5: Commit trusted proxy support**

```bash
git add internal/trafficguard/proxy.go internal/trafficguard/proxy_test.go internal/trafficguard/cloudflare.go internal/trafficguard/cloudflare_test.go
git commit -m "feat(security): validate trusted proxy addresses"
```

### Task 4: Generate and roll back Nginx Traffic Guard configuration

**Files:**
- Create: `internal/trafficguard/nginx.go`
- Create: `internal/trafficguard/nginx_test.go`
- Create: `internal/trafficguard/status.go`
- Create: `internal/trafficguard/status_test.go`
- Modify: `internal/website/templates.go`
- Modify: `internal/website/templates_test.go`
- Modify: `internal/website/provisioner.go`
- Modify: `internal/website/provisioner_test.go`

**Interfaces:**
- Produces: `NginxManager.EnsureBase(context.Context) error` and `ApplyWebsite(context.Context, model.Website, WebsiteProfile) error`.
- Produces stable include path `/etc/nginx/jenderal/security/sites/<website-id>.conf`.

- [ ] **Step 1: Write failing template and rollback tests**

```go
func TestGeneratedWebsiteIncludesOpaqueSecuritySnippet(t *testing.T) {
	got := renderProfileHTTP(t, VhostData{Domain: "example.com", SecurityInclude: "/etc/nginx/jenderal/security/sites/01SITE.conf"})
	if !strings.Contains(got, "include /etc/nginx/jenderal/security/sites/01SITE.conf;") { t.Fatalf("config=%s", got) }
}

func TestApplyWebsiteRollsBackSnippetWhenNginxReloadFails(t *testing.T) {
	h := nginxHarness(t, "previous", reloadFailure())
	if err := h.manager.ApplyWebsite(context.Background(), h.website, balancedProfile()); err == nil { t.Fatal("expected error") }
	if got := h.files.Content(h.path); got != "previous" { t.Fatalf("snippet=%q", got) }
}
```

- [ ] **Step 2: Run Nginx tests and verify RED**

Run: `go test ./internal/trafficguard ./internal/website -run 'SecuritySnippet|ApplyWebsite|Traffic' -count=1`

Expected: FAIL on missing include support and manager.

- [ ] **Step 3: Add stable includes and transactional snippets**

Add `SecurityInclude string` to `website.VhostData` and render an include only when non-empty in application HTTP/TLS server blocks. Provisioning creates an empty comment-only snippet before generating a new site. Existing sites receive the include only through an explicit Traffic Guard enable/repair action; do not rewrite a manually edited vhost silently.

Create global zones in `/etc/nginx/conf.d/jenderal-traffic-zones.conf` with fixed allowlisted names. When `nginx -V` confirms `http_stub_status_module`, create a local-only status server on a root-owned Unix socket under `/run/jenderal/`; parse Active/Reading/Writing/Waiting counts through a bounded local request. Missing module support omits connection metrics and is labeled unavailable, never zero. Per-site snippets render the normalized real-IP directives, `limit_req`/`limit_conn`, status 429, and dry-run directives for Observe. Balanced, Strict, and Custom values have server-side bounds; shared-memory zone size is fixed and not user input.

For each change: stage on the same filesystem, preserve prior content, atomically rename, run `nginx -t`, reload Nginx, and verify active state. Restore the exact prior file and reload if any step fails. Refuse enforcement before `observe_started_at + 24h` or without confirmation.

- [ ] **Step 4: Verify template compatibility and rollback**

Run: `go test ./internal/trafficguard ./internal/website ./internal/nginx -count=1`

Expected: PASS for HTTP/TLS generated sites, manual-site refusal, Observe dry-run, 24-hour guard, 429, trusted IP directives, and rollback.

- [ ] **Step 5: Commit Nginx Traffic Guard integration**

```bash
git add internal/trafficguard/nginx.go internal/trafficguard/nginx_test.go internal/website/templates.go internal/website/templates_test.go internal/website/provisioner.go internal/website/provisioner_test.go
git commit -m "feat(security): apply Nginx traffic guard profiles"
```

### Task 5: Wire workers, API, and Security Center UI

**Files:**
- Create: `internal/trafficguard/worker.go`
- Create: `internal/trafficguard/worker_test.go`
- Create: `internal/trafficguard/handler.go`
- Create: `internal/trafficguard/handler_test.go`
- Modify: `internal/api/router.go`
- Modify: `internal/security/service.go`
- Modify: `cmd/jenderal/main.go`
- Create: `web/src/lib/traffic-guard.js`
- Create: `web/tests/security/TrafficGuard.test.mjs`
- Modify: `web/src/routes/security/+page.svelte`
- Modify: `web/package.json`
- Modify: `docs/security-center-ubuntu-24.04.md`

**Interfaces:**
- Produces HTTP under `/api/v1/security/traffic`: overview, buckets, top lists, website profiles, preview, apply, observe reset, proxy refresh, and origin warning.
- Produces: one collector worker with bounded periodic ticks.

- [ ] **Step 1: Write failing API/UI state tests**

```go
func TestApplyRejectsEnforcementWithoutConfirmation(t *testing.T) {
	h := newTrafficHandler(t)
	w := httptest.NewRecorder()
	h.Apply(w, request(`{"mode":"balanced","confirm":false}`))
	if w.Code != http.StatusBadRequest { t.Fatalf("status=%d", w.Code) }
}
```

```js
test('enforcement summary distinguishes local mitigation from volumetric DDoS protection', () => {
	assert.match(enforcementWarning('balanced'), /HTTP-layer/i);
	assert.match(enforcementWarning('balanced'), /CDN|provider/i);
});
```

- [ ] **Step 2: Run handler and frontend tests and verify RED**

Run: `go test ./internal/trafficguard ./internal/api -run 'ApplyRejects|Worker' -count=1 && (cd web && node --test tests/security/TrafficGuard.test.mjs)`

Expected: FAIL on missing API, worker, and helper.

- [ ] **Step 3: Implement routes, worker lifecycle, and UI**

Run collection once at startup and every minute with a non-overlapping mutex. Refresh Cloudflare prefixes daily and aggregate hourly buckets after completed hours. Read endpoints require `security.view`; profile/refresh/enforcement endpoints require `security.manage`; every accepted apply/reset is audited.

In the Traffic Guard tab show Normal/Warning/High/Critical with evidence, 24-hour observation progress, request/status charts, top IP/path/domain lists, Direct/Cloudflare/Custom proxy forms, config preview, impact counts, and explicit Apply confirmation. The primary recovery action always sets Observe Mode. Persist task IDs and reload profile/buckets after completion.

- [ ] **Step 4: Run full automated and Ubuntu verification**

Run: `go test ./... -count=1 && (cd web && npm test && npm run check && npm run build) && git diff --check`

On disposable Ubuntu 24.04, test direct traffic, Cloudflare-like requests from trusted and untrusted peers, log rotation, Observe counters, a legitimate burst, an abusive client, enforcement with 429, rollback from invalid config, panel restart cursor recovery, IPv4-only VPS behavior, and the one-click Observe recovery. Record sanitized evidence in the runbook.

- [ ] **Step 5: Commit the verified Traffic Guard slice**

```bash
git add internal/trafficguard internal/security/service.go internal/api/router.go cmd/jenderal/main.go web/src/lib/traffic-guard.js web/tests/security web/src/routes/security/+page.svelte web/package.json docs/security-center-ubuntu-24.04.md
git commit -m "feat(security): add Traffic Guard controls"
```
