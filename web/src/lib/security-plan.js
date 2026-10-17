// Builds the Security Center overview's action plan and protection checklist
// from data the page already loads. Each item carries a code the page
// translates (sec.plan.<code>.title/.desc, sec.check.<key>) plus the tab,
// page, or terminal command that resolves it.

/** @typedef {'critical'|'high'|'medium'|'low'} PlanSeverity */
/**
 * @typedef {{
 *   key: string, code: string, severity: PlanSeverity,
 *   params: Record<string, string|number>, tab?: string, href?: string,
 *   command?: string, eventIds?: string[]
 * }} PlanItem
 */
/** @typedef {{ key: string, status: 'ok'|'warn'|'off'|'unknown', tab?: string, href?: string }} CheckItem */

const severityOrder = { critical: 0, high: 1, medium: 2, low: 3 };

/** Posture finding codes with dedicated guidance. */
const postureGuidance = {
	nginx_invalid: { href: '/nginx', command: 'sudo nginx -t' },
	ufw_inactive: { tab: 'firewall' },
	ssh_root_login_enabled: { href: '/terminal', command: 'sudo sshd -T | grep -i permitrootlogin' },
	ssh_password_auth_enabled: { href: '/terminal', command: 'sudo sshd -T | grep -i passwordauthentication' },
	security_updates_available: { href: '/terminal', command: 'sudo apt update && sudo apt upgrade' },
	apparmor_profiles_not_enforced: { href: '/terminal', command: 'sudo aa-status' }
};

/** Security component name to the Security Center tab managing it. */
const componentTabs = { fail2ban: 'fail2ban', malware: 'malware', traffic_guard: 'traffic' };

/** @param {unknown} value */
function asArray(value) {
	return Array.isArray(value) ? value : [];
}

/** @param {string} severity @returns {PlanSeverity} */
function planSeverity(severity) {
	if (severity === 'critical' || severity === 'high' || severity === 'medium') return severity;
	return 'low';
}

/**
 * @param {{
 *   overview?: any, fail2ban?: any, events?: any[], malware?: any,
 *   malwareSchedules?: any[], quarantine?: any[], waf?: any,
 *   trafficProfiles?: any[], websites?: any[], canFirewall?: boolean
 * }} data
 * @returns {PlanItem[]}
 */
export function buildActionPlan(data) {
	/** @type {PlanItem[]} */
	const items = [];
	const overview = data.overview || {};
	const fail2ban = data.fail2ban || {};
	const malware = data.malware || {};

	for (const finding of asArray(overview.posture?.findings)) {
		const guidance = postureGuidance[/** @type {keyof typeof postureGuidance} */ (finding.code)];
		const count = Number(finding.evidence?.count ?? finding.evidence?.complain ?? 0);
		if (!guidance) {
			items.push({
				key: 'posture:' + finding.code, code: 'posture_generic', severity: planSeverity(finding.severity),
				params: { summary: finding.summary || finding.code, remediation: finding.remediation || '' }
			});
			continue;
		}
		/** @type {PlanItem} */
		const item = {
			key: 'posture:' + finding.code, code: finding.code, severity: planSeverity(finding.severity),
			params: { count }, ...guidance
		};
		if (item.tab === 'firewall' && !data.canFirewall) {
			delete item.tab;
			item.href = '/firewall';
		}
		items.push(item);
	}

	if (!fail2ban.installed) {
		items.push({ key: 'fail2ban:missing', code: 'fail2ban_missing', severity: 'high', params: {}, tab: 'fail2ban' });
	} else if (!fail2ban.running) {
		items.push({ key: 'fail2ban:stopped', code: 'fail2ban_stopped', severity: 'high', params: {}, tab: 'fail2ban' });
	}

	if (!malware.installed) {
		items.push({ key: 'malware:missing', code: 'malware_missing', severity: 'medium', params: {}, tab: 'malware' });
	} else {
		if (malware.signature_fresh === false) {
			items.push({ key: 'malware:stale', code: 'malware_stale', severity: 'medium', params: {}, tab: 'malware' });
		}
		if (!asArray(data.malwareSchedules).some((schedule) => schedule.enabled)) {
			items.push({ key: 'malware:schedule', code: 'malware_no_schedule', severity: 'low', params: {}, tab: 'malware' });
		}
	}

	const quarantined = asArray(data.quarantine).filter((item) => item.status === 'quarantined').length;
	if (quarantined > 0) {
		items.push({ key: 'malware:quarantine', code: 'quarantine_review', severity: 'high', params: { count: quarantined }, tab: 'malware' });
	}

	for (const component of asArray(overview.components)) {
		// Missing Fail2ban/malware already has its own item above.
		if (!component.enabled || component.healthy) continue;
		if (component.name === 'fail2ban' && !fail2ban.running) continue;
		items.push({
			key: 'component:' + component.name, code: 'component_unhealthy', severity: 'medium',
			params: { name: component.name, message: component.message || '' },
			tab: componentTabs[/** @type {keyof typeof componentTabs} */ (component.name)]
		});
	}

	// Open events grouped per component and category. Posture events mirror
	// the findings above; quarantined malware has its own review item.
	/** @type {Map<string, PlanItem>} */
	const groups = new Map();
	for (const event of asArray(data.events)) {
		if (event.status !== 'open' && event.status !== 'acknowledged') continue;
		if (event.severity === 'low' || event.severity === 'info' || event.component === 'posture') continue;
		if (event.category === 'malware' && quarantined > 0) continue;
		const key = 'events:' + event.component + ':' + event.category;
		let group = groups.get(key);
		if (!group) {
			group = {
				key, code: 'events_review', severity: planSeverity(event.severity),
				params: { count: 0, component: event.component, category: event.category, action: event.recommended_action || '' },
				tab: 'events', eventIds: []
			};
			groups.set(key, group);
			items.push(group);
		}
		group.params.count = Number(group.params.count) + 1;
		group.eventIds?.push(event.id);
		if (severityOrder[planSeverity(event.severity)] < severityOrder[group.severity]) {
			group.severity = planSeverity(event.severity);
		}
	}

	const waf = data.waf;
	if (waf && !waf.modsecurity?.installed) {
		items.push({ key: 'waf:missing', code: 'waf_missing', severity: 'low', params: {}, tab: 'waf' });
	} else if (waf && waf.modsecurity?.mode && waf.modsecurity.mode !== 'blocking') {
		items.push({ key: 'waf:detection', code: 'waf_detection_only', severity: 'low', params: {}, tab: 'waf' });
	}

	if (asArray(data.websites).length > 0 && asArray(data.trafficProfiles).length === 0) {
		items.push({ key: 'traffic:none', code: 'traffic_unconfigured', severity: 'low', params: {}, tab: 'traffic' });
	}

	if (overview.setup_complete === false && !fail2ban.installed) {
		items.push({ key: 'setup', code: 'setup_incomplete', severity: 'medium', params: {}, tab: 'setup' });
	}

	return items.sort((a, b) => severityOrder[a.severity] - severityOrder[b.severity]);
}

