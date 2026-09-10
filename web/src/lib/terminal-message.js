/**
 * Decode the structured response emitted by the terminal WebSocket while
 * remaining compatible with plain-text messages.
 *
 * @param {string} message
 * @returns {string}
 */
export function decodeTerminalMessage(message) {
	try {
		const response = JSON.parse(message);
		if (
			response &&
			typeof response === 'object' &&
			(response.type === 'output' || response.type === 'error')
		) {
			return typeof response.output === 'string' ? response.output : '';
		}
	} catch {
		// Plain-text messages are valid for compatibility with older servers.
	}

	return message;
}

/**
 * Decode a terminal WebSocket message into its full structured event so the
 * terminal can react to streaming chunks, exit codes, and the shell's working
 * directory. Returns null for plain-text or malformed messages.
 *
 * @param {string} message
 * @returns {{ type: 'output'|'error', output: string, exitCode: number|null, cwd: string|null, partial: boolean }|null}
 */
export function decodeTerminalEvent(message) {
	try {
		const response = JSON.parse(message);
		if (
			response &&
			typeof response === 'object' &&
			(response.type === 'output' || response.type === 'error')
		) {
			return {
				type: response.type,
				output: typeof response.output === 'string' ? response.output : '',
				exitCode: typeof response.exit_code === 'number' ? response.exit_code : null,
				cwd: typeof response.cwd === 'string' ? response.cwd : null,
				partial: response.partial === true
			};
		}
	} catch {
		// Plain-text or malformed messages have no structured event.
	}
	return null;
}
