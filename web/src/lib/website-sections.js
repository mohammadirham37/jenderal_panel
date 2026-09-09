const sections = [
	{ label: 'Overview', suffix: '' },
	{ label: 'Deployments', suffix: '/deployments' },
	{ label: 'SSL', suffix: '/ssl' },
	{ label: 'Cron Jobs', suffix: '/cron' },
	{ label: 'Queue Workers', suffix: '/queue-workers' }
];

/**
 * @param {string} websiteID
 * @returns {{ label: string, href: string }[]}
 */
export function websiteSectionLinks(websiteID) {
	const encodedWebsiteID = encodeURIComponent(websiteID);
	const basePath = `/websites/${encodedWebsiteID}`;

	return sections.map(({ label, suffix }) => ({
		label,
		href: `${basePath}${suffix}`
	}));
}
