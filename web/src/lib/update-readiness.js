/**
 * Wait until the restarted panel reports the revision installed by the update.
 * Temporary request failures are expected while systemd replaces the process.
 *
 * @param {{
 *   expectedVersion: string,
 *   check: () => Promise<{ current_version: string }>,
 *   delay?: (milliseconds: number) => Promise<void>,
 *   maxAttempts?: number,
 *   shouldContinue?: () => boolean
 * }} options
 * @returns {Promise<boolean>}
 */
export async function waitForUpdatedPanel({
	expectedVersion,
	check,
	delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds)),
	maxAttempts = 30,
	shouldContinue = () => true
}) {
	for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
		if (!shouldContinue()) return false;
		try {
			const info = await check();
			if (info.current_version === expectedVersion) return true;
		} catch {
			// The panel is expected to be briefly unavailable during restart.
		}

		if (attempt + 1 < maxAttempts) await delay(2000);
	}

	return false;
}

/**
 * Build a document-navigation URL that cannot reuse the previous page cache.
 * @param {string} href
 * @param {string} token
 */
export function cacheBustedURL(href, token = Date.now().toString()) {
	const url = new URL(href);
	url.searchParams.set('_panel_reload', token);
	return url.toString();
}

/**
 * Classify persisted update recovery state against a fresh panel version.
 * @param {string} currentVersion
 * @param {string} targetVersion
 * @param {string} taskID
 * @returns {'none' | 'active' | 'complete' | 'stale'}
 */
export function classifyUpdateRecovery(currentVersion, targetVersion, taskID) {
	if (!targetVersion) return 'none';
	if (currentVersion === targetVersion) return 'complete';
	return taskID ? 'active' : 'stale';
}
