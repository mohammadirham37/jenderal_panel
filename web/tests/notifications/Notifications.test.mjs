import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

test('notification page exposes real channel forms and canonical routes', async () => {
	const source = await readFile(new URL('../../src/routes/notifications/+page.svelte', import.meta.url), 'utf8');
	assert.match(source, /\/api\/v1\/notification-channels/);
	for (const field of ['smtp_host', 'smtp_port', 'username', 'password', 'from', 'to', 'encryption', 'bot_token', 'chat_id', 'webhook_url', 'authorization']) {
		assert.match(source, new RegExp(field));
	}
	assert.match(source, /type="password"/);
	assert.match(source, /starttls/);
	assert.match(source, /tls/);
});
