// Pure helpers for the /server page. Tested in tests/server/ServerInfo.test.mjs.

const hostnameLabel = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/;

/**
 * Mirrors the backend RFC 1123 check so the form can explain the problem
 * before the request is sent.
 * @param {string} hostname
 */
export function isValidHostname(hostname) {
	const value = hostname.trim();
	if (!value || value.length > 253) return false;
	return value.split('.').every((label) => hostnameLabel.test(label));
}

/** @param {string} value e.g. "45%" */
export function parsePercent(value) {
	const n = Number.parseFloat(String(value ?? '').replace('%', ''));
	return Number.isFinite(n) ? Math.min(Math.max(n, 0), 100) : null;
}

/**
 * Tailwind colour family for a usage percentage.
 * @param {number | null} percent
 * @returns {'green' | 'yellow' | 'red' | 'gray'}
 */
export function usageTone(percent) {
	if (percent === null || !Number.isFinite(percent)) return 'gray';
	if (percent >= 90) return 'red';
	if (percent >= 75) return 'yellow';
	return 'green';
}

/** @param {number} bytes */
export function formatBytes(bytes) {
	if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let i = 0;
	let size = bytes;
	while (size >= 1024 && i < units.length - 1) {
		size /= 1024;
		i++;
	}
	return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

/**
 * Compact uptime such as "12d 4h 3m"; null for missing values.
 * @param {number} seconds
 */
export function formatUptime(seconds) {
	if (!Number.isFinite(seconds) || seconds <= 0) return null;
	const days = Math.floor(seconds / 86400);
	const hours = Math.floor((seconds % 86400) / 3600);
	const minutes = Math.floor((seconds % 3600) / 60);
	const parts = [];
	if (days) parts.push(`${days}d`);
	if (hours || days) parts.push(`${hours}h`);
	parts.push(`${minutes}m`);
	return parts.join(' ');
}

/**
 * Load average relative to CPU cores, as a 0–100+ percentage.
 * @param {number} load
 * @param {number} cores
 */
export function loadPercent(load, cores) {
	if (!Number.isFinite(load) || !cores || cores <= 0) return null;
	return Math.round((load / cores) * 100);
}

/**
 * Wall-clock time in a server timezone, or null when the zone is unknown
 * to the browser.
 * @param {Date} date
 * @param {string} timezone
 * @param {string} [locale]
 */
export function formatServerTime(date, timezone, locale) {
	try {
		return new Intl.DateTimeFormat(locale, {
			timeZone: timezone,
			weekday: 'short',
			day: 'numeric',
			month: 'short',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		}).format(date);
	} catch {
		return null;
	}
}

/**
 * UTC offset label such as "UTC+07:00" for a timezone at a given instant.
 * @param {Date} date
 * @param {string} timezone
 */
export function utcOffsetLabel(date, timezone) {
	try {
		const parts = new Intl.DateTimeFormat('en-US', { timeZone: timezone, timeZoneName: 'longOffset' }).formatToParts(date);
		const name = parts.find((p) => p.type === 'timeZoneName')?.value || '';
		return name === 'GMT' ? 'UTC+00:00' : name.replace('GMT', 'UTC');
	} catch {
		return '';
	}
}
