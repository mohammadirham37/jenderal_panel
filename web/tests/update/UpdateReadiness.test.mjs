import { test } from 'node:test';
import assert from 'node:assert/strict';

let cacheBustedURL;
let waitForUpdatedPanel;
try {
	({ cacheBustedURL, waitForUpdatedPanel } = await import('../../src/lib/update-readiness.js'));
} catch {
	// The first TDD run reaches the assertions before the helper exists.
}

test('waits through restart errors and stale versions until the new panel responds', async () => {
	assert.equal(typeof waitForUpdatedPanel, 'function', 'expected update readiness helper');

	const responses = [
		new Error('service restarting'),
		{ current_version: 'aaaaaaaa', latest_version: 'bbbbbbbb', update_available: true },
		{ current_version: 'bbbbbbbb', latest_version: 'bbbbbbbb', update_available: false }
	];
	let delays = 0;
	const ready = await waitForUpdatedPanel({
		expectedVersion: 'bbbbbbbb',
		check: async () => {
			const response = responses.shift();
			if (response instanceof Error) throw response;
			return response;
		},
		delay: async () => {
			delays += 1;
		},
		maxAttempts: 3
	});

	assert.equal(ready, true);
	assert.equal(delays, 2);
});

test('returns false after the new panel version never becomes ready', async () => {
	assert.equal(typeof waitForUpdatedPanel, 'function', 'expected update readiness helper');

	let attempts = 0;
	const ready = await waitForUpdatedPanel({
		expectedVersion: 'bbbbbbbb',
		check: async () => {
			attempts += 1;
			return { current_version: 'aaaaaaaa', latest_version: 'bbbbbbbb', update_available: true };
		},
		delay: async () => {},
		maxAttempts: 3
	});

	assert.equal(ready, false);
	assert.equal(attempts, 3);
});

test('builds a full reload URL without discarding existing query parameters', () => {
	assert.equal(typeof cacheBustedURL, 'function', 'expected cache-busted URL helper');

	assert.equal(
		cacheBustedURL('https://panel.test/update?foo=1', '123'),
		'https://panel.test/update?foo=1&_panel_reload=123'
	);
});
