# Update Button Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep Update Now available when an obsolete browser recovery marker exists without a live update task.

**Architecture:** Model persisted update state with a small pure classifier, then reconcile both storage keys after the fresh version check. TaskProgress remains the authority for a real saved task; readiness polling remains the post-completion path.

**Tech Stack:** Svelte 5, JavaScript, Node test runner.

**Spec:** Approved bounded design in the 2026-09-10 conversation; independent of `docs/superpowers/specs/2026-09-10-website-scoped-operations-design.md`.

## Global Constraints

- Do not change backend update execution or restart behavior.
- A target marker without a task marker must not hide Update Now.
- A real saved task must keep update controls locked until TaskProgress resolves it.
- A target already served by the current panel must be cleared.

---

### Task 1: Reconcile Persisted Update State

**Files:**
- Modify: `web/src/lib/update-readiness.js`
- Modify: `web/src/routes/update/+page.svelte`
- Modify: `web/tests/update/UpdateReadiness.test.mjs`

**Interfaces:**
- Produces: `classifyUpdateRecovery(currentVersion, targetVersion, taskID): 'none' | 'active' | 'complete' | 'stale'`.
- Consumes: `jenderal_update_target`, `jenderal_update_task`, and the fresh `/api/v1/update/check` response.

- [ ] **Step 1: Write the failing classifier tests**

Add literal cases proving that a target without a task is `stale`, a target with a task is `active`, and an already-served target is `complete`.

```js
assert.equal(classifyUpdateRecovery('old', 'new', ''), 'stale');
assert.equal(classifyUpdateRecovery('old', 'new', 'task-1'), 'active');
assert.equal(classifyUpdateRecovery('new', 'new', 'task-1'), 'complete');
assert.equal(classifyUpdateRecovery('old', '', ''), 'none');
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd web && node --test tests/update/UpdateReadiness.test.mjs`

Expected: FAIL because `classifyUpdateRecovery` is not exported.

- [ ] **Step 3: Implement the classifier**

```js
export function classifyUpdateRecovery(currentVersion, targetVersion, taskID) {
  if (!targetVersion) return 'none';
  if (currentVersion === targetVersion) return 'complete';
  return taskID ? 'active' : 'stale';
}
```

- [ ] **Step 4: Reconcile state only after a fresh update check**

On mount, restore both storage keys into page state and call `checkUpdate`. After the response:

```ts
const recovery = classifyUpdateRecovery(
  checked.current_version,
  expectedUpdateVersion,
  currentTaskId
);
if (recovery === 'complete' || recovery === 'stale') {
  localStorage.removeItem(updateTargetStorageKey);
  if (recovery === 'complete') localStorage.removeItem(updateTaskStorageKey);
  expectedUpdateVersion = '';
  if (recovery === 'complete') currentTaskId = '';
  updating = false;
}
```

Remove unconditional target-only `resumeUpdateReload()` from `onMount`. Keep `onTaskComplete()` readiness polling unchanged.

- [ ] **Step 5: Run focused and frontend checks**

Run: `cd web && node --test tests/update/*.test.mjs`

Expected: all update tests PASS.

Run: `cd web && npm run check`

Expected: zero errors; the existing unrelated `uploadInput` warning may remain.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/update-readiness.js web/src/routes/update/+page.svelte web/tests/update/UpdateReadiness.test.mjs
git commit -m "fix(update): clear stale recovery state"
```
