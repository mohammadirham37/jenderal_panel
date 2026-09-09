import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

let createFileManagerAPI;
try {
	({ createFileManagerAPI } = await import('../../src/lib/file-manager.js'));
} catch {}

function recordingAPI() {
	const calls = [];
	return {
		calls,
		client: {
			get(path) { calls.push(['GET', path]); },
			post(path, body) { calls.push(['POST', path, body]); },
			del(path) { calls.push(['DELETE', path]); }
		}
	};
}

test('file manager client matches the backend route contract', async () => {
	assert.equal(typeof createFileManagerAPI, 'function');
	const { calls, client } = recordingAPI();
	const files = createFileManagerAPI(client, 'site-1');

	await files.browse('/public assets');
	await files.read('/public/index.php');
	await files.write('/public/index.php', '<?php');
	await files.remove('/public/old.php');
	await files.mkdir('/public/cache');
	await files.rename('/public/a.txt', '/public/b.txt');

	assert.deepEqual(calls, [
		['GET', '/api/v1/websites/site-1/files?path=%2Fpublic%20assets'],
		['GET', '/api/v1/websites/site-1/files/read?path=%2Fpublic%2Findex.php'],
		['POST', '/api/v1/websites/site-1/files/write', { path: '/public/index.php', content: '<?php' }],
		['DELETE', '/api/v1/websites/site-1/files?path=%2Fpublic%2Fold.php'],
		['POST', '/api/v1/websites/site-1/files/mkdir', { path: '/public/cache' }],
		['POST', '/api/v1/websites/site-1/files/rename', { old_path: '/public/a.txt', new_path: '/public/b.txt' }]
	]);
});

test('file uploads send the CSRF token required by the API middleware', async () => {
	const page = await readFile(new URL('../../src/routes/websites/[id]/+page.svelte', import.meta.url), 'utf8');
	assert.match(page, /import \{ api, getCSRFToken \} from '\$lib\/api'/);
	assert.match(page, /'X-CSRF-Token': getCSRFToken\(\)/);
});
