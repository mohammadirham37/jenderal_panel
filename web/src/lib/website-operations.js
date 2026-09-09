/**
 * @param {string} websiteID
 */
export function websiteOperationAPI(websiteID) {
	const website = `/api/v1/websites/${encodeURIComponent(websiteID)}`;

	return {
		website,
		deploy: `${website}/deploy`,
		deployments: `${website}/deployments`,
		ssl: `${website}/ssl`,
		sslIssue: `${website}/ssl/issue`,
		sslCustom: `${website}/ssl/custom`,
		cronJobs: `${website}/cron-jobs`,
		queueWorkers: `${website}/queue-workers`
	};
}
