/**
 * @param {{ installed: boolean }} status
 */
export function composerActionPath(status) {
	return status.installed
		? '/api/v1/services/composer/update'
		: '/api/v1/services/composer/install';
}
