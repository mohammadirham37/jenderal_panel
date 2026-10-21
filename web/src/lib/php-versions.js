// Pure helpers for the /php page: version summary, website usage, and the
// extension browser. Tested in tests/php/PHPVersions.test.mjs.
import { usesPHP } from './website-list.js';

/** @typedef {{ version: string, installed: boolean, running: boolean }} PHPVersion */
/** @typedef {{ enabled: string[], disabled: string[], available: string[] }} ExtensionStatus */
/** @typedef {'enabled' | 'disabled' | 'available'} ExtensionState */

/** @param {string} a @param {string} b */
function compareVersions(a, b) {
	const pa = a.split('.').map(Number);
	const pb = b.split('.').map(Number);
	for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
		const diff = (pa[i] || 0) - (pb[i] || 0);
		if (diff) return diff;
	}
	return 0;
}

/**
 * Split versions into installed and installable lists, newest first.
 * @param {PHPVersion[]} versions
 */
export function groupPHPVersions(versions) {
	const sorted = [...versions].sort((a, b) => compareVersions(b.version, a.version));
	const installed = sorted.filter((v) => v.installed);
	return {
		installed,
		available: sorted.filter((v) => !v.installed),
		running: installed.filter((v) => v.running).length,
		stopped: installed.filter((v) => !v.running).length,
		newest: sorted[0]?.version ?? ''
	};
}

/**
 * Map each PHP version to the domains of websites that run on it.
 * @param {import('./website-list.js').ListWebsite[]} websites
 * @returns {Record<string, string[]>}
 */
export function websitesByPHPVersion(websites) {
	/** @type {Record<string, string[]>} */
	const usage = {};
	for (const website of websites) {
		if (!usesPHP(website)) continue;
		(usage[website.php_version] ||= []).push(website.domain);
	}
	for (const domains of Object.values(usage)) domains.sort();
	return usage;
}

/**
 * Flatten the extension status into one searchable, sorted list.
 * @param {ExtensionStatus | null} status
 * @param {{ query?: string, state?: 'all' | ExtensionState }} [options]
 * @returns {{ name: string, state: ExtensionState }[]}
 */
export function listExtensions(status, { query = '', state = 'all' } = {}) {
	if (!status) return [];
	const normalized = query.trim().toLowerCase();
	/** @type {ExtensionState[]} */
	const states = ['enabled', 'disabled', 'available'];
	return states
		.filter((s) => state === 'all' || state === s)
		.flatMap((s) => (status[s] || []).map((name) => ({ name, state: s })))
		.filter((ext) => !normalized || ext.name.toLowerCase().includes(normalized))
		.sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * @param {ExtensionStatus | null} status
 * @returns {Record<'all' | ExtensionState, number>}
 */
export function countExtensions(status) {
	const enabled = status?.enabled?.length || 0;
	const disabled = status?.disabled?.length || 0;
	const available = status?.available?.length || 0;
	return { all: enabled + disabled + available, enabled, disabled, available };
}

/** @typedef {{ name: string, installed: string, candidate: string }} PackageUpdate */
/** @typedef {{ version: string, current: string, latest: string, update_available: boolean, packages: PackageUpdate[], security_baseline: string, vulnerable: boolean, end_of_life: boolean, advisory_url?: string }} PatchStatus */

/**
 * Group patch statuses for the page banner and toolbar.
 * @param {PatchStatus[]} versions
 */
export function summarizePatches(versions) {
	return {
		vulnerable: versions.filter((v) => v.vulnerable),
		updatable: versions.filter((v) => v.update_available),
		endOfLife: versions.filter((v) => v.end_of_life),
		// Vulnerable but apt offers nothing newer: the lists are stale or the
		// repository has not published the fix yet.
		stuck: versions.filter((v) => v.vulnerable && !v.update_available)
	};
}

/**
 * Hours since apt last refreshed its lists; null when unknown.
 * @param {string | null | undefined} listsUpdatedAt
 * @param {number} [now]
 */
export function listsAgeHours(listsUpdatedAt, now = Date.now()) {
	if (!listsUpdatedAt) return null;
	const t = Date.parse(listsUpdatedAt);
	return Number.isFinite(t) ? Math.max(0, Math.floor((now - t) / 3600000)) : null;
}
