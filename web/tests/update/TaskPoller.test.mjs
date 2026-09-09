import { test } from 'node:test';
import assert from 'node:assert/strict';

let clearMissingTask;
let createTaskPoller;
try {
	({ clearMissingTask, createTaskPoller } = await import('../../src/lib/task-poller.js'));
} catch {
	// The first TDD run intentionally reaches the assertion below before the helper exists.
}

function flushPromises() {
	return new Promise((resolve) => setImmediate(resolve));
}

test('continues polling after a temporary request failure', async () => {
	assert.equal(typeof createTaskPoller, 'function', 'expected a reusable task poller');

	let scheduledPoll;
	let attempts = 0;
	const received = [];
	const stop = createTaskPoller({
		taskId: 'task-1',
		fetchTask: async () => {
			attempts += 1;
			if (attempts === 1) throw Object.assign(new Error('temporarily unavailable'), { status: 500 });
			return { status: 'running' };
		},
		onTask: (task) => received.push(task.status),
		onComplete: () => assert.fail('a running task must not complete'),
		schedule: (callback) => {
			scheduledPoll = callback;
			return 1;
		},
		cancel: () => {}
	});

	await flushPromises();
	assert.equal(attempts, 1);
	await scheduledPoll();
	assert.deepEqual(received, ['running']);
	stop();
});

test('stops polling when a task reaches a terminal state', async () => {
	assert.equal(typeof createTaskPoller, 'function', 'expected a reusable task poller');

	let cancelled = false;
	let completedWith;
	createTaskPoller({
		taskId: 'task-2',
		fetchTask: async () => ({ status: 'completed' }),
		onTask: () => {},
		onComplete: (task) => {
			completedWith = task.status;
		},
		schedule: () => 7,
		cancel: (id) => {
			assert.equal(id, 7);
			cancelled = true;
		}
	});

	await flushPromises();
	assert.equal(completedWith, 'completed');
	assert.equal(cancelled, true);
});

test('discards a stale task after a not-found response instead of retrying forever', async () => {
	assert.equal(typeof createTaskPoller, 'function', 'expected a reusable task poller');

	let scheduledPoll;
	let attempts = 0;
	let retryErrors = 0;
	let missingErrors = 0;
	let cancelled = false;
	createTaskPoller({
		taskId: 'stale-task',
		fetchTask: async () => {
			attempts += 1;
			throw Object.assign(new Error('task not found'), { status: 404 });
		},
		onTask: () => assert.fail('a missing task must not emit progress'),
		onComplete: () => assert.fail('a missing task is not a completed task'),
		onError: () => {
			retryErrors += 1;
		},
		onMissing: () => {
			missingErrors += 1;
		},
		schedule: (callback) => {
			scheduledPoll = callback;
			return 9;
		},
		cancel: (id) => {
			assert.equal(id, 9);
			cancelled = true;
		}
	});

	await flushPromises();
	await scheduledPoll();
	assert.equal(attempts, 1);
	assert.equal(retryErrors, 0);
	assert.equal(missingErrors, 1);
	assert.equal(cancelled, true);
});

test('clears missing task state and storage before notifying its owner', () => {
	assert.equal(typeof clearMissingTask, 'function', 'expected shared missing-task cleanup');

	const events = [];
	clearMissingTask({
		storageKey: 'jenderal_nodejs_task',
		storage: {
			removeItem: (key) => events.push(`remove:${key}`)
		},
		clearTask: () => events.push('clear'),
		onMissing: () => events.push('notify')
	});

	assert.deepEqual(events, ['clear', 'remove:jenderal_nodejs_task', 'notify']);
});
