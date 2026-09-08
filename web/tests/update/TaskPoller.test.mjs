import { test } from 'node:test';
import assert from 'node:assert/strict';

let createTaskPoller;
try {
	({ createTaskPoller } = await import('../../src/lib/task-poller.js'));
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
			if (attempts === 1) throw new Error('temporarily unavailable');
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