/**
 * @param {{
 *   overview?: any, fail2ban?: any, malware?: any, malwareSchedules?: any[],
 *   waf?: any, trafficProfiles?: any[], canFirewall?: boolean
 * }} data
 * @returns {CheckItem[]}
 */
export function buildProtectionChecklist(data) {
	const posture = data.overview?.posture?.components || {};
	const fail2ban = data.fail2ban || {};
	const malware = data.malware || {};

	/** @param {string|undefined} state @returns {CheckItem['status']} */
	const fromPosture = (state) => {
		if (state === 'healthy') return 'ok';
		if (state === 'inactive' || state === 'invalid') return 'off';
		if (state === 'needs_attention' || state === 'mixed') return 'warn';
		return 'unknown';
	};

	const sshJail = asArray(fail2ban.jails).some((jail) => jail.name === 'sshd');
	const profiles = asArray(data.trafficProfiles);
	const wafMode = data.waf?.modsecurity?.mode;

	return [
		{ key: 'firewall', status: fromPosture(posture.firewall), ...(data.canFirewall ? { tab: 'firewall' } : { href: '/firewall' }) },
		{
			key: 'fail2ban', tab: 'fail2ban',
			status: !fail2ban.installed ? 'off' : fail2ban.running && sshJail ? 'ok' : 'warn'
		},
		{ key: 'ssh', status: fromPosture(posture.ssh), href: '/server' },
		{ key: 'updates', status: fromPosture(posture.security_updates), href: '/terminal' },
		{
			key: 'malware', tab: 'malware',
			status: !malware.installed ? 'off' : malware.signature_fresh === false ? 'warn' : 'ok'
		},
		{
			key: 'malware_schedule', tab: 'malware',
			status: asArray(data.malwareSchedules).some((schedule) => schedule.enabled) ? 'ok' : 'off'
		},
		{
			key: 'waf', tab: 'waf',
			status: !data.waf ? 'unknown' : !data.waf.modsecurity?.installed ? 'off' : wafMode === 'blocking' ? 'ok' : 'warn'
		},
		{
			key: 'traffic', tab: 'traffic',
			status: profiles.length === 0 ? 'off' : profiles.some((profile) => profile.mode !== 'observe') ? 'ok' : 'warn'
		},
		{ key: 'nginx', status: fromPosture(posture.nginx), href: '/nginx' },
		{ key: 'apparmor', status: fromPosture(posture.apparmor) }
	];
}

/** @param {any} fail2ban */
export function blockedSummary(fail2ban) {
	let current = 0;
	let total = 0;
	for (const jail of asArray(fail2ban?.jails)) {
		current += Number(jail.currently_banned) || 0;
		total += Number(jail.total_banned) || 0;
	}
	return { current, total };
}
