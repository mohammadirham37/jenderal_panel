import { test } from 'node:test';
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
