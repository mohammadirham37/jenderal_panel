// Pure helpers for the /nodejs runtime security overview.
// Tested in tests/websites/NodeRuntime.test.mjs.

/** @typedef {{ website_id: string, domain: string, selected_version: string, installed: boolean, installed_version: string, vulnerable?: boolean, end_of_life?: boolean, security_baseline?: string }} NodeRuntime */

/**
 * Group website runtimes for the security banner and the update button.
 * @template {NodeRuntime} T
 * @param {T[]} runtimes
 */
export function summarizeNodeRuntimes(runtimes) {
	const selected = runtimes.filter((r) => r.selected_version);
	return {
		selected,
		vulnerable: selected.filter((r) => r.vulnerable),
		endOfLife: selected.filter((r) => r.end_of_life),
		notInstalled: selected.filter((r) => !r.installed)
	};
}

/**
 * Status key for one runtime row: vulnerable, eol, missing, ok, or none.
 * @param {NodeRuntime} r
 */
export function nodeRuntimeState(r) {
	if (!r.selected_version) return 'none';
	if (r.end_of_life) return 'eol';
	if (!r.installed) return 'missing';
	if (r.vulnerable) return 'vulnerable';
	return 'ok';
}
