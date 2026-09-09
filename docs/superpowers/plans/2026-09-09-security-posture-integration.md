# Security Posture and Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish Security Center with explainable posture checks, a resumable Safe setup wizard, notification/retention integration, recovery UX, and full Ubuntu 24.04 proof.

**Architecture:** Implement read-only probes over the existing firewall, SSL, service, and executor boundaries; aggregate probe findings with open security events into an explainable condition. Persist wizard steps before execution, run all mutations through existing component services and durable tasks, then complete the frontend around one recovery-first workflow.

**Tech Stack:** Go 1.26, SQLite, systemd/UFW/AppArmor/OpenSSH/Nginx/ClamAV/Fail2ban adapters, existing alert/notification/audit/taskrunner packages, Svelte 5, Tailwind CSS 4, Node test runner.

**Spec:** `docs/superpowers/specs/2026-09-09-security-center-design.md`

## Global Constraints

- Complete the foundation, malware, and Traffic Guard plans first.
- Posture checks observe and explain; they do not silently mutate SSH, UFW, AppArmor, or package configuration.
- Missing optional tools report `unknown` or `not_installed`, not a false healthy state.
- Security condition always contains concrete reasons and is not presented as a guarantee.
- Setup steps are explicit, resumable, idempotent, and reviewed before Apply.
- Critical events notify immediately; repeats use cooldown; recovery sends at most one message.
- Secrets, public IP inventories, complete auth logs, and malware content never enter normal logs or runbook evidence.

---

## File Map

- `internal/security/posture.go`, `posture_test.go`: system probes and normalized findings.
- `internal/security/scoring.go`, `scoring_test.go`: explainable Good/Needs Attention/Critical result.
- `internal/database/migrations/025_security_setup.sql`: resumable setup runs and step checkpoints.
- `internal/security/setup.go`, `setup_test.go`: Safe wizard state machine.
- `internal/security/worker.go`, `worker_test.go`: posture, event, and retention schedules.
- `internal/security/handler.go`: setup, posture, recovery, and event endpoints.
- `internal/security/event_service.go`: recovery notification and cooldown completion.
- `web/src/lib/security-setup.js`: step validation and review summaries.
- `web/src/routes/security/+page.svelte`: completed overview, setup, posture, and recovery UX.
- `README.md`, `docs/security-center-ubuntu-24.04.md`: user and operator guidance.

### Task 1: Add read-only posture probes

**Files:**
- Create: `internal/security/posture.go`
- Create: `internal/security/posture_test.go`
- Modify: `internal/security/service.go`

**Interfaces:**
- Consumes: `firewall.Service`, `ssl.Service`, `service.ServiceManager`, `executor.CommandExecutor`, and component probes.
- Produces: `PostureChecker.Check(context.Context, time.Time) PostureReport`.
- Produces: normalized `Finding{Code, Component, Severity, State, Summary, Remediation string; Evidence map[string]any}`.

- [ ] **Step 1: Write failing probe tests from real command fixtures**

```go
func TestPostureReportsAppArmorMixedModesWithoutCallingItDisabled(t *testing.T) {
	exec := fixtureExecutor(map[string]executor.Result{"aa-status --json": {Stdout: `{"profiles":{"enforce":10,"complain":2},"processes":{"unconfined":1}}`}})
	report := NewPostureChecker(exec, nil, nil, nil).Check(context.Background(), fixedNow)
	f := findingByCode(t, report, "apparmor_profiles_not_enforced")
	if f.Severity != SeverityMedium || !strings.Contains(f.Summary, "2") { t.Fatalf("finding=%#v", f) }
}

func TestPostureTreatsUnavailableSecurityUpdateToolAsUnknown(t *testing.T) {
	exec := commandNotFoundFixture("ubuntu-security-status")
	report := NewPostureChecker(exec, nil, nil, nil).Check(context.Background(), fixedNow)
	if got := componentState(report, "security_updates"); got != "unknown" { t.Fatalf("state=%q", got) }
}
```

- [ ] **Step 2: Run posture tests and verify RED**

Run: `go test ./internal/security -run 'Posture|AppArmor|SecurityUpdate' -count=1`

Expected: FAIL because the posture checker is absent.

- [ ] **Step 3: Implement bounded probes and explicit unknown states**

