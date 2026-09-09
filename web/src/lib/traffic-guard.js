const modes = new Set(['observe', 'balanced', 'strict', 'custom']);
const proxyModes = new Set(['direct', 'cloudflare', 'custom']);
const proxyHeaders = new Set(['X-Forwarded-For', 'X-Real-IP', 'CF-Connecting-IP']);

/** @param {string} mode */
export function enforcementWarning(mode) {
	if (mode === 'observe') {
		return 'Observe Mode records HTTP-layer traffic without rejecting requests.';
	}
	return 'This applies HTTP-layer limits at the origin. It cannot stop volumetric DDoS traffic; use a CDN or network provider for upstream protection.';
}

/** @param {string | null | undefined} startedAt @param {Date} [now] */
export function observationProgress(startedAt, now = new Date()) {
	const started = new Date(startedAt || '');
	if (Number.isNaN(started.getTime()) || Number.isNaN(now.getTime())) return 0;
	const percent = ((now.getTime() - started.getTime()) / 86_400_000) * 100;
	return Math.max(0, Math.min(100, Math.round(percent)));
}

/** @param {Record<string, any>} [value] */
export function buildTrafficProfile(value = {}) {
	const mode = String(value.mode || 'observe');
	const proxyMode = String(value.proxy_mode || 'direct');
	if (!modes.has(mode)) throw new Error('Select a valid Traffic Guard mode.');
	if (!proxyModes.has(proxyMode)) throw new Error('Select a valid trusted proxy mode.');
	const requestsPerSecond = Number(value.requests_per_second ?? 10);
	const burst = Number(value.burst ?? 20);
	const connections = Number(value.connections ?? 20);
	if (!Number.isInteger(requestsPerSecond) || requestsPerSecond < 1 || requestsPerSecond > 1000) throw new Error('Custom request rate must be between 1 and 1000 requests per second.');
	if (!Number.isInteger(burst) || burst < 1 || burst > 5000) throw new Error('Burst must be between 1 and 5000.');
	if (!Number.isInteger(connections) || connections < 1 || connections > 1000) throw new Error('Connections must be between 1 and 1000.');
	let proxyHeader = String(value.proxy_header || '').trim();
	const proxyCIDRs = Array.isArray(value.proxy_cidrs) ? value.proxy_cidrs.map(String).map((v) => v.trim()).filter(Boolean) : [];
	if (proxyMode === 'cloudflare') proxyHeader = 'CF-Connecting-IP';
	if (proxyMode === 'custom') {
		if (!proxyHeaders.has(proxyHeader)) throw new Error('Select an allowlisted forwarded-IP header.');
		if (proxyCIDRs.length === 0) throw new Error('Enter at least one trusted proxy CIDR.');
	}
	return {
		mode,
		proxy_mode: proxyMode,
		proxy_header: proxyMode === 'direct' ? '' : proxyHeader,
		proxy_cidrs: proxyMode === 'custom' ? proxyCIDRs : [],
		requests_per_second: requestsPerSecond,
		burst,
		connections
	};
}

/** @param {Array<Record<string, any>>} [buckets] */
export function summarizeTraffic(buckets = []) {
	const summary = { requests: 0, status4xx: 0, status5xx: 0, status429: 0, bytes: 0, peakRPS: 0, topIPs: /** @type {Array<[string, number]>} */ ([]) };
	const ips = new Map();
	for (const bucket of buckets || []) {
		summary.requests += Number(bucket.requests || 0);
		summary.status4xx += Number(bucket.status_4xx || 0);
		summary.status5xx += Number(bucket.status_5xx || 0);
		summary.status429 += Number(bucket.status_429 || 0);
		summary.bytes += Number(bucket.bytes || 0);
		summary.peakRPS = Math.max(summary.peakRPS, Number(bucket.peak_rps || 0));
		for (const [ip, count] of Object.entries(bucket.top_ips || {})) ips.set(ip, (ips.get(ip) || 0) + Number(count || 0));
	}
	summary.topIPs = [...ips.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).slice(0, 20);
	return summary;
}
