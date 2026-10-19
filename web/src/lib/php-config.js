const simpleDirectives = [
	'memory_limit',
	'upload_max_filesize',
	'post_max_size',
	'max_execution_time',
	'max_input_time',
	'max_input_vars',
	'display_errors'
];

/** @type {Record<string, string>} */
const simpleDefaults = {
	memory_limit: '128M',
	upload_max_filesize: '2M',
	post_max_size: '8M',
	max_execution_time: '30',
	max_input_time: '60',
	max_input_vars: '1000',
	display_errors: 'Off'
};

/**
 * Read the commonly edited directives from php.ini.
 *
 * @param {string} content
 * @returns {Record<string, string>}
 */
export function parseSimplePHPConfig(content) {
	const values = { ...simpleDefaults };
	const supported = new Set(simpleDirectives);

	for (const line of content.split(/\r?\n/)) {
		const match = line.match(/^\s*([A-Za-z0-9_.]+)\s*=\s*(.*?)\s*$/);
		if (match && supported.has(match[1])) {
			values[match[1]] = match[2];
		}
	}

	return values;
}

/**
 * Update only the simple-mode directives and leave the rest of php.ini intact.
 * Missing directives are appended so the saved values are explicit.
 *
 * @param {string} content
 * @param {Record<string, string>} values
 * @returns {string}
 */
export function updateSimplePHPConfig(content, values) {
	let updated = content;
	const missing = [];

	for (const directive of simpleDirectives) {
		const pattern = new RegExp(`^\\s*${directive}\\s*=.*$`, 'gm');
		const replacement = `${directive} = ${values[directive]}`;
		if (pattern.test(updated)) {
			updated = updated.replace(pattern, replacement);
		} else {
			missing.push(replacement);
		}
	}

	if (missing.length > 0) {
		if (updated.length > 0 && !updated.endsWith('\n')) updated += '\n';
		updated += missing.join('\n') + '\n';
	}

	return updated;
}

/**
 * Move between the Simple and Advanced config views without losing an
 * unsaved draft. Selecting the active mode is intentionally a no-op.
 *
 * @param {{ mode: 'simple' | 'advanced', content: string, simpleConfig: Record<string, string> }} state
 * @param {'simple' | 'advanced'} targetMode
 * @returns {{ mode: 'simple' | 'advanced', content: string, simpleConfig: Record<string, string> }}
 */
export function switchPHPConfigMode(state, targetMode) {
	if (state.mode === targetMode) return state;

	if (targetMode === 'advanced') {
		return {
			...state,
			mode: targetMode,
			content: updateSimplePHPConfig(state.content, state.simpleConfig)
		};
	}

	return {
		...state,
		mode: targetMode,
		simpleConfig: parseSimplePHPConfig(state.content)
	};
}

/**
 * One-click starting points for the Simple form. Applying a preset only
 * fills the form; nothing is written until the operator saves.
 * @type {Record<string, Record<string, string>>}
 */
export const phpConfigPresets = {
	default: { ...simpleDefaults },
	wordpress: {
		memory_limit: '256M',
		upload_max_filesize: '64M',
		post_max_size: '64M',
		max_execution_time: '300',
		max_input_time: '300',
		max_input_vars: '3000',
		display_errors: 'Off'
	},
	laravel: {
		memory_limit: '512M',
		upload_max_filesize: '32M',
		post_max_size: '32M',
		max_execution_time: '120',
		max_input_time: '120',
		max_input_vars: '3000',
		display_errors: 'Off'
	},
	uploads: {
		memory_limit: '512M',
		upload_max_filesize: '512M',
		post_max_size: '512M',
		max_execution_time: '600',
		max_input_time: '600',
		max_input_vars: '5000',
		display_errors: 'Off'
	}
};

/**
 * Convert a php.ini size shorthand (128M, 1G, 512K, 1048576) to bytes.
 * Returns null for unlimited (-1) or unparseable values.
 *
 * @param {string} value
 * @returns {number | null}
 */
export function parseIniSize(value) {
	const match = String(value ?? '').trim().match(/^([0-9]+)\s*([KMGkmg]?)$/);
	if (!match) return null;
	const units = { '': 1, k: 1024, m: 1024 ** 2, g: 1024 ** 3 };
	return Number(match[1]) * units[/** @type {'' | 'k' | 'm' | 'g'} */ (match[2].toLowerCase())];
}

/**
 * Cross-field sanity checks for the Simple form, returned as i18n keys.
 *
 * @param {Record<string, string>} values
 * @returns {string[]}
 */
export function phpConfigWarnings(values) {
	const warnings = [];
	const upload = parseIniSize(values.upload_max_filesize);
	const post = parseIniSize(values.post_max_size);
	const memory = parseIniSize(values.memory_limit);
	if (upload !== null && post !== null && post < upload) warnings.push('phpv.config.warnPostBelowUpload');
	if (memory !== null && post !== null && memory < post) warnings.push('phpv.config.warnMemoryBelowPost');
	if (values.display_errors === 'On') warnings.push('phpv.config.warnDisplayErrors');
	return warnings;
}

/**
 * True when the editor holds changes that differ from the saved php.ini.
 *
 * @param {{ mode: 'simple' | 'advanced', content: string, simpleConfig: Record<string, string> }} state
 * @param {string} savedContent
 */
export function phpConfigIsDirty(state, savedContent) {
	if (state.mode === 'advanced') return state.content !== savedContent;
	const saved = parseSimplePHPConfig(savedContent);
	return simpleDirectives.some((directive) => String(state.simpleConfig[directive] ?? '') !== String(saved[directive] ?? ''));
}
