const knownConditions = new Set(['good', 'needs_attention', 'critical']);

/** @param {string[]} [ignoreIPs] */
export function buildSafeFail2banSettings(ignoreIPs = []) {
	return {
		sshd_enabled: true,
		max_retry: 5,
		find_time_seconds: 600,
		ban_time_seconds: 900,
		ignore_ips: ignoreIPs.map((value) => String(value).trim()).filter(Boolean)
	};
}

/** @param {{jail?: unknown, ip?: unknown, duration_seconds?: unknown}} input */
export function validateBan(input) {
	const jail = String(input?.jail || '').trim();
	const ip = String(input?.ip || '').trim();
	const duration = Number(input?.duration_seconds);
	if (!jail) throw new Error('Select an active jail.');
	if (!ip) throw new Error('Enter an IP address.');
	if (!Number.isInteger(duration) || duration < 60) {
		throw new Error('Manual bans must be temporary and at least 60 seconds.');
	}
	if (duration > 604800) throw new Error('Manual bans cannot exceed seven days.');
	return { jail, ip, duration_seconds: duration };
}

/** @param {Record<string, any>} [value] */
export function normalizeOverview(value = {}) {
	const condition = knownConditions.has(value.condition) ? value.condition : 'unknown';
	const rawReasons = Array.isArray(value.reasons) ? value.reasons : [];
	const reasons = rawReasons.map(String).map((reason) => reason.trim()).filter(Boolean);
	if (reasons.length === 0) reasons.push('Review component status below.');
	return {
		condition,
		reasons,
		components: Array.isArray(value.components) ? value.components : [],
		open_events: Number.isFinite(value.open_events) ? value.open_events : 0,
		setup_complete: value.setup_complete === true,
		active_tasks: Array.isArray(value.active_tasks) ? value.active_tasks : [],
		posture: value.posture && typeof value.posture === 'object' ? value.posture : undefined
	};
}

/** @param {string} condition */
export function conditionTone(condition) {
	if (condition === 'critical') return 'critical';
	if (condition === 'needs_attention') return 'warning';
	if (condition === 'good') return 'good';
	return 'neutral';
}

/** @param {string | null | undefined} value @param {Date} [now] */
export function formatBanExpiry(value, now = new Date()) {
	if (!value) return 'Managed by Fail2ban';
	const expiry = new Date(value);
	if (Number.isNaN(expiry.getTime())) return 'Unknown expiry';
	const remaining = expiry.getTime() - now.getTime();
	if (remaining <= 0) return 'Expired';
	const minutes = Math.ceil(remaining / 60000);
	return `${minutes} min remaining`;
}
