# Security Foundation and Fail2ban Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the persistent Security Center foundation and a safe, recoverable Fail2ban manager for Ubuntu 24.04.

**Architecture:** Extend the existing task runner with SQLite persistence, add a shared security-event service, then wrap Fail2ban behind typed services and permission-checked HTTP handlers. The panel owns only `jenderal-panel` configuration files, validates candidates before reload, and retains the prior valid state on failure.

**Tech Stack:** Go 1.26, SQLite, Chi, existing executor/audit/notification/taskrunner packages, Fail2ban, Svelte 5, Tailwind CSS 4, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-09-security-center-design.md`

## Global Constraints

- Target Ubuntu 24.04; unsupported hosts report an actionable unavailable state.
- Safe mode permits only temporary Fail2ban bans; no permanent automatic bans.
- Never trust forwarded client-IP headers unless the peer is configured as trusted.
- Panel configuration must remain separate from administrator-owned files.
- Validate before reload and restore the last valid panel-owned configuration on failure.
- All privileged mutations require `security.manage` and an audit entry.
- Long-running task output must survive browser refresh and panel restart.
- Do not add arbitrary shell input or silently enable security components during panel updates.
- Execute this plan before the malware, Traffic Guard, and posture plans.

---

## File Map

- `internal/database/migrations/021_persistent_tasks.sql`: durable task records.
- `internal/database/migrations/022_security_foundation.sql`: settings, events, and occurrences.
- `internal/taskrunner/store.go`: SQLite task persistence boundary.
- `internal/taskrunner/runner.go`, `callback.go`: bounded output, module labels, and recovery.
- `internal/security/models.go`, `event_service.go`: shared types and event lifecycle.
- `internal/security/service.go`, `handler.go`: overview and event API.
- `internal/fail2ban/config.go`: Safe preset renderer and input validation.
- `internal/fail2ban/service.go`, `parser.go`: system adapter and typed status.
- `internal/fail2ban/handler.go`: task-backed HTTP operations and audit.
- `internal/api/router.go`, `cmd/jenderal/main.go`: wiring and routes.
- `internal/auth/rbac.go`: three Security Center permissions.
- `web/src/lib/security.js`: UI normalization and validation helpers.
- `web/src/routes/security/+page.svelte`: Security Center shell and Fail2ban UI.
- `web/src/routes/+layout.svelte`, `web/src/lib/stores/language.ts`: navigation.

### Task 1: Persist and recover background tasks

**Files:**
- Create: `internal/database/migrations/021_persistent_tasks.sql`
- Create: `internal/taskrunner/store.go`
- Create: `internal/taskrunner/store_test.go`
- Modify: `internal/taskrunner/runner.go`
- Modify: `internal/taskrunner/callback.go`
- Modify: `internal/taskrunner/runner_test.go`
- Modify: `internal/database/migrations_test.go`
- Modify: `cmd/jenderal/main.go`

**Interfaces:**
- Produces: `taskrunner.Options{Name string, Module string, Timeout time.Duration}`.
- Produces: `(*Runner).RunFuncWithOptions(Options, func(context.Context, func(string)) error) string`.
- Produces: `taskrunner.NewPersistent(*sql.DB) (*Runner, error)`; existing `New()` remains in-memory for tests.
- Produces: task JSON fields `module` and `updated_at`; existing fields remain compatible.

- [x] **Step 1: Write failing persistence and recovery tests**

```go
func TestPersistentRunnerRestoresCompletedOutput(t *testing.T) {
	db := migratedTaskDB(t)
	runner, err := NewPersistent(db)
	if err != nil { t.Fatal(err) }
	id := runner.RunFuncWithOptions(Options{Name: "scan", Module: "security", Timeout: time.Minute}, func(_ context.Context, log func(string)) error {
		log("file 1")
		return nil
	})
	waitForTask(t, runner, id, "completed")
	restarted, err := NewPersistent(db)
	if err != nil { t.Fatal(err) }
	task, ok := restarted.Get(id)
	if !ok || task.Module != "security" || task.Output != "file 1\n" { t.Fatalf("restored task = %#v", task) }
}

