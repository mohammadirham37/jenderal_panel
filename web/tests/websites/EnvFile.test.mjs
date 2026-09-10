import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseEnvFile, applyEnvValues } from '../../src/lib/env-file.js';

test('parseEnvFile extracts simple assignments and skips comments and blanks', () => {
	const raw = [
		'APP_NAME=Laravel',
		'# a comment',
		'',
		'  INDENTED_KEY=spaced',
		'APP_ENV=local',
		'BROKEN LINE'
	].join('\n');

	assert.deepEqual(parseEnvFile(raw), [
		{ key: 'APP_NAME', value: 'Laravel', line: 0 },
		{ key: 'INDENTED_KEY', value: 'spaced', line: 3 },
		{ key: 'APP_ENV', value: 'local', line: 4 }
	]);
});

test('applyEnvValues replaces values in place and preserves everything else', () => {
	const raw = 'APP_NAME="My App"\n# keep\nAPP_ENV=local\nQUEUE_CONNECTION=sync\n';
	const result = applyEnvValues(raw, {
		APP_NAME: 'Panel App',
		QUEUE_CONNECTION: 'redis',
		MISSING_KEY: 'ignored'
	});

	assert.equal(result, 'APP_NAME=Panel App\n# keep\nAPP_ENV=local\nQUEUE_CONNECTION=redis\n');
});

test('applyEnvValues keeps special characters in replacement values literal', () => {
	const raw = 'APP_KEY=\nBASE_VALUE=a&b$c\n';
	const result = applyEnvValues(raw, {
		APP_KEY: 'base64:$&xyz',
		BASE_VALUE: '1=2&3'
	});

	assert.equal(result, 'APP_KEY=base64:$&xyz\nBASE_VALUE=1=2&3\n');
});