Probe UFW through `firewall.Status`; AppArmor through `aa-status --json` with a text fallback; SSH through `/usr/sbin/sshd -T`; services through existing service managers; ClamAV signature timestamps through the malware service; Nginx config through `nginx -t`; certificates through the SSL service; security updates through `ubuntu-security-status --format json` when available and a clearly labeled unknown state otherwise.

All command contexts use a 10-second deadline and 1 MiB output cap. Parse effective SSH keys `port`, `permitrootlogin`, and `passwordauthentication`; emit guidance only. Treat active UFW, valid Nginx, current signatures, and enforced AppArmor profiles as positive evidence, but never suppress unrelated findings.

- [ ] **Step 4: Verify success, warning, malformed, timeout, and missing-command fixtures**

Run: `go test ./internal/security -run 'Posture|AppArmor|SSH|UFW|Signature|Nginx|SSL|SecurityUpdate' -count=1`

Expected: PASS without privileged mutations in captured executor calls.

- [ ] **Step 5: Commit posture probes**

```bash
git add internal/security/posture.go internal/security/posture_test.go internal/security/service.go
git commit -m "feat(security): inspect server security posture"
```

### Task 2: Complete explainable scoring, event recovery, and retention workers

**Files:**
- Create: `internal/security/scoring.go`
- Create: `internal/security/scoring_test.go`
- Create: `internal/security/worker.go`
- Create: `internal/security/worker_test.go`
- Modify: `internal/security/event_service.go`
- Modify: `internal/security/event_service_test.go`
- Modify: `cmd/jenderal/main.go`

**Interfaces:**
- Produces: `Score(PostureReport, []Event) Condition`.
- Produces: `Worker.Start(context.Context)` and testable `Tick(context.Context, time.Time)`.
- Extends: event transition/reconciliation with one recovery notification.

- [ ] **Step 1: Write failing scoring and cooldown tests**

```go
func TestScoreReturnsCriticalWithOrderedReasons(t *testing.T) {
	condition := Score(PostureReport{Findings: []Finding{{Code: "nginx_invalid", Severity: SeverityCritical, Summary: "Nginx configuration is invalid"}}}, nil)
	if condition.Level != "critical" || !reflect.DeepEqual(condition.Reasons, []string{"Nginx configuration is invalid"}) { t.Fatalf("condition=%#v", condition) }
}

func TestRepeatedEventAndRecoverySendAtMostTwoNotifications(t *testing.T) {
	n := &recordingNotifier{}
	svc := newEventTestService(t, n)
	e := recordAt(t, svc, highInput(), fixedNow)
	recordAt(t, svc, highInput(), fixedNow.Add(time.Minute))
	if err := svc.Transition(context.Background(), e.ID, StatusResolved, fixedNow.Add(2*time.Minute)); err != nil { t.Fatal(err) }
	if len(n.Messages) != 2 { t.Fatalf("messages=%v", n.Messages) }
}
```

- [ ] **Step 2: Run scoring/worker tests and verify RED**

Run: `go test ./internal/security -run 'Score|Notification|Worker|Cleanup' -count=1`

Expected: FAIL on missing scoring and scheduled reconciliation.

- [ ] **Step 3: Implement deterministic score and non-overlapping workers**

Critical if any critical open event or critical posture finding exists. Needs Attention if any high/medium open event or unhealthy enabled component exists. Good otherwise. Sort reasons by severity, component, then code; return at most five primary reasons plus a count of additional findings.

Run posture/event reconciliation every five minutes, retention cleanup daily, Traffic Guard retention hourly, and malware schedule ticks every minute through their existing workers. Guard each tick against overlap. When evidence returns to normal, resolve matching posture events once and send one recovery notification. Notification transport failures are logged without reopening resolved events.

- [ ] **Step 4: Verify lifecycle and worker tests**

Run: `go test ./internal/security ./internal/trafficguard ./internal/malware -count=1`

Expected: PASS for ordered scoring, deduplication, cooldown, recovery, cancellation, non-overlap, and retention boundaries.

- [ ] **Step 5: Commit scoring and workers**

```bash
git add internal/security cmd/jenderal/main.go
git commit -m "feat(security): reconcile security condition and events"
```

### Task 3: Implement the resumable Safe setup wizard

