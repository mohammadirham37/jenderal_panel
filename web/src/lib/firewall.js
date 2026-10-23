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

const smbPorts = [137, 138, 139, 445];

/**
 * Rules that open SMB file sharing (137–139, 445) to anyone. The panel never
 * serves SMB, and the in-kernel SMB server (ksmbd) had pre-auth remote code
 * execution flaws on 445, so these should be removed.
 * @template {{ to: string, action: string, from: string }} T
 * @param {T[]} rules
 * @returns {T[]}
 */
export function publicSMBRules(rules) {
	return rules.filter((rule) => {
		const tone = firewallActionTone(rule.action || '');
		if (tone !== 'allow' && tone !== 'limit') return false;
		if (!(rule.from || '').trim().toLowerCase().startsWith('anywhere')) return false;
		const spec = (rule.to || '').replace(/\(v6\)/i, '').trim().split(/[\s/]/)[0];
		return spec.split(',').some((part) => {
			const [low, high] = part.split(':').map(Number);
			if (!Number.isInteger(low)) return false;
			const top = Number.isInteger(high) ? high : low;
			return smbPorts.some((port) => port >= low && port <= top);
		});
	});
}
