export const THEME_STORAGE_KEY = 'jenderal-theme';

/**
 * @param {string | null} savedTheme
 * @param {boolean} systemPrefersDark
 * @returns {'dark' | 'light'}
 */
export function resolveTheme(savedTheme, systemPrefersDark) {
	if (savedTheme === 'dark' || savedTheme === 'light') {
		return savedTheme;
	}

	return systemPrefersDark ? 'dark' : 'light';
}

/**
 * @param {'dark' | 'light'} theme
 * @param {HTMLElement} root
 * @param {HTMLMetaElement | null} themeColor
 */
export function applyTheme(theme, root, themeColor = null) {
	root.dataset.theme = theme;
	root.style.colorScheme = theme;
	themeColor?.setAttribute('content', theme === 'dark' ? '#07111e' : '#f8fafc');
}