func TestPersistentRunnerMarksInterruptedWorkFailed(t *testing.T) {
	db := migratedTaskDB(t)
	_, err := db.Exec(`INSERT INTO background_tasks
		(id,name,module,status,output,error,started_at,updated_at) VALUES
		('running','install','security','running','step 1','',?,?)`, now, now)
	if err != nil { t.Fatal(err) }
	runner, err := NewPersistent(db)
	if err != nil { t.Fatal(err) }
	task, _ := runner.Get("running")
	if task.Status != "failed" || !strings.Contains(task.Error, "panel restarted") { t.Fatalf("task = %#v", task) }
}
```

- [x] **Step 2: Run the focused test and verify RED**

Run: `go test ./internal/taskrunner ./internal/database -run 'Persistent|Migrate' -count=1`

Expected: FAIL because the migration, store, module field, and persistent constructor do not exist.

- [x] **Step 3: Add the schema and minimal store-backed runner**

Use this schema:

```sql
CREATE TABLE IF NOT EXISTS background_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    module TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK(status IN ('running','completed','failed')),
    output TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    ended_at TEXT,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_background_tasks_updated ON background_tasks(updated_at DESC);
```

Add these exact types and entry points:

```go
const maxTaskOutputBytes = 512 * 1024

type Options struct {
	Name    string
	Module  string
	Timeout time.Duration
}

type Store interface {
	Upsert(context.Context, Task) error
	LoadRecent(context.Context, int) ([]Task, error)
	FailRunning(context.Context, time.Time, string) error
}

func NewPersistent(db *sql.DB) (*Runner, error) {
	store := NewSQLiteStore(db)
	now := time.Now().UTC()
	if err := store.FailRunning(context.Background(), now, "panel restarted before task completed"); err != nil { return nil, err }
	tasks, err := store.LoadRecent(context.Background(), 200)
	if err != nil { return nil, err }
	return newRunner(store, tasks), nil
}
```

Route `Run`, `RunMultiple`, and `RunFunc` through shared task creation, bounded append, and completion helpers. Persist creation and completion immediately; persist output no more than once per 250 ms and once again at completion. Truncate from the beginning with a visible `[older output truncated]` marker. Preserve `New()` by calling `newRunner(nil, nil)`.

In `cmd/jenderal/main.go`, replace `taskrunner.New()` with `taskrunner.NewPersistent(db)` and fail startup with a clear log entry if restoration fails.

- [x] **Step 4: Verify focused and package tests**

Run: `go test ./internal/taskrunner ./internal/database ./cmd/jenderal -count=1`

Expected: PASS, including existing non-interactive sudo behavior.

- [x] **Step 5: Commit the durable task slice**

```bash
git add internal/database/migrations/021_persistent_tasks.sql internal/database/migrations_test.go internal/taskrunner cmd/jenderal/main.go
git commit -m "feat(tasks): persist background task progress"
```

### Task 2: Add shared security events and retention-safe persistence

**Files:**
- Create: `internal/database/migrations/022_security_foundation.sql`
- Create: `internal/security/models.go`
- Create: `internal/security/event_service.go`
- Create: `internal/security/event_service_test.go`
- Modify: `internal/database/migrations_test.go`

**Interfaces:**
- Consumes: migrated SQLite database.
- Produces: `security.EventService.Record(context.Context, EventInput, time.Time) (Event, bool, error)`; the boolean is true only for a new event.
- Produces: `List(context.Context, EventFilter) ([]Event, int, error)`, `Transition(context.Context, string, EventStatus, time.Time) error`, and `Cleanup(context.Context, time.Time) error`.
- Produces: `security.NotificationSender` with existing-compatible `SendAll(context.Context, string) error`.

- [ ] **Step 1: Write failing event lifecycle tests**

```go
func TestRecordDeduplicatesOpenEventAndStoresOccurrence(t *testing.T) {
	svc := newEventTestService(t, &recordingNotifier{})
	input := EventInput{Fingerprint: "fail2ban:sshd:203.0.113.7", Category: "intrusion", Severity: SeverityHigh, Component: "fail2ban", Resource: "sshd", Evidence: `{"ip":"203.0.113.7"}`}
	first, created, err := svc.Record(context.Background(), input, time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC))
	if err != nil || !created { t.Fatalf("first record: created=%v err=%v", created, err) }
	second, created, err := svc.Record(context.Background(), input, time.Date(2026, 9, 9, 1, 1, 0, 0, time.UTC))
	if err != nil || created || second.ID != first.ID || second.OccurrenceCount != 2 { t.Fatalf("second = %#v created=%v err=%v", second, created, err) }
}

