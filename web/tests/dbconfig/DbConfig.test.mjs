import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyDbConfig, buildConfigFromForm, dbConfigFields, dbConfigOptions, parseDbConfig } from '../../src/lib/db-config.js';

const mysqlConfig = `[mysqld]
user		= mysql
pid-file	= /var/run/mysqld/mysqld.pid
port		= 3306
bind-address		= 127.0.0.1
max_allowed_packet	= 16M
skip-external-locking

[client]
port		= 3306
`;

const postgresConfig = `# -----------------------------
# PostgreSQL configuration file
# -----------------------------
listen_addresses = 'localhost'
port = 5432
#max_connections = 100
max_connections = 100
#work_mem = 4MB
`;

test('parses mysql values only from the [mysqld] section', () => {
	const { values, errors } = parseDbConfig(mysqlConfig, 'mysql');
	assert.equal(values.port, '3306');
	assert.equal(values['bind-address'], '127.0.0.1');
	assert.equal(values.max_allowed_packet, '16M');
	// skip-external-locking is a bare flag (ON) but not a managed field.
	assert.equal(values['skip-external-locking'], undefined);
	// [client] section values must not leak into the form.
	assert.deepEqual(errors, []);
});

test('applies a mysql directive in place and preserves unrelated lines', () => {
	const next = applyDbConfig(mysqlConfig, 'mysql', 'max_allowed_packet', '64M');
	assert.match(next, /^max_allowed_packet\s*=\s*64M$/m);
	assert.match(next, /pid-file\s*=\s*\/var\/run\/mysqld\/mysqld\.pid/);
	assert.match(next, /\[client\]/);
	// The old value line is gone.
	assert.equal(next.match(/^max_allowed_packet\s*=\s*16M$/m), null);
});

test('inserts a mysql directive after the [mysqld] header when missing', () => {
	const next = applyDbConfig(mysqlConfig, 'mysql', 'max_connections', '500');
	const lines = next.split('\n');
	const header = lines.findIndex((line) => line.trim().toLowerCase() === '[mysqld]');
	assert.equal(lines[header + 1], 'max_connections = 500');
});

test('appends a [mysqld] section when the config has none', () => {
	const next = applyDbConfig('# minimal\n', 'mysql', 'max_connections', '500');
	assert.match(next, /\[mysqld\]\nmax_connections = 500\n$/);
});

test('removes a mysql directive when the value is emptied', () => {
	const next = applyDbConfig(mysqlConfig, 'mysql', 'bind-address', '');
	assert.doesNotMatch(next, /bind-address/);
	assert.match(next, /\[mysqld\]/);
});

test('rejects mysql values that fail the pattern', () => {
	assert.throws(() => applyDbConfig(mysqlConfig, 'mysql', 'max_connections', 'many'));
	assert.throws(() => applyDbConfig(mysqlConfig, 'mysql', 'innodb_buffer_pool_size', 'huge'));
});

test('parses postgresql active values and ignores commented defaults', () => {
	const { values, errors } = parseDbConfig(postgresConfig, 'postgresql');
	assert.equal(values.listen_addresses, 'localhost');
	assert.equal(values.max_connections, '100');
	assert.equal(values.work_mem, undefined);
	assert.deepEqual(errors, []);
});

test('updates a postgresql directive in place', () => {
	const next = applyDbConfig(postgresConfig, 'postgresql', 'max_connections', '300');
	assert.match(next, /^max_connections = 300$/m);
	assert.equal(next.match(/^max_connections = 100$/m), null);
});

test('inserts a postgresql directive after its commented template default', () => {
	const next = applyDbConfig(postgresConfig, 'postgresql', 'work_mem', '8MB');
	const lines = next.split('\n');
	const comment = lines.findIndex((line) => line.trim() === '#work_mem = 4MB');
	assert.equal(lines[comment + 1], 'work_mem = 8MB');
});

test('appends a postgresql directive when no default exists', () => {
	const next = applyDbConfig(postgresConfig, 'postgresql', 'random_page_cost', '1.5');
	assert.match(next, /random_page_cost = 1\.5\n?$/);
});

test('removes a postgresql directive when the value is emptied', () => {
	const next = applyDbConfig(postgresConfig, 'postgresql', 'max_connections', '');
	assert.doesNotMatch(next, /^max_connections =/m);
	// The commented template default stays for reference.
	assert.match(next, /#max_connections = 100/);
});

test('builds config from form values touching only changed directives', () => {
	const { values } = parseDbConfig(mysqlConfig, 'mysql');
	const next = buildConfigFromForm(mysqlConfig, 'mysql', values, {
		...values,
		max_connections: '500',
		slow_query_log: 'ON'
	});
	assert.match(next, /max_connections = 500/);
	assert.match(next, /slow_query_log = ON/);
	// Untouched directives keep their original formatting.
	assert.match(next, /port\s*=\s*3306/);
});

test('select options come from presets and keep unknown current values', () => {
	const toggle = dbConfigFields.mysql.slow_query_log;
	assert.deepEqual(dbConfigOptions(toggle, ''), ['ON', 'OFF']);
	assert.deepEqual(dbConfigOptions(toggle, 'OFF'), ['ON', 'OFF']);
	// A value set outside the panel (e.g. 1) must stay selectable.
	assert.deepEqual(dbConfigOptions(toggle, '1'), ['1', 'ON', 'OFF']);
	// Free-input fields (port) have no presets and stay text inputs.
	assert.equal(dbConfigOptions(dbConfigFields.mysql.port, '3306'), null);
});