**Files:**
- Create: `internal/database/migrations/025_security_setup.sql`
- Create: `internal/security/setup.go`
- Create: `internal/security/setup_test.go`
- Modify: `internal/security/handler.go`
- Modify: `internal/security/handler_test.go`
- Modify: `internal/api/router.go`
- Modify: `internal/database/migrations_test.go`

**Interfaces:**
- Produces: `SetupAssessment`, `SetupRequest`, `SetupReview`, and `SetupState`.
- Produces: `Assess`, `Review`, `Apply`, and `Resume` methods.
- Produces HTTP: `GET /security/setup`, `POST /security/setup/review`, `POST /security/setup/apply`, and `POST /security/setup/resume`.

- [ ] **Step 1: Write failing review/idempotency tests**

```go
func TestReviewListsEveryMutationBeforeApply(t *testing.T) {
	svc := setupHarness(t).service
	review, err := svc.Review(context.Background(), safeSetupRequest())
	if err != nil { t.Fatal(err) }
	want := []string{"install fail2ban", "configure sshd jail", "install clamav", "schedule daily quick scan", "start Traffic Guard observation"}
	if diff := cmp.Diff(want, review.Mutations); diff != "" { t.Fatalf("mutations (-want +got): %s", diff) }
}

func TestApplyResumesAfterCompletedStepWithoutRepeatingIt(t *testing.T) {
	h := interruptedSetupHarness(t, "fail2ban_configured")
	if err := h.service.Apply(context.Background(), h.request, h.log); err != nil { t.Fatal(err) }
	if h.fail2banInstallCalls != 0 || h.malwareInstallCalls != 1 { t.Fatalf("calls=%#v", h) }
}
```

- [ ] **Step 2: Run setup tests and verify RED**

Run: `go test ./internal/security ./internal/api -run 'ReviewLists|ApplyResumes|Setup' -count=1`

Expected: FAIL because the setup state machine and routes do not exist.

- [ ] **Step 3: Implement explicit review and checkpointed application**

Assessment returns Ubuntu/resource/package/module/log/SSH/UFW/notification facts and does not mutate. Request contains management CIDRs, Fail2ban Safe toggle, malware runtime/schedule, Traffic Guard Observe selections, proxy profiles, and notification test channel IDs. Review validates all selections and returns exact packages, files, service actions, websites, and warnings.

Migration 025 creates `security_setup_runs` and `security_setup_steps`, keyed by run ID and closed step name, with request/review JSON, review hash, status, safe error, task ID, and timestamps. Persist a setup run and each completed step before advancing. Apply runs through component service methods in this order: prerequisite recheck, Fail2ban install/config, ClamAV install/signatures/schedule, Traffic Guard base config/Observe profiles, notification tests, final health assessment. Resume skips completed idempotent steps. Return one persistent security task ID. A failed step stores its safe error and retry point without rolling back earlier independently healthy components.

All setup reads require `security.view`; review/apply/resume require `security.manage`. Audit the accepted review hash, task ID, completed steps, and final result.

- [ ] **Step 4: Verify setup state, permissions, refresh, and retry**

Run: `go test ./internal/security ./internal/api ./internal/taskrunner -count=1`

Expected: PASS for assessment-only behavior, invalid CIDRs, review completeness, explicit confirmation, task restoration, failed-step resume, and repeat Apply idempotency.

- [ ] **Step 5: Commit the setup wizard backend**

```bash
git add internal/database/migrations/025_security_setup.sql internal/database/migrations_test.go internal/security/setup.go internal/security/setup_test.go internal/security/handler.go internal/security/handler_test.go internal/api/router.go
git commit -m "feat(security): add resumable safe setup wizard"
```

### Task 4: Complete Security Center UX and recovery controls

**Files:**
- Create: `web/src/lib/security-setup.js`
- Create: `web/tests/security/SecuritySetup.test.mjs`
- Modify: `web/src/routes/security/+page.svelte`
- Modify: `web/src/lib/stores/language.ts`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: setup, overview, posture, event, Fail2ban, malware, Traffic Guard, task, and notification APIs.
- Produces: completed Simple/Advanced Security Center with accessible setup and emergency recovery.

- [ ] **Step 1: Write failing wizard and recovery helper tests**

