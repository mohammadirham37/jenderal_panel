import { test } from 'node:test';
import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';

let decodeTerminalMessage;
try {
	({ decodeTerminalMessage } = await import('../../src/lib/terminal-message.js'));
} catch {
	// The first TDD run intentionally reaches the assertion before the decoder exists.
}

test('extracts command output from a terminal websocket response', () => {
	assert.equal(typeof decodeTerminalMessage, 'function', 'expected a terminal message decoder');

	const message = JSON.stringify({
		type: 'output',
		output: 'jenderal\njenderal.bak\nsource\n',
		exit_code: 0
	});

	assert.equal(decodeTerminalMessage(message), 'jenderal\njenderal.bak\nsource\n');
});

test('preserves plain text terminal messages for compatibility', () => {
	assert.equal(typeof decodeTerminalMessage, 'function', 'expected a terminal message decoder');
	assert.equal(decodeTerminalMessage('legacy output'), 'legacy output');
});

test('renders no text when a structured terminal response has no output', () => {
	assert.equal(typeof decodeTerminalMessage, 'function', 'expected a terminal message decoder');
	assert.equal(decodeTerminalMessage('{"type":"output","exit_code":1}'), '');
});

test('decodeTerminalEvent exposes structured fields for streaming output', async () => {
	const { decodeTerminalEvent } = await import('../../src/lib/terminal-message.js');

	const event = decodeTerminalEvent(
		JSON.stringify({ type: 'output', output: 'chunk', partial: true })
	);
	assert.deepEqual(event, {
		type: 'output',
		output: 'chunk',
		exitCode: null,
		cwd: null,
		partial: true
	});
});

test('decodeTerminalEvent carries exit code and working directory', async () => {
	const { decodeTerminalEvent } = await import('../../src/lib/terminal-message.js');

	const event = decodeTerminalEvent(
		JSON.stringify({ type: 'output', exit_code: 2, cwd: '/var/www/app' })
	);
	assert.equal(event.exitCode, 2);
	assert.equal(event.cwd, '/var/www/app');
	assert.equal(event.partial, false);
	assert.equal(event.output, '');
});

test('decodeTerminalEvent reports errors as events', async () => {
	const { decodeTerminalEvent } = await import('../../src/lib/terminal-message.js');

	const event = decodeTerminalEvent(
		JSON.stringify({ type: 'error', output: 'session timed out' })
	);
	assert.equal(event.type, 'error');
	assert.equal(event.output, 'session timed out');
});

test('decodeTerminalEvent returns null for plain text and garbage', async () => {
	const { decodeTerminalEvent } = await import('../../src/lib/terminal-message.js');

	assert.equal(decodeTerminalEvent('legacy output'), null);
	assert.equal(decodeTerminalEvent('not json {'), null);
	assert.equal(decodeTerminalEvent(JSON.stringify({ type: 'other' })), null);
});

test('TerminalConsole implements the persistent shell protocol', async () => {
	const consoleComponent = await readFile(
		new URL('../../src/lib/components/TerminalConsole.svelte', import.meta.url),
		'utf8'
	);
	assert.match(consoleComponent, /decodeTerminalEvent/);
	assert.match(consoleComponent, /ev\.partial/);
	assert.match(consoleComponent, /ev\.exitCode/);
	assert.match(consoleComponent, /ev\.cwd/);
});

test('the standalone page and website tab both render TerminalConsole', async () => {
	const page = await readFile(
		new URL('../../src/routes/terminal/+page.svelte', import.meta.url),
		'utf8'
	);
	const websitePage = await readFile(
		new URL('../../src/routes/websites/[id]/+page.svelte', import.meta.url),
		'utf8'
	);

	assert.match(page, /<TerminalConsole \{endpoint\}/);
	// The website console points at the website-scoped endpoint and only
	// holds a session while its tab is active.
	assert.match(websitePage, /<TerminalConsole/);
	assert.match(websitePage, /ws\/terminal\?web_user=/);
	assert.match(websitePage, /active=\{activeTab === 'Terminal'\}/);
	assert.doesNotMatch(websitePage, /decodeTerminalMessage/);
});
