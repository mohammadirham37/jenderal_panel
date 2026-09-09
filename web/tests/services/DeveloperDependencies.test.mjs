import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

let composerActionPath;
try {
	({ composerActionPath } = await import('../../src/lib/developer-dependencies.js'));
} catch {
	// The first TDD run reaches the assertion before the helper exists.
}

test('selects install only when Composer is absent', () => {
	assert.equal(typeof composerActionPath, 'function', 'expected Composer action helper');
	assert.equal(
		composerActionPath({ name: 'composer', installed: false, version: '' }),
		'/api/v1/services/composer/install'
	);
});

test('selects update when Composer is installed', () => {
	assert.equal(typeof composerActionPath, 'function', 'expected Composer action helper');
	assert.equal(
		composerActionPath({ name: 'composer', installed: true, version: '2.10.3' }),
		'/api/v1/services/composer/update'
	);
});

test('fully refreshes the services page after a successful Composer task', () => {
	const source = readFileSync(new URL('../../src/routes/services/+page.svelte', import.meta.url), 'utf8');
	const completedBranch = source.match(/if \(task\?\.status === 'completed'\) \{([\s\S]*?)\} else \{/);
	assert.ok(completedBranch, 'expected a completed Composer task branch');
	assert.match(completedBranch[1], /window\.location\.reload\(\)/);
});
