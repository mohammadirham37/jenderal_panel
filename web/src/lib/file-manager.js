/**
 * Converts an absolute website document root into the path expected by the
 * website-scoped file manager, whose root is the website account home.
 *
 * @param {string} documentRoot
 * @param {string} webUser
 */
export function fileManagerStartPath(documentRoot, webUser) {
	if (!documentRoot || !webUser) return '/';

	const homePath = `/home/${webUser}`;
	if (documentRoot === homePath) return '/';
	if (!documentRoot.startsWith(`${homePath}/`)) return '/';

	return documentRoot.slice(homePath.length);
}

/**
 * Creates the website-scoped file manager client using the backend's routes
 * and HTTP methods.
 *
 * @param {{ get: Function, post: Function, del: Function }} api
 * @param {string} websiteID
 */
export function createFileManagerAPI(api, websiteID) {
	const base = `/api/v1/websites/${encodeURIComponent(websiteID)}/files`;
	return {
		browse(/** @type {string} */ path) {
			return api.get(`${base}?path=${encodeURIComponent(path)}`);
		},
		read(/** @type {string} */ path) {
			return api.get(`${base}/read?path=${encodeURIComponent(path)}`);
		},
		write(/** @type {string} */ path, /** @type {string} */ content) {
			return api.post(`${base}/write`, { path, content });
		},
		remove(/** @type {string} */ path) {
			return api.del(`${base}?path=${encodeURIComponent(path)}`);
		},
		mkdir(/** @type {string} */ path) {
			return api.post(`${base}/mkdir`, { path });
		},
		rename(/** @type {string} */ oldPath, /** @type {string} */ newPath) {
			return api.post(`${base}/rename`, { old_path: oldPath, new_path: newPath });
		}
	};
}
