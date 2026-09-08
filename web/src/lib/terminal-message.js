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
		if (response && typeof response === 'object' && typeof response.output === 'string') {
			return response.output;
		}
	} catch {
		// Plain-text messages are valid for compatibility with older servers.
	}

	return message;
}
