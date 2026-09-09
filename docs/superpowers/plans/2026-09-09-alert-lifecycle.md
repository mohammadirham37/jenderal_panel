# Alert Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make alert rules configurable, target-aware, deduplicated, duration-sensitive, and automatically recoverable.

**Architecture:** An idempotent companion table stores alert targets without changing existing rule rows. Partial-update DTOs protect stored fields, while the checker receives narrow service-status and certificate-list interfaces and owns an in-memory pending-duration map. Unresolved history rows are the durable source for fired state and recovery.

**Tech Stack:** Go, SQLite, chi, Svelte 5, systemd service API, existing SSL service.

**Spec:** `docs/superpowers/specs/2026-09-09-nginx-firewall-alert-notification-design.md`

## Global Constraints

- Existing alert records remain readable after migration.
- New rules default to enabled when `enabled` is omitted.
- Partial toggle requests must preserve all other fields.
- Notifications fire once on transition to alerting and once on recovery.
- Target runtime is Ubuntu 24.04.

---

### Task 1: Persist alert targets safely

**Files:**
- Create: `internal/database/migrations/018_alert_rule_targets.sql`
- Modify: `internal/database/migrations_test.go`
- Modify: `internal/model/models.go`
- Modify: `internal/alert/service.go`
- Modify: `internal/alert/service_test.go`

**Interfaces:**
- Produces: `model.AlertRule.Target string` serialized as `target`.
- Produces: `(*Service).FindUnresolvedByRule(ctx, ruleID) (model.AlertEvent, bool, error)`.

- [ ] **Step 1: Write failing migration and service tests**

