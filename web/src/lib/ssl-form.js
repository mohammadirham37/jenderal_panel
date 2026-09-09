/**
 * @param {{ id: string, domain: string, domains?: { name: string }[] }[]} websites
 * @param {string} websiteID
 * @returns {string[]}
 */
export function domainsForWebsite(websites, websiteID) {
	const website = websites.find((item) => item.id === websiteID);
	if (!website) return [];
	return [
		...new Set(
			[website.domain, ...(website.domains || []).map((domain) => domain.name)].filter(Boolean)
		)
	];
}

/**
 * @param {'letsencrypt' | 'custom'} mode
 * @param {{ websiteId: string, domain: string, certificatePEM: string, privateKeyPEM: string }} values
 */
export function buildSSLInstallRequest(mode, values) {
	const base = {
		website_id: values.websiteId,
		domain: values.domain.trim()
	};
	if (mode === 'custom') {
		return {
			path: '/api/v1/ssl/custom',
			body: {
				...base,
				certificate_pem: values.certificatePEM,
				private_key_pem: values.privateKeyPEM
			}
		};
	}
	return { path: '/api/v1/ssl/issue', body: base };
}

/**
 * @param {'letsencrypt' | 'custom'} mode
 * @param {string} websiteID
 * @param {{ domain: string, certificatePEM?: string, privateKeyPEM?: string }} values
 */
export function buildWebsiteSSLInstallRequest(mode, websiteID, values) {
	const operationAPI = websiteOperationAPI(websiteID);
	const body = { domain: values.domain.trim() };

	if (mode === 'custom') {
		return {
			path: operationAPI.sslCustom,
			body: {
				...body,
				certificate_pem: values.certificatePEM || '',
				private_key_pem: values.privateKeyPEM || ''
			}
		};
	}

	return { path: operationAPI.sslIssue, body };
}

/**
 * @param {{ status?: string, error_message?: string }} certificate
 * @returns {string}
 */
export function certificateInstallError(certificate) {
	if (certificate?.status !== 'failed') return '';
	return certificate.error_message || 'Certificate installation failed';
}
import { websiteOperationAPI } from './website-operations.js';
