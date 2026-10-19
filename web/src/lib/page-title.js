// Browser tab title for the current route. The layout is the only place that
// sets <title>: in Svelte 5 a page-level <title> writes document.title
// directly and is not restored on navigation, which left stale tab titles.

/**
 * Pick the i18n key of the route that best matches the pathname: an exact
 * match or the longest "href/" prefix, so /websites/123 resolves to /websites.
 *
 * @param {string} pathname
 * @param {{ href: string, labelKey: string }[]} routes
 * @returns {string}
 */
export function pageTitleKey(pathname, routes) {
	let best = { href: '', labelKey: '' };
	for (const route of routes) {
		const matches = pathname === route.href || pathname.startsWith(route.href + '/');
		if (matches && route.href.length > best.href.length) best = route;
	}
	return best.labelKey;
}

/**
 * "Websites · server01", or just the host when the route has no label.
 *
 * @param {string} label
 * @param {string} host
 */
export function formatPageTitle(label, host) {
	return label ? `${label} · ${host}` : host;
}