func TestTransitionRejectsInvalidStateChange(t *testing.T) {
	svc := newEventTestService(t, nil)
	event := recordEvent(t, svc)
	if err := svc.Transition(context.Background(), event.ID, StatusResolved, time.Now()); err != nil { t.Fatal(err) }
	if err := svc.Transition(context.Background(), event.ID, StatusAcknowledged, time.Now()); err == nil { t.Fatal("resolved event reopened") }
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/security ./internal/database -run 'Event|Migrate' -count=1`

Expected: FAIL because the security tables and package do not exist.

- [ ] **Step 3: Create additive schema and transactional event service**

Create `security_settings`, `security_events`, `security_event_occurrences`, and `security_manual_bans`. The manual-ban table records component, jail, address, requested expiry, actual expiry, and active/released state so expiry reconciliation survives restart. Store timestamps as RFC3339 UTC. Index `(status,last_seen DESC)`, `(component,last_seen DESC)`, occurrence `event_id`, and active manual-ban expiry.

Define closed enums and validate every input:

```go
type Severity string
const ( SeverityInfo Severity = "info"; SeverityLow Severity = "low"; SeverityMedium Severity = "medium"; SeverityHigh Severity = "high"; SeverityCritical Severity = "critical" )
type EventStatus string
const ( StatusOpen EventStatus = "open"; StatusAcknowledged EventStatus = "acknowledged"; StatusResolved EventStatus = "resolved"; StatusFalsePositive EventStatus = "false_positive" )
type EventInput struct { Fingerprint, Category, Component, Resource, Evidence, RecommendedAction string; Severity Severity }
```

`Record` must use one transaction: find the newest open/acknowledged matching fingerprint, update count/last-seen and add an occurrence, or insert both a new event and its first occurrence. Send notification only after commit, immediately for high/critical new events, and respect a persisted 15-minute `notified_at` cooldown for repeats. Notification failure does not roll back evidence.

`Cleanup` deletes occurrences and resolved/false-positive events older than the `security.event_retention_days` setting (default 90), but never touches open events.

- [ ] **Step 4: Verify event behavior and migrations**

Run: `go test ./internal/security ./internal/database -count=1`

Expected: PASS with one event, two occurrences, valid transitions, and idempotent migration execution.

- [ ] **Step 5: Commit the event foundation**

```bash
git add internal/database/migrations/022_security_foundation.sql internal/database/migrations_test.go internal/security
git commit -m "feat(security): add security event lifecycle"
```

### Task 3: Add Security Center permissions, overview, and event API

**Files:**
- Create: `internal/security/service.go`
- Create: `internal/security/service_test.go`
- Create: `internal/security/handler.go`
- Create: `internal/security/handler_test.go`
- Modify: `internal/auth/rbac.go`
- Modify: `internal/auth/rbac_test.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/jenderal/main.go`

**Interfaces:**
- Consumes: `security.EventService`, `audit.Service`, and `notification.Service`.
- Produces: `security.Probe{Name() string; Check(context.Context) ComponentStatus}`.
- Produces: `Service.Overview(context.Context) Overview`, `Service.ListEvents`, and `Service.TransitionEvent`.
- Produces HTTP: `GET /api/v1/security/overview`, `GET /api/v1/security/events`, `POST /api/v1/security/events/{id}/transition`.

- [ ] **Step 1: Write failing RBAC and handler contract tests**

```go
func TestSecurityPermissionsSeededForAdminAndViewForUser(t *testing.T) {
	db := migratedAuthDB(t)
	rbac := NewRBAC(db)
	if err := rbac.Seed(context.Background()); err != nil { t.Fatal(err) }
	assertRolePermission(t, db, "admin", "security.manage", true)
	assertRolePermission(t, db, "admin", "security.quarantine", true)
	assertRolePermission(t, db, "user", "security.view", true)
	assertRolePermission(t, db, "user", "security.manage", false)
}

func TestTransitionEventRejectsUnknownStatus(t *testing.T) {
	h := newSecurityHandler(t)
	r := authenticatedRequest(http.MethodPost, "/api/v1/security/events/01/transition", `{"status":"deleted"}`)
	w := httptest.NewRecorder()
	h.TransitionEvent(w, r)
	if w.Code != http.StatusBadRequest { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
}
```

- [ ] **Step 2: Run focused tests and verify RED**

Run: `go test ./internal/auth ./internal/security ./internal/api -run 'Security|Transition' -count=1`

Expected: FAIL on missing permissions, handlers, and dependency wiring.

- [ ] **Step 3: Implement permissions and typed endpoints**

Append `security.view`, `security.manage`, and `security.quarantine` to the seed list. Give regular users only `security.view`; admins receive all permissions through the existing admin loop. Update count-based RBAC assertions from 60 to 63.

Define:

```go
type ComponentStatus struct { Name, State, Version, Message string; Installed, Enabled, Healthy bool; CheckedAt time.Time }
type Probe interface { Name() string; Check(context.Context) ComponentStatus }
type Overview struct { Condition string; Reasons []string; Components []ComponentStatus; OpenEvents int; SetupComplete bool; ActiveTasks []taskrunner.Task }
```

The condition algorithm is deterministic: Critical when any critical open event exists; Needs Attention for unhealthy enabled components or high open events; Good otherwise. Missing optional components before setup are `not_installed`, not Critical.

Register routes with `security.view` for reads and `security.manage` for transitions. Transition requests accept only `acknowledged`, `resolved`, or `false_positive`. Audit successful mutations with module `security`.

Wire the services through new `SecuritySvc` and `SecurityEvents` fields in `api.Dependencies` and construct them in `cmd/jenderal/main.go`.

- [ ] **Step 4: Verify RBAC, API, and overview tests**

Run: `go test ./internal/auth ./internal/security ./internal/api ./cmd/jenderal -count=1`

Expected: PASS and existing route tests remain compatible.

- [ ] **Step 5: Commit the Security Center API**

```bash
git add internal/auth internal/security internal/api/router.go cmd/jenderal/main.go
git commit -m "feat(security): expose security overview and events"
```

### Task 4: Implement Fail2ban discovery, parsing, and Safe configuration

**Files:**
- Create: `internal/fail2ban/models.go`
- Create: `internal/fail2ban/parser.go`
- Create: `internal/fail2ban/parser_test.go`
- Create: `internal/fail2ban/config.go`
- Create: `internal/fail2ban/config_test.go`
- Create: `internal/fail2ban/service.go`
- Create: `internal/fail2ban/service_test.go`

**Interfaces:**
- Consumes: `executor.CommandExecutor` and `security.EventService`.
- Produces: `fail2ban.Settings`, `Status`, `Jail`, and `Ban` JSON models.
- Produces: `Service.Check`, `Install`, `Apply`, `Start`, `Stop`, `Restart`, `Bans`, `Ban`, and `Unban`.
- Produces: a `security.Probe` implementation named `fail2ban`.

- [ ] **Step 1: Write failing parser, renderer, and rollback tests**

```go
func TestRenderSafeConfigUsesTemporarySSHBanAndLiteralCIDRs(t *testing.T) {
	got, err := Render(Settings{SSHDEnabled: true, MaxRetry: 5, FindTimeSeconds: 600, BanTimeSeconds: 900, IgnoreIPs: []string{"127.0.0.1/8", "203.0.113.8/32"}})
	if err != nil { t.Fatal(err) }
	for _, want := range []string{"[sshd]", "maxretry = 5", "findtime = 600", "bantime = 900", "ignoreip = 127.0.0.1/8 203.0.113.8/32"} {
		if !strings.Contains(got, want) { t.Fatalf("missing %q in %s", want, got) }
	}
}

func TestApplyRestoresPreviousConfigWhenReloadFails(t *testing.T) {
	fs := newFakeManagedFiles(map[string]string{managedConfigPath: "previous"})
	svc := NewService(reloadFailingExecutor(), fs, nil)
	if err := svc.Apply(context.Background(), SafeSettings()); err == nil { t.Fatal("expected reload failure") }
	if got := fs.Content(managedConfigPath); got != "previous" { t.Fatalf("config=%q", got) }
}
```

- [ ] **Step 2: Run Fail2ban tests and verify RED**

Run: `go test ./internal/fail2ban -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the typed Fail2ban adapter**

Validate PHP-independent values as follows: max retry 1-20, find time 60-86400 seconds, ban time 60-604800 seconds, and every ignore entry through `net/netip.ParsePrefix` or `ParseAddr`. Always add loopback. Render only known jail names discovered under `/etc/fail2ban/filter.d`; never accept a filename from the API.

Discovery uses fixed commands: `fail2ban-client --version`, `systemctl show fail2ban --property=ActiveState,SubState,UnitFileState`, `fail2ban-client status`, `fail2ban-client status <allowlisted-jail>`, and `sshd -T` for the effective SSH port. Parse status output without depending on translated label spacing; reject malformed ban counts rather than returning zero.

Installation runs `apt-get install -y -o DPkg::Lock::Timeout=120 -o Acquire::Retries=2 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30 fail2ban`, then `systemctl enable --now fail2ban`.

For `Apply`, copy `/etc/fail2ban` into a temporary validation root, install the candidate as `jail.d/jenderal-panel.local`, run `fail2ban-client -t -c <validation-root>`, atomically install the candidate at `/etc/fail2ban/jail.d/jenderal-panel.local`, reload, and confirm requested jails. Restore the prior content or remove a newly created target if reload/health fails.

Manual bans use `fail2ban-client set <discovered-jail> banip <validated-address>` and require an expiry between 60 seconds and that jail's current `bantime` (900 seconds in Safe mode). Persist the requested expiry in `security_manual_bans`; a one-minute reconciliation worker performs early unban when required and repairs expired rows after restart. Report the actual expiry returned by the service. Automatic Fail2ban bans rely on jail `bantime` and never use UFW rules.

- [ ] **Step 4: Run the Fail2ban unit suite**

Run: `go test ./internal/fail2ban -count=1`

Expected: PASS for English/spacing variants, missing package, unavailable filters, invalid CIDRs, Safe defaults, successful apply, validation rollback, reload rollback, manual expiry, and command allowlisting.

- [ ] **Step 5: Commit the Fail2ban service**

```bash
git add internal/fail2ban
git commit -m "feat(security): add safe fail2ban manager"
```

### Task 5: Expose task-backed Fail2ban operations and events

**Files:**
- Create: `internal/fail2ban/handler.go`
- Create: `internal/fail2ban/handler_test.go`
- Modify: `internal/security/service.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/jenderal/main.go`

**Interfaces:**
- Consumes: Fail2ban service, task runner, audit, and event service.
- Produces HTTP: `GET /security/fail2ban`, `POST /install`, `PUT /settings`, service actions, `GET /bans`, `POST /bans`, and `DELETE /bans/{ip}` under `/api/v1/security/fail2ban`.

- [ ] **Step 1: Write failing handler behavior tests**

```go
func TestInstallReturnsPersistentSecurityTask(t *testing.T) {
	h := newFail2banHandler(t)
	w := httptest.NewRecorder()
	h.Install(w, authenticatedRequest(http.MethodPost, "/", `{}`))
	if w.Code != http.StatusAccepted { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	var body struct{ Data struct{ TaskID string `json:"task_id"` } `json:"data"` }
	json.NewDecoder(w.Body).Decode(&body)
	if body.Data.TaskID == "" { t.Fatal("missing task_id") }
}

func TestManualBanRejectsPermanentDuration(t *testing.T) {
	h := newFail2banHandler(t)
	w := httptest.NewRecorder()
	h.Ban(w, authenticatedRequest(http.MethodPost, "/", `{"jail":"sshd","ip":"203.0.113.7","duration_seconds":0}`))
	if w.Code != http.StatusBadRequest { t.Fatalf("status=%d", w.Code) }
}
```

- [ ] **Step 2: Run handler tests and verify RED**

Run: `go test ./internal/fail2ban ./internal/api -run 'InstallReturns|ManualBan|SecurityRoutes' -count=1`

Expected: FAIL on missing handlers and routes.

- [ ] **Step 3: Implement permissioned routes, auditing, and task progress**

Install and apply operations call `RunFuncWithOptions` with `Module: "security"` and explicit 20-minute timeouts. Log named phases before package install, validation, promotion, reload, and health confirmation. Service start/stop/restart, settings changes, ban, and unban require `security.manage`; reads require `security.view`.

Audit only accepted mutations, recording the user, remote address, jail, IP, duration, and task ID without copying raw command output. Record high-severity intrusion events from observed bans with fingerprint `fail2ban:<jail>:<ip>` and resolve them after unban/expiry.

Add the Fail2ban probe to the overview. A missing optional package before setup is `not_installed`; an enabled but stopped service is unhealthy.

- [ ] **Step 4: Verify route, task, audit, and event tests**

Run: `go test ./internal/fail2ban ./internal/security ./internal/api ./cmd/jenderal -count=1`

Expected: PASS and a page refresh can retrieve the same task by ID.

- [ ] **Step 5: Commit Fail2ban API integration**

```bash
git add internal/fail2ban internal/security/service.go internal/api/router.go cmd/jenderal/main.go
git commit -m "feat(security): expose fail2ban operations"
```

### Task 6: Build the Security Center and Fail2ban UI

**Files:**
- Create: `web/src/lib/security.js`
- Create: `web/tests/security/SecurityCenter.test.mjs`
- Create: `web/src/routes/security/+page.svelte`
- Modify: `web/src/routes/+layout.svelte`
- Modify: `web/src/lib/stores/language.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: overview, events, Fail2ban, and task endpoints from Tasks 3 and 5.
- Produces: navigation entry `/security`, Simple/Advanced Fail2ban forms, setup status, bans table, and persistent `TaskProgress` integration.

- [ ] **Step 1: Write failing frontend helper tests**

```js
test('safe preset cannot produce a permanent ban', () => {
	assert.deepEqual(buildSafeFail2banSettings(['203.0.113.8/32']), {
		sshd_enabled: true, max_retry: 5, find_time_seconds: 600,
		ban_time_seconds: 900, ignore_ips: ['203.0.113.8/32']
	});
	assert.throws(() => validateBan({ jail: 'sshd', ip: '203.0.113.7', duration_seconds: 0 }), /temporary/i);
});

test('overview condition always includes human-readable reasons', () => {
	assert.deepEqual(normalizeOverview({ condition: 'needs_attention', reasons: [] }).reasons, ['Review component status below.']);
});
```

- [ ] **Step 2: Run frontend tests and verify RED**

Run: `cd web && node --test tests/security/SecurityCenter.test.mjs`

Expected: FAIL because `security.js` does not exist.

- [ ] **Step 3: Implement helpers and the responsive page**

Export `buildSafeFail2banSettings`, `validateBan`, `normalizeOverview`, `conditionTone`, and `formatBanExpiry`. Use literal IP/CIDR strings and defer authoritative validation to the backend.

The page contains Overview, Fail2ban, and Events tabs. Fail2ban starts in Simple mode with the Safe values and an explicit warning before enabling SSH protection without a management CIDR. Advanced fields enforce numeric bounds. Install/apply operations store their task IDs under `jenderal_security_fail2ban_task` and render `TaskProgress`; completion reloads overview, settings, jails, and bans. Every loading state terminates in content, empty state, or actionable retry.

Add `nav.security_center` translations (`Security Center`, `Pusat Keamanan`) and place it first in the Security navigation group using the existing shield icon.

- [ ] **Step 4: Verify tests, type checks, and build**

Run: `cd web && npm test && npm run check && npm run build`

Expected: all Node tests pass, Svelte reports zero errors, and the production bundle builds.

- [ ] **Step 5: Commit the first Security Center UI**

```bash
git add web/src/lib/security.js web/tests/security web/src/routes/security web/src/routes/+layout.svelte web/src/lib/stores/language.ts web/package.json
git commit -m "feat(security): add fail2ban security center UI"
```

### Task 7: Verify the first deployable security slice

**Files:**
- Create: `docs/security-center-ubuntu-24.04.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: all outputs in this plan.
- Produces: operator recovery guide and a verified base for the malware plan.

- [ ] **Step 1: Document exact safe setup and recovery checks**

Document package state, `/etc/fail2ban/jail.d/jenderal-panel.local`, task recovery semantics, SSH allowlist warning, `sudo fail2ban-client status`, `sudo fail2ban-client set sshd unbanip <address>`, and how to return the component to a stopped state without disabling UFW.

- [ ] **Step 2: Run the complete automated gate**

Run: `go test ./... -count=1 && (cd web && npm test && npm run check && npm run build) && git diff --check`

Expected: exit 0 with no Go failures, Node failures, Svelte errors, build errors, or whitespace errors.

- [ ] **Step 3: Exercise a disposable Ubuntu 24.04 VPS**

Run the panel setup, install Fail2ban from `/security`, confirm the task survives a full browser refresh, validate `sshd` is active, generate failed SSH attempts from a non-allowlisted test address, observe a temporary ban, unban it from the panel, restart the panel during a deliberately long test task, and confirm the interrupted task retains output with a failed/retryable state. Keep one provider console session open throughout the lockout test.

- [ ] **Step 4: Record evidence in the runbook**

Append the tested panel commit, Ubuntu release, Fail2ban version, active firewall backend, test timestamp, commands used, and pass/fail results. Do not include public IPs, credentials, tokens, or complete auth logs.

- [ ] **Step 5: Commit the verified slice**

```bash
git add docs/security-center-ubuntu-24.04.md README.md
git commit -m "docs(security): add fail2ban operations runbook"
```
