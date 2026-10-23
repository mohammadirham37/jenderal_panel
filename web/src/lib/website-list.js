// Pure helpers for the websites list page: status grouping, search, and sort.
// Kept framework-free so tests/websites/WebsiteList.test.mjs can run them.

/** @typedef {{ domain: string, app_type: string, framework: string, framework_version: string, php_version: string, status: string, created_at: string, bandwidth_bytes: number, disk_bytes?: number, owner_email?: string }} ListWebsite */
/** @typedef {'all' | 'active' | 'pending' | 'failed' | 'suspended'} StatusFilter */

export const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

/**
 * Collapse raw website statuses into the filter groups shown on the page.
 * @param {string} status
 */
export function statusGroup(status) {
	if (status === 'active') return 'active';
	if (pendingStatuses.includes(status)) return 'pending';
	if (status === 'failed') return 'failed';
	if (status === 'suspended' || status === 'disabled') return 'suspended';
	return 'other';
}

/**
 * @param {ListWebsite[]} websites
 * @returns {Record<StatusFilter, number>}
 */
export function countByGroup(websites) {
	/** @type {Record<StatusFilter, number>} */
	const counts = { all: websites.length, active: 0, pending: 0, failed: 0, suspended: 0 };
	for (const website of websites) {
		const group = statusGroup(website.status);
		if (group !== 'other') counts[group]++;
	}
	return counts;
}

/** @param {ListWebsite} website */
export function frameworkLabel(website) {
	const base = website.framework && website.framework !== 'none' ? website.framework : website.app_type;
	return website.framework_version ? `${base} ${website.framework_version}` : base;
}

/**
 * True when the website runs PHP, so a PHP version is meaningful to show.
 * @param {ListWebsite} website
 */
export function usesPHP(website) {
	return !!website.php_version && appTypeUsesPHP(website.app_type);
}

// App types that run without PHP-FPM even if an older row stored a version.
// Mirrors nonPHPAppTypes in internal/website/profiles.go.
const nonPHPAppTypes = ['static', 'reverse-proxy', 'node', 'go', 'python', 'deno', 'bun'];

/**
 * Whether a website of this app type runs on PHP-FPM (PHP version, PHP
 * settings, and nginx PHP templates apply).
 * @param {string | undefined} appType
 */
export function appTypeUsesPHP(appType) {
	return !!appType && !nonPHPAppTypes.includes(appType);
}

/**
 * @param {ListWebsite} website
 * @param {string} query
 */
function matchesQuery(website, query) {
	if (!query) return true;
	const haystack = [website.domain, frameworkLabel(website), website.owner_email, usesPHP(website) ? `php ${website.php_version}` : '']
		.filter(Boolean)
		.join(' ')
		.toLowerCase();
	return query.split(/\s+/).every((term) => haystack.includes(term));
}

/** @type {Record<string, (a: ListWebsite, b: ListWebsite) => number>} */
const sorters = {
	newest: (a, b) => (b.created_at || '').localeCompare(a.created_at || ''),
	oldest: (a, b) => (a.created_at || '').localeCompare(b.created_at || ''),
	name: (a, b) => a.domain.localeCompare(b.domain),
	bandwidth: (a, b) => (b.bandwidth_bytes || 0) - (a.bandwidth_bytes || 0),
	size: (a, b) => (b.disk_bytes || 0) - (a.disk_bytes || 0)
};

export const sortKeys = Object.keys(sorters);

/**
 * Filter by status group and free-text query (all terms must match the
 * domain, stack, owner, or PHP version), then sort. Failed and in-progress
 * sites float to the top under every sort so problems stay visible.
 * @template {ListWebsite} T
 * @param {T[]} websites
 * @param {{ query?: string, status?: string, sort?: string }} [options]
 * @returns {T[]}
 */
export function filterWebsites(websites, { query = '', status = 'all', sort = 'newest' } = {}) {
	const normalized = query.trim().toLowerCase();
	const compare = sorters[sort] || sorters.newest;
	const attention = (/** @type {ListWebsite} */ w) => (['failed', 'pending'].includes(statusGroup(w.status)) ? 0 : 1);
	return websites
		.filter((w) => (status === 'all' || statusGroup(w.status) === status) && matchesQuery(w, normalized))
		.sort((a, b) => attention(a) - attention(b) || compare(a, b));
}

/** @param {number} bytes */
export function formatBytes(bytes) {
	if (!bytes || bytes <= 0) return '0 B';
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let i = 0;
	let size = bytes;
	while (size >= 1024 && i < units.length - 1) {
		size /= 1024;
		i++;
	}
	return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
