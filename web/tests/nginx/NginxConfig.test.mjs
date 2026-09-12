import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

let parseSimpleNginxConfig;
let switchNginxConfigMode;
let updateSimpleNginxConfig;
try {
	({ parseSimpleNginxConfig, switchNginxConfigMode, updateSimpleNginxConfig } = await import('../../src/lib/nginx-config.js'));
} catch {
	// The first TDD run intentionally reaches the assertions before the helper exists.
}

const config = `worker_processes auto;
# worker_processes 8;
events {
    worker_connections 768;
}
http {
    client_max_body_size 64M;
    keepalive_timeout 65;
    server_tokens off;
    gzip on;
    server { gzip off; }
}
`;

test('reads managed directives only from their nginx contexts', () => {
	assert.equal(typeof parseSimpleNginxConfig, 'function', 'expected an nginx config parser');
	assert.deepEqual(parseSimpleNginxConfig(config), {
		values: {
			worker_processes: 'auto',
			worker_connections: '768',
			client_max_body_size: '64M',
			keepalive_timeout: '65',
			server_tokens: 'off',
			gzip: 'on'
		},
		errors: []
	});
});

test('updates and inserts directives without changing unrelated content', () => {
	assert.equal(typeof updateSimpleNginxConfig, 'function', 'expected an nginx config updater');
	const replaced = updateSimpleNginxConfig(config, 'gzip', 'off');
	assert.match(replaced, /\n    gzip off;\n/);
	assert.match(replaced, /server \{ gzip off; \}/);

	const inserted = updateSimpleNginxConfig('events { worker_connections 512; }\nhttp {\n}\n', 'client_max_body_size', '32M');
	assert.match(inserted, /http \{\n\s+client_max_body_size 32M;\n/);
});

test('rejects invalid values and missing structural contexts', () => {
	assert.throws(() => updateSimpleNginxConfig(config, 'worker_connections', '0'), /invalid worker_connections/i);
	assert.throws(() => updateSimpleNginxConfig('events {}\n', 'gzip', 'on'), /http block/i);
	const parsed = parseSimpleNginxConfig(config.replace('gzip on;', 'gzip invalid;'));
	assert.equal(parsed.values.gzip, 'invalid');
	assert.ok(parsed.errors.some((message) => /invalid gzip/i.test(message)), 'existing invalid directives must disable Simple-mode save');
});

test('reads and replaces directives written inline after a block brace', () => {
	const inline = 'worker_processes auto;\nevents { worker_connections 1024; }\nhttp { gzip on; }\n';
	assert.equal(parseSimpleNginxConfig(inline).values.worker_connections, '1024');
	const updated = updateSimpleNginxConfig(inline, 'worker_connections', '2048');
	assert.match(updated, /events \{ worker_connections 2048; \}/);
	assert.equal((updated.match(/worker_connections/g) || []).length, 1);
});

test('mode switches preserve the current unsaved draft', () => {
	assert.equal(typeof switchNginxConfigMode, 'function', 'expected an nginx mode switcher');
	const state = { mode: 'manual', draft: config };
	assert.equal(switchNginxConfigMode(state, 'simple').draft, config);
	assert.equal(switchNginxConfigMode({ mode: 'simple', draft: config }, 'simple').draft, config);
});

test('nginx page exposes simple and manual modes through the shared helper', async () => {
	const source = await readFile(new URL('../../src/routes/nginx/+page.svelte', import.meta.url), 'utf8');
	assert.match(source, /from '\$lib\/nginx-config\.js'/);
	assert.match(source, />\{translate\(\$language, 'ngx\.simple'\)\}</);
	assert.match(source, />\{translate\(\$language, 'ngx\.manual'\)\}</);
	for (const field of ['worker_processes', 'worker_connections', 'client_max_body_size', 'keepalive_timeout', 'server_tokens', 'gzip']) {
		assert.match(source, new RegExp(field));
	}
});
