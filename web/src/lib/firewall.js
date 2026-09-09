/** @param {{active?: boolean, default?: string, rules?: unknown[]}} data */
export function normalizeFirewallStatus(data) {
	return {
		active: Boolean(data?.active),
		defaultPolicy: typeof data?.default === 'string' ? data.default : '',
		rules: Array.isArray(data?.rules) ? data.rules : []
	};
}

/** @param {string} value */
export function parseFirewallPort(value) {
	const trimmed = value.trim();
	if (!/^\d+$/.test(trimmed)) throw new Error('Enter a valid port number.');
	const port = Number(trimmed);
	if (port < 1 || port > 65535) throw new Error('Port must be between 1 and 65535.');
	return port;
}

/** @param {string} action @returns {'allow'|'deny'|'limit'|'neutral'} */
export function firewallActionTone(action) {
	const normalized = action.trim().toLowerCase();
	if (normalized.startsWith('allow')) return 'allow';
	if (normalized.startsWith('deny') || normalized.startsWith('reject')) return 'deny';
	if (normalized.startsWith('limit')) return 'limit';
	return 'neutral';
}
