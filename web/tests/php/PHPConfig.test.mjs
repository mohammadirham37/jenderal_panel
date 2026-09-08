import { test } from 'node:test';
import assert from 'node:assert/strict';

let parseSimplePHPConfig;
let switchPHPConfigMode;
let updateSimplePHPConfig;
try {
	({ parseSimplePHPConfig, switchPHPConfigMode, updateSimplePHPConfig } = await import('../../src/lib/php-config.js'));
} catch {
	// The first TDD run intentionally reaches the assertions before the helper exists.
}

test('reads common active directives from php.ini without using commented examples', () => {
	assert.equal(typeof parseSimplePHPConfig, 'function', 'expected a PHP config parser');

	const content = `; memory_limit = 64M
memory_limit = 256M
upload_max_filesize = 32M
post_max_size=40M
max_execution_time = 120
max_input_time = -1
max_input_vars = 3000
display_errors = Off
`;

	assert.deepEqual(parseSimplePHPConfig(content), {
		memory_limit: '256M',
		upload_max_filesize: '32M',
		post_max_size: '40M',
		max_execution_time: '120',
		max_input_time: '-1',
		max_input_vars: '3000',
		display_errors: 'Off'
	});
});

test('updates simple directives while preserving unrelated php.ini content', () => {
	assert.equal(typeof updateSimplePHPConfig, 'function', 'expected a PHP config updater');

	const content = `; production settings
memory_limit = 128M
upload_max_filesize = 2M
post_max_size = 8M
max_execution_time = 30
max_input_time = 60
max_input_vars = 1000
display_errors = Off
date.timezone = Asia/Jakarta
`;
	const values = {
		memory_limit: '512M',
		upload_max_filesize: '64M',
		post_max_size: '64M',
		max_execution_time: '180',
		max_input_time: '120',
		max_input_vars: '5000',
		display_errors: 'On'
	};

	assert.equal(
		updateSimplePHPConfig(content, values),
		`; production settings
memory_limit = 512M
upload_max_filesize = 64M
post_max_size = 64M
max_execution_time = 180
max_input_time = 120
max_input_vars = 5000
display_errors = On
date.timezone = Asia/Jakarta
`
	);
});

test('appends a simple directive when php.ini does not define it', () => {
	assert.equal(typeof updateSimplePHPConfig, 'function', 'expected a PHP config updater');

	const updated = updateSimplePHPConfig("memory_limit = 128M\n", {
		memory_limit: '256M',
		upload_max_filesize: '16M',
		post_max_size: '20M',
		max_execution_time: '60',
		max_input_time: '60',
		max_input_vars: '2000',
		display_errors: 'Off'
	});

	assert.match(updated, /memory_limit = 256M/);
	assert.match(updated, /upload_max_filesize = 16M/);
	assert.match(updated, /display_errors = Off/);
});

test('clicking the active Simple mode preserves unsaved form values', () => {
	assert.equal(typeof switchPHPConfigMode, 'function', 'expected a PHP config mode switcher');

	const state = {
		mode: 'simple',
		content: 'memory_limit = 128M\n',
		simpleConfig: { ...parseSimplePHPConfig(''), memory_limit: '512M' }
	};
	const result = switchPHPConfigMode(state, 'simple');

	assert.equal(result.simpleConfig.memory_limit, '512M');
	assert.equal(result.content, 'memory_limit = 128M\n');
});

test('clicking the active Advanced mode preserves unsaved raw content', () => {
	assert.equal(typeof switchPHPConfigMode, 'function', 'expected a PHP config mode switcher');

	const state = {
		mode: 'advanced',
		content: 'memory_limit = 768M\ndate.timezone = Asia/Jakarta\n',
		simpleConfig: parseSimplePHPConfig('memory_limit = 128M\n')
	};
	const result = switchPHPConfigMode(state, 'advanced');

	assert.equal(result.content, 'memory_limit = 768M\ndate.timezone = Asia/Jakarta\n');
});
