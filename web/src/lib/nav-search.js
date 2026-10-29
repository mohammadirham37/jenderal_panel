// Sidebar menu search: pure helpers so tests/layout can run them.

/**
 * @typedef {{ href: string, labelKey: string, keywords?: string[] }} SearchableItem
 * @typedef {{ key: string, items: SearchableItem[] }} SearchableGroup
 */

/** @param {string} value */
function normalize(value) {
	return value
		.toLowerCase()
		.normalize('NFKD')
		.replace(/[̀-ͯ]/g, '');
}

/**
 * Keep the menu items whose label, group, path, or keywords contain every
 * term of the query; groups left empty are dropped. An empty query keeps
 * everything.
 * @template {SearchableGroup} G
 * @param {G[]} groups
 * @param {string} query
 * @param {(key: string) => string} label translates a label key
 * @returns {G[]}
 */
export function filterNavGroups(groups, query, label) {
	const terms = normalize(query).split(/\s+/).filter(Boolean);
	if (terms.length === 0) return groups;
	return groups
		.map((group) => ({
			...group,
			items: group.items.filter((item) => {
				const haystack = normalize(
					[label(item.labelKey), label(group.key), item.href, ...(item.keywords ?? [])].join(' ')
				);
				return terms.every((term) => haystack.includes(term));
			})
		}))
		.filter((group) => group.items.length > 0);
}
