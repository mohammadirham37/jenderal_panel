/** @typedef {{ version: string, installed: boolean, running?: boolean }} PHPOption */
/** @typedef {{ name: string, installed: boolean, version: string, manage_url: string }} DependencyOption */
/** @typedef {{ version: string, enabled: boolean, reason: string }} CompatibilityOption */
/** @typedef {{ template: string, php_version: string, framework_version: string, frontend_stack: string, inertia_adapter: string, project_variant: string, setup_mode: string }} WebsiteSelection */
/** @typedef {{ template: string, framework_version: string, frontend_stack: string, inertia_adapter: string, project_variant: string, setup_mode: string, enabled: boolean, reason: string, document_root: string, prerequisites: string[], php_compatibility: CompatibilityOption[] }} ProfileOption */
/** @typedef {{ php_versions: PHPOption[], dependencies: DependencyOption[], profiles: ProfileOption[], inertia_adapters: string[], defaults: WebsiteSelection }} WebsiteFormOptions */

/**
 * Return only PHP runtimes detected as installed by the backend.
 * @param {WebsiteFormOptions} options
 * @returns {PHPOption[]}
 */
export function availablePHPVersions(options) {
	return (options?.php_versions || []).filter((item) => item.installed);
}

/**
 * Normalize dependent form fields without duplicating the backend support matrix.
 * @param {Partial<WebsiteSelection>} selection
 * @param {WebsiteFormOptions} options
 * @returns {WebsiteSelection}
 */
export function normalizeWebsiteSelection(selection, options) {
	const defaults = options?.defaults || {};
	const normalized = {
		template: selection.template || defaults.template || 'php',
		php_version: selection.php_version || defaults.php_version || '',
		framework_version: selection.framework_version || '',
		frontend_stack: selection.frontend_stack || '',
		inertia_adapter: selection.inertia_adapter || '',
		project_variant: selection.project_variant || 'empty',
		setup_mode: selection.setup_mode || defaults.setup_mode || 'config-only'
	};

	if (normalized.template === 'static') {
		normalized.php_version = '';
	}
	if (normalized.template !== 'laravel') {
		normalized.framework_version = '';
		normalized.frontend_stack = '';
		normalized.inertia_adapter = '';
		normalized.project_variant = 'empty';
		return normalized;
	}

	normalized.framework_version ||= defaults.framework_version || '12';
	normalized.frontend_stack ||= defaults.frontend_stack || 'blade';
	if (normalized.frontend_stack !== 'inertia') {
		normalized.inertia_adapter = '';
	} else {
		normalized.inertia_adapter ||= options?.inertia_adapters?.[0] || 'react';
	}
	if (normalized.frontend_stack === 'blade') {
		normalized.project_variant = 'empty';
	}
	return normalized;
}

/**
 * Resolve the exact backend catalog entry plus runtime availability.
 * @param {WebsiteFormOptions} options
 * @param {WebsiteSelection} selection
 * @returns {any}
 */
export function selectedCombination(options, selection) {
	const normalized = normalizeWebsiteSelection(selection, options);
	const profile = (options?.profiles || []).find((item) =>
		item.template === normalized.template &&
		(item.framework_version || '') === normalized.framework_version &&
		(item.frontend_stack || '') === normalized.frontend_stack &&
		(item.inertia_adapter || '') === normalized.inertia_adapter &&
		(item.project_variant || 'empty') === normalized.project_variant &&
		item.setup_mode === normalized.setup_mode
	);
	if (!profile) {
		return { enabled: false, reason: 'This template combination is not available.', document_root: '', prerequisites: [], missing_dependencies: [] };
	}
	if (!profile.enabled) {
		return { ...profile, enabled: false, missing_dependencies: [] };
	}
	const compatibility = (profile.php_compatibility || []).find((item) => item.version === normalized.php_version);
	if (compatibility && !compatibility.enabled) {
		return { ...profile, enabled: false, reason: compatibility.reason, missing_dependencies: [] };
	}
	const dependencies = options?.dependencies || [];
	/** @type {DependencyOption[]} */
	const missing = [];
	for (const name of profile.prerequisites || []) {
		const dependency = dependencies.find((item) => item.name === name);
		if (dependency && !dependency.installed) missing.push(dependency);
	}
	if (missing.length > 0) {
		return { ...profile, enabled: false, reason: `${missing.map((item) => item.name).join(' and ')} must be installed first.`, missing_dependencies: missing };
	}
	return { ...profile, enabled: true, reason: '', missing_dependencies: [] };
}
