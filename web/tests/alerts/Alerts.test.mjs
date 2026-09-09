import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

test('alert page uses canonical routes and target-aware rule fields', async () => {
	const source = await readFile(new URL('../../src/routes/alerts/+page.svelte', import.meta.url), 'utf8');
	assert.match(source, /\/api\/v1\/alert-rules/);
	assert.match(source, /\/api\/v1\/alert-history/);
	assert.match(source, /duration_s/);
	assert.match(source, /target/);
	assert.match(source, /load1/);
	assert.match(source, /\/api\/v1\/services/);
	assert.match(source, /\/api\/v1\/ssl/);
});