```js
test('safe setup review keeps mutation and warning order', () => {
	const review = normalizeSetupReview({ mutations: ['install fail2ban', 'install clamav'], warnings: ['Confirm console access'] });
	assert.deepEqual(review, { mutations: ['install fail2ban', 'install clamav'], warnings: ['Confirm console access'] });
});

test('recovery action always returns Traffic Guard to observe', () => {
	assert.deepEqual(buildTrafficRecovery('01SITE'), { website_id: '01SITE', mode: 'observe', confirm: true });
});
```

- [ ] **Step 2: Run Security Center frontend tests and verify RED**

Run: `cd web && node --test tests/security/SecuritySetup.test.mjs`

Expected: FAIL because `security-setup.js` does not exist.

- [ ] **Step 3: Implement final responsive and accessible workflow**

Overview shows condition reasons, component cards, active incidents, last posture/scan, and active tasks. Setup is an eight-step dialog: assessment, management CIDRs, Fail2ban, malware schedule, Traffic Guard Observe, trusted proxies, notification test, and review/apply. Persist only non-secret drafts locally; restore server-side task/setup state after refresh.

Events support Open/Acknowledged/Resolved/False Positive with severity filters and occurrence details. Recovery panel exposes Fail2ban unban/console guidance, return Traffic Guard to Observe, and quarantine restore. Simple mode remains the initial view; Advanced reveals bounded fields and config previews. All destructive buttons have distinct confirmations and keyboard focus restoration.

Add Indonesian/English strings for Security Center status, setup steps, recovery, DDoS capability boundary, quarantine warnings, and unknown states. Update `web/package.json` so `npm test` includes every `tests/security/*.test.mjs` file.

- [ ] **Step 4: Verify frontend behavior and production build**

Run: `cd web && npm test && npm run check && npm run build`

Expected: zero Node test failures, zero Svelte errors, and a successful production build in both light and dark theme markup.

- [ ] **Step 5: Commit final Security Center UX**

```bash
git add web/src/lib/security-setup.js web/tests/security web/src/routes/security/+page.svelte web/src/lib/stores/language.ts web/package.json
git commit -m "feat(security): complete safe setup and recovery UX"
```

### Task 5: Final documentation, compatibility, and Ubuntu 24.04 release gate

**Files:**
- Modify: `README.md`
- Modify: `docs/security-center-ubuntu-24.04.md`
- Modify: `scripts/install.sh`
- Modify: `internal/database/migrations_test.go`

**Interfaces:**
- Consumes: all four Security Center plans.
- Produces: upgrade-safe documentation and release evidence ready for main.

- [ ] **Step 1: Update operator documentation and installer-owned directories**

Document Safe defaults, component packages, resource modes, proxy trust, DDoS boundary, retention, all recovery actions, and removal behavior. Update `scripts/install.sh` only to create `/var/lib/jenderal/quarantine` mode 0700 and `/etc/nginx/jenderal/security/sites` root-owned mode 0755; do not install or enable Fail2ban/ClamAV during panel installation. Preserve existing installs and make directory creation idempotent.

- [ ] **Step 2: Run the full automated gate from a clean dependency state**

Run: `go test ./... -count=1 && (cd web && npm ci && npm test && npm run check && npm run build) && git diff --check`

Expected: exit 0; migration tests include tables 021-024 and prove a second migration run changes no existing website, SSL, alert, task, or security data.

- [ ] **Step 3: Execute the complete disposable Ubuntu 24.04 matrix**

Verify fresh panel install, in-place upgrade from the pre-Security-Center release, Safe wizard refresh/resume, SSH access during Fail2ban tests, malware clean/infected/quarantine/restore paths, Traffic Guard Direct/Cloudflare/Custom modes, IPv4-only Nginx, event dedup/recovery, every notification channel, panel restart task recovery, low-resource behavior, configuration rollback, and repeat Repair/Apply idempotency.

- [ ] **Step 4: Record sanitized proof and run final diff review**

Record commit, Ubuntu/kernel/package versions, durations, peak memory, scenarios, and pass/fail results in the runbook. Run `git status --short`, `git diff --check`, and review every changed file for secrets, raw malware content, accidental broad filesystem paths, shell interpolation, and unrelated edits.

- [ ] **Step 5: Commit the release gate**

```bash
git add README.md docs/security-center-ubuntu-24.04.md scripts/install.sh internal/database/migrations_test.go
git commit -m "docs(security): finalize Ubuntu security center rollout"
```
