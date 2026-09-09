# Firewall Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Firewall status and all rule actions work against the existing UFW backend contract.

**Architecture:** The backend treats non-zero UFW status commands as domain errors. A small frontend helper normalizes the direct status response, converts form ports to numbers, and classifies compound UFW actions for presentation.

**Tech Stack:** Go, Svelte 5, JavaScript ES modules, Node test runner, UFW.

**Spec:** `docs/superpowers/specs/2026-09-09-nginx-firewall-alert-notification-design.md`

## Global Constraints

- Preserve SSH deletion warnings and force-confirmation behavior.
- Use the existing direct `{active, default, rules}` API response.
- Accept only ports 1 through 65535.
- Target runtime is Ubuntu 24.04.

---

### Task 1: Report UFW status failures

**Files:**
- Modify: `internal/firewall/service.go`
- Modify: `internal/firewall/service_test.go`

**Interfaces:**
- Produces: `(*Service).Status(context.Context) (*model.FirewallStatus, error)` returning `UFW_ERROR` when `ExitCode != 0`.

- [ ] **Step 1: Write a failing status-command test**

Add a test executor result with `ExitCode: 1` and `Stderr: "ufw: command not found"`, then assert `Status` returns an error containing the safe stderr text instead of an inactive status.

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `go test ./internal/firewall -run TestStatusReturnsCommandFailure -v`

Expected: FAIL because `Status` currently ignores `ExitCode`.

- [ ] **Step 3: Implement the exit-code check**

After `RunSudo`, add:

```go
if result.ExitCode != 0 {
    message := strings.TrimSpace(result.Stderr)
    if message == "" {
        message = "failed to read firewall status"
    }
    return nil, model.NewDomainError("UFW_ERROR", message, nil)
}
```

- [ ] **Step 4: Run firewall tests**

Run: `go test ./internal/firewall -v`

Expected: PASS.

- [ ] **Step 5: Commit the backend fix**

```bash
git add internal/firewall/service.go internal/firewall/service_test.go
git commit -m "fix: report ufw status failures"
```

### Task 2: Normalize Firewall UI data and actions

**Files:**
- Create: `web/src/lib/firewall.js`
- Create: `web/tests/firewall/Firewall.test.mjs`
- Modify: `web/src/routes/firewall/+page.svelte`
- Modify: `web/package.json`

**Interfaces:**
- Produces: `normalizeFirewallStatus(data)` returning `{active, defaultPolicy, rules}`.
- Produces: `parseFirewallPort(value)` returning an integer or throwing a validation error.
- Produces: `firewallActionTone(action)` returning `allow`, `deny`, `limit`, or `neutral`.

- [ ] **Step 1: Write failing normalization tests**

```js
assert.deepEqual(normalizeFirewallStatus({ active: true, default: 'deny', rules: [] }), {
  active: true, defaultPolicy: 'deny', rules: []
});
assert.equal(parseFirewallPort('443'), 443);
assert.throws(() => parseFirewallPort('443x'), /valid port/i);
assert.equal(firewallActionTone('ALLOW IN'), 'allow');
assert.equal(firewallActionTone('REJECT IN'), 'deny');
```

- [ ] **Step 2: Run the focused test and confirm failure**

Run: `cd web && node --test tests/firewall/Firewall.test.mjs`

Expected: FAIL because the helper does not exist.

- [ ] **Step 3: Implement helper and page integration**

Load `/api/v1/firewall/status` as the direct backend object, assign its rules,
show `defaultPolicy`, and send `port: parseFirewallPort(newPort)`. Use prefix
classification so `ALLOW IN`, `DENY IN`, `REJECT IN`, and `LIMIT IN` retain
their existing colors. Reset stale load errors before retries.

- [ ] **Step 4: Register and run verification**

Add `tests/firewall/*.test.mjs` to `web/package.json`.

Run: `cd web && npm test && npm run check && npm run build`

Expected: all commands exit 0.

- [ ] **Step 5: Commit the frontend fix**

```bash
git add web/src/lib/firewall.js web/tests/firewall/Firewall.test.mjs web/src/routes/firewall/+page.svelte web/package.json
git commit -m "fix: align firewall page with ufw api"
```
