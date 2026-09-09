/** @param {Record<string, any>} [value] */
export function normalizeSetupReview(value = {}) {
	return {
		mutations: Array.isArray(value.mutations) ? value.mutations.map(String) : [],
		warnings: Array.isArray(value.warnings) ? value.warnings.map(String) : [],
		hash: typeof value.hash === 'string' ? value.hash : ''
	};
}

/** @param {string} websiteID */
export function buildTrafficRecovery(websiteID) {
	if (!String(websiteID || '').trim()) throw new Error('Website ID is required.');
	return { website_id: String(websiteID), mode: 'observe', confirm: true };
}

/** @param {Record<string, any>} [value] */
export function buildSafeSetupRequest(value = {}) {
	const managementCIDRs = Array.isArray(value.management_cidrs) ? value.management_cidrs.map(String).map((v) => v.trim()).filter(Boolean) : [];
	const enableFail2ban = value.enable_fail2ban !== false;
	if (enableFail2ban && managementCIDRs.length === 0) throw new Error('Enter at least one management CIDR before enabling SSH protection.');
	const malwareMode = String(value.malware_mode || 'low_memory');
	if (!['', 'low_memory', 'daemon'].includes(malwareMode)) throw new Error('Invalid malware runtime.');
	const scheduleMalware = value.schedule_malware !== false && malwareMode !== '';
	const scheduleTime = String(value.schedule_time || '02:00');
	if (scheduleMalware && !/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(scheduleTime)) throw new Error('Schedule time must use HH:MM.');
	return {
		management_cidrs: managementCIDRs,
		enable_fail2ban: enableFail2ban,
		malware_mode: malwareMode,
		schedule_malware: scheduleMalware,
		schedule_time: scheduleTime,
		traffic_website_ids: Array.isArray(value.traffic_website_ids) ? value.traffic_website_ids.map(String).filter(Boolean) : []
	};
}