Assert migration from the existing schema creates `alert_rule_targets`, old rows scan with an empty target, new rows round-trip a target, and `FindUnresolvedByRule` returns only the newest unresolved event for a rule.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/database ./internal/alert -run 'Test.*(Target|Unresolved)' -v`

Expected: FAIL because the column and lookup do not exist.

- [ ] **Step 3: Add the migration and storage fields**

Use this idempotent migration:

```sql
CREATE TABLE IF NOT EXISTS alert_rule_targets (
    rule_id TEXT PRIMARY KEY REFERENCES alert_rules(id) ON DELETE CASCADE,
    target TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alert_history_rule_resolved
ON alert_history(rule_id, resolved, created_at DESC);
```

Write targets with `INSERT ... ON CONFLICT(rule_id) DO UPDATE`, delete the
companion row when target is empty, and load targets with a `LEFT JOIN` plus
`COALESCE(t.target, '')` in every alert rule SELECT and scanner.
Implement unresolved lookup with `WHERE rule_id=? AND resolved=0 ORDER BY created_at DESC LIMIT 1`.

- [ ] **Step 4: Run persistence tests**

Run: `go test ./internal/database ./internal/alert -v`

Expected: PASS.

- [ ] **Step 5: Commit persistence**

```bash
git add internal/database/migrations/018_alert_rule_targets.sql internal/database/migrations_test.go internal/model/models.go internal/alert/service.go internal/alert/service_test.go
git commit -m "feat: persist alert rule targets"
```

### Task 2: Create and partial-update contracts

**Files:**
- Modify: `internal/alert/handler.go`
- Modify: `internal/alert/service.go`
- Create: `internal/alert/handler_test.go`
- Modify: `internal/api/router.go`

**Interfaces:**
- Produces: `CreateRuleRequest` with `Enabled *bool`.
- Produces: `UpdateRuleRequest` with pointer fields for `Metric`, `Operator`, `Threshold`, `DurationS`, `Target`, and `Enabled`.
- Produces: plural route aliases sharing canonical handlers.

- [ ] **Step 1: Write failing handler/service tests**

Test that `{ "metric":"cpu", "operator":"gt", "threshold":80 }` creates an enabled rule, `{ "enabled":false }` preserves every other field, negative duration is rejected, `service_down` and `ssl_expiry` require targets, and system metrics clear an irrelevant target.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/alert -run 'Test(CreateDefaultsEnabled|PartialUpdate|TargetValidation)' -v`

Expected: FAIL under the current full-model decoder.

- [ ] **Step 3: Implement request DTOs and validation**

Decode create/update DTOs in the handler. On update, load the stored rule first,
apply only non-nil fields, validate the merged result, and persist it. Accept
metrics `cpu`, `ram`, `disk`, `load1`, `load5`, `load15`, `service_down`, and
`ssl_expiry`; accept operators `gt`, `lt`, and `eq`; require `duration_s >= 0`.

Register `/alerts/rules`, `/alerts/rules/{id}`, and `/alerts/history` aliases
with the same permissions and handlers as the canonical routes.

- [ ] **Step 4: Run API tests**

Run: `go test ./internal/alert ./internal/api -v`

Expected: PASS.

- [ ] **Step 5: Commit API contracts**

```bash
git add internal/alert/handler.go internal/alert/handler_test.go internal/alert/service.go internal/api/router.go
git commit -m "fix: support partial alert rule updates"
```

### Task 3: Target probes and alert lifecycle

**Files:**
- Modify: `internal/alert/checker.go`
- Create: `internal/alert/checker_test.go`
- Modify: `cmd/jenderal/main.go`

**Interfaces:**
- Consumes: `serviceMgr.Status(ctx, name)` and `sslSvc.List(ctx)` through narrow interfaces.
- Produces: `NewChecker(alertSvc, notifSvc, getMetrics, serviceStatusProvider, certificateProvider)`.
- Produces: `(*Checker).Check(ctx, now)` for deterministic tests and ticker use.

- [ ] **Step 1: Write failing evaluator and lifecycle tests**

Cover CPU evaluation, inactive service value `1`, active service value `0`, SSL days remaining, missing target errors, duration not-yet-fired, duration reached, duplicate suppression, and recovery resolving the prior event and sending exactly one recovery message.

- [ ] **Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/alert -run 'TestChecker' -v`

Expected: FAIL because target probes, duration, deduplication, and recovery do not exist.

- [ ] **Step 3: Implement deterministic checking**

Add these dependency interfaces:

```go
type ServiceStatusProvider interface {
    Status(context.Context, string) (*model.ServiceStatus, error)
}
type CertificateProvider interface {
    List(context.Context) ([]model.SSLCertificate, error)
}
```

Keep `pendingSince map[string]time.Time` under a mutex. For each enabled rule,
calculate its value, evaluate the condition, and query unresolved state. Start
or retain pending time on violation; fire only after duration. On recovery,
clear pending state, resolve the open event, and send `Resolved: ...`. Run one
check immediately before starting the 60-second ticker.

- [ ] **Step 4: Wire real providers and verify**

Pass `serviceMgr` and `sslSvc` from `cmd/jenderal/main.go`.

Run: `go test ./internal/alert ./cmd/jenderal -v`

Expected: PASS.

- [ ] **Step 5: Commit lifecycle behavior**

```bash
git add internal/alert/checker.go internal/alert/checker_test.go cmd/jenderal/main.go
git commit -m "feat: evaluate and recover targeted alerts"
```

### Task 4: Alert rule UI

**Files:**
- Modify: `web/src/routes/alerts/+page.svelte`
- Create: `web/tests/alerts/Alerts.test.mjs`
- Modify: `web/package.json`

**Interfaces:**
- Consumes: canonical `/api/v1/alert-rules`, `/api/v1/alert-history`, `/api/v1/services`, and `/api/v1/ssl` responses.

- [ ] **Step 1: Write a failing source-level UI test**

Assert the page uses canonical alert endpoints, includes load metrics and
`duration_s`, binds `target`, fetches services/SSL data, and sends numeric
threshold/duration values.

- [ ] **Step 2: Run the test and confirm failure**

Run: `cd web && node --test tests/alerts/Alerts.test.mjs`

Expected: FAIL because targets and duration controls are absent.

- [ ] **Step 3: Implement target-aware create/edit forms**

Show a service selector for `service_down`, a domain selector for
`ssl_expiry`, and no target field for system metrics. Include duration in both
create and edit payloads, display target and duration columns, use canonical
paths, and retain toggle-only updates.

- [ ] **Step 4: Register and verify frontend tests**

Add `tests/alerts/*.test.mjs` to `web/package.json`.

Run: `cd web && npm test && npm run check && npm run build`

Expected: all commands exit 0.

- [ ] **Step 5: Commit the Alert UI**

```bash
git add web/src/routes/alerts/+page.svelte web/tests/alerts/Alerts.test.mjs web/package.json
git commit -m "feat: add target-aware alert rules"
```
