import test from 'node:test';
import assert from 'node:assert/strict';
import { countExtensions, groupPHPVersions, listExtensions, websitesByPHPVersion } from '../../src/lib/php-versions.js';
import { parseIniSize, phpConfigIsDirty, phpConfigPresets, phpConfigWarnings } from '../../src/lib/php-config.js';

test('groupPHPVersions splits installed and available, newest first', () => {
	const grouped = groupPHPVersions([
		{ version: '8.1', installed: true, running: false },
		{ version: '8.10', installed: false, running: false },
		{ version: '8.3', installed: true, running: true },
		{ version: '7.4', installed: false, running: false }
	]);
	assert.deepEqual(grouped.installed.map((v) => v.version), ['8.3', '8.1']);
	assert.deepEqual(grouped.available.map((v) => v.version), ['8.10', '7.4']);
	assert.equal(grouped.running, 1);
	assert.equal(grouped.stopped, 1);
	assert.equal(grouped.newest, '8.10');
});

test('websitesByPHPVersion ignores static and proxy sites', () => {
	const usage = websitesByPHPVersion([
		{ domain: 'b.test', app_type: 'php', php_version: '8.3' },
		{ domain: 'a.test', app_type: 'laravel', php_version: '8.3' },
		{ domain: 'static.test', app_type: 'static', php_version: '8.3' },
		{ domain: 'proxy.test', app_type: 'reverse-proxy', php_version: '8.2' }
	]);
	assert.deepEqual(usage, { '8.3': ['a.test', 'b.test'] });
});

test('listExtensions flattens, filters by state and query, and sorts', () => {
	const status = { enabled: ['mbstring', 'curl'], disabled: ['xdebug'], available: ['redis', 'imagick'] };
	assert.deepEqual(listExtensions(status).map((e) => e.name), ['curl', 'imagick', 'mbstring', 'redis', 'xdebug']);
	assert.deepEqual(listExtensions(status, { state: 'available' }), [{ name: 'imagick', state: 'available' }, { name: 'redis', state: 'available' }]);
	assert.deepEqual(listExtensions(status, { query: ' RE ' }), [{ name: 'redis', state: 'available' }]);
	assert.deepEqual(countExtensions(status), { all: 5, enabled: 2, disabled: 1, available: 2 });
	assert.deepEqual(listExtensions(null), []);
});

test('parseIniSize understands php.ini shorthand', () => {
	assert.equal(parseIniSize('128M'), 128 * 1024 * 1024);
	assert.equal(parseIniSize('1g'), 1024 ** 3);
	assert.equal(parseIniSize('2048'), 2048);
	assert.equal(parseIniSize('-1'), null);
	assert.equal(parseIniSize('lots'), null);
});

test('phpConfigWarnings flags inconsistent limits', () => {
	assert.deepEqual(phpConfigWarnings(phpConfigPresets.wordpress), []);
	assert.deepEqual(
		phpConfigWarnings({ ...phpConfigPresets.default, upload_max_filesize: '64M', post_max_size: '8M', memory_limit: '4M', display_errors: 'On' }),
		['phpv.config.warnPostBelowUpload', 'phpv.config.warnMemoryBelowPost', 'phpv.config.warnDisplayErrors']
	);
	// Unlimited memory never warns.
	assert.deepEqual(phpConfigWarnings({ ...phpConfigPresets.uploads, memory_limit: '-1' }), []);
});

test('phpConfigIsDirty compares the editor with the saved php.ini', () => {
	const saved = 'memory_limit = 128M\n';
	const simple = { ...phpConfigPresets.default };
	assert.equal(phpConfigIsDirty({ mode: 'simple', content: saved, simpleConfig: simple }, saved), false);
	assert.equal(phpConfigIsDirty({ mode: 'simple', content: saved, simpleConfig: { ...simple, memory_limit: '256M' } }, saved), true);
	assert.equal(phpConfigIsDirty({ mode: 'advanced', content: saved, simpleConfig: simple }, saved), false);
	assert.equal(phpConfigIsDirty({ mode: 'advanced', content: saved + 'x = 1\n', simpleConfig: simple }, saved), true);
});

test('summarizePatches groups vulnerable, updatable, EOL, and stuck versions', async () => {
	const { summarizePatches, listsAgeHours } = await import('../../src/lib/php-versions.js');
	const s = summarizePatches([
		{ version: '8.1', update_available: false, vulnerable: false, end_of_life: true },
		{ version: '8.3', update_available: true, vulnerable: true, end_of_life: false },
		{ version: '8.2', update_available: false, vulnerable: true, end_of_life: false },
		{ version: '8.4', update_available: true, vulnerable: false, end_of_life: false }
	]);
	assert.deepEqual(s.vulnerable.map((v) => v.version), ['8.3', '8.2']);
	assert.deepEqual(s.updatable.map((v) => v.version), ['8.3', '8.4']);
	assert.deepEqual(s.endOfLife.map((v) => v.version), ['8.1']);
	assert.deepEqual(s.stuck.map((v) => v.version), ['8.2']);
	assert.equal(listsAgeHours('2026-10-21T00:00:00Z', Date.parse('2026-10-22T06:30:00Z')), 30);
	assert.equal(listsAgeHours(null), null);
});
