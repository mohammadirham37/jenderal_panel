<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { websiteOperationAPI } from '$lib/website-operations.js';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';
	import {
		buildWebsiteSSLInstallRequest,
		certificateInstallError,
		domainsForWebsite
	} from '$lib/ssl-form.js';

	interface SSLCertificate {
		id: string;
		website_id: string;
		domain: string;
		issuer: string;
		status: string;
		expires_at: string;
		auto_renew: boolean;
		created_at: string;
	}

	interface WebsiteLite {
		id: string;
		domain: string;
		status: string;
		domains?: { name: string; type?: string }[];
	}

	let { website }: { website: WebsiteLite | null } = $props();

	let websiteID = $derived(website?.id ?? '');
	let certificates = $state<SSLCertificate[]>([]);
	let loading = $state(false);
	let error = $state('');
	let actionInProgress = $state(false);

	// Install form
	let showIssueForm = $state(false);
	let wildcardIssue = $state(false);
	let wildcardToken = $state('');
	let issueDomain = $state('');
	let installMode = $state<'letsencrypt' | 'custom'>('letsencrypt');
	let certificatePEM = $state('');
	let privateKeyPEM = $state('');
	let issuing = $state(false);
	let issueDomains = $derived(website ? domainsForWebsite([website], website.id) : []);

	// Confirm dialogs
	let revokeConfirmId = $state<string | null>(null);
	let deleteConfirmId = $state<string | null>(null);

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let loadGeneration = 0;

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'active':
				return 'bg-green-900/50 text-green-400';
			case 'pending':
			case 'issuing':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'expired':
				return 'bg-red-900/50 text-red-400';
			case 'revoked':
				return 'bg-gray-700 text-gray-400';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '-';
		const d = new Date(dateStr);
		return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
	}

	function daysUntilExpiry(dateStr: string): number {
		if (!dateStr) return Infinity;
		const now = new Date();
		const expiry = new Date(dateStr);
		return Math.ceil((expiry.getTime() - now.getTime()) / (1000 * 60 * 60 * 24));
	}

	function expiryRowClass(cert: SSLCertificate): string {
		if (cert.status !== 'active') return '';
		const days = daysUntilExpiry(cert.expires_at);
		if (days < 7) return 'bg-red-950/40';
		if (days < 30) return 'bg-yellow-950/30';
		return '';
	}

	function expiryTextClass(cert: SSLCertificate): string {
		if (cert.status !== 'active') return 'text-gray-400';
		const days = daysUntilExpiry(cert.expires_at);
		if (days < 7) return 'text-red-400 font-medium';
		if (days < 30) return 'text-yellow-400 font-medium';
		return 'text-gray-300';
	}

	function hasPending(certs: SSLCertificate[]): boolean {
		return certs.some((c) => c.status === 'pending' || c.status === 'issuing');
	}

	function isCurrent(requestedWebsiteID: string, generation: number): boolean {
		return !!website && website.id === requestedWebsiteID && loadGeneration === generation;
	}

	function startPolling(requestedWebsiteID: string, generation: number) {
		stopPolling();
		pollTimer = setInterval(async () => {
			if (!isCurrent(requestedWebsiteID, generation)) {
				stopPolling();
				return;
			}
			try {
				const nextCertificates = (await api.get<SSLCertificate[]>(websiteOperationAPI(requestedWebsiteID).ssl)) || [];
				if (!isCurrent(requestedWebsiteID, generation)) return;
				certificates = nextCertificates;
				if (!hasPending(certificates)) {
					stopPolling();
				}
			} catch {
				// Silently ignore polling errors
			}
		}, 5000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	async function loadCertificates(requestedWebsiteID: string) {
		if (!requestedWebsiteID || !website) {
			certificates = [];
			return;
		}
		const generation = ++loadGeneration;
		stopPolling();
		loading = true;
		error = '';
		try {
			const nextCertificates = (await api.get<SSLCertificate[]>(websiteOperationAPI(requestedWebsiteID).ssl)) || [];
			if (!isCurrent(requestedWebsiteID, generation)) return;
			certificates = nextCertificates;
			if (hasPending(certificates)) {
				startPolling(requestedWebsiteID, generation);
			}
		} catch (err) {
			if (isCurrent(requestedWebsiteID, generation)) {
				error = err instanceof Error ? err.message : translate($language, 'wss.errLoad');
			}
		} finally {
			if (isCurrent(requestedWebsiteID, generation)) {
				loading = false;
			}
		}
	}

	async function issueCertificate() {
		const requestedWebsiteID = websiteID;
		if (!website || !formIsValid()) return;
		issuing = true;
		try {
			const request = buildWebsiteSSLInstallRequest(installMode, requestedWebsiteID, {
				domain: issueDomain,
				certificatePEM,
				privateKeyPEM,
				wildcard: wildcardIssue,
				cfToken: wildcardToken
			});
			const installed = await api.post<SSLCertificate>(request.path, request.body);
			if (!isCurrent(requestedWebsiteID, loadGeneration)) return;
			const installError = certificateInstallError(installed);
			if (installError) throw new Error(installError);
			toast.success(
				installMode === 'custom'
					? translate($language, 'wss.installedCustom').replace('{domain}', issueDomain)
					: translate($language, 'wss.installedLE').replace('{domain}', issueDomain)
			);
			closeIssueForm();
			await loadCertificates(requestedWebsiteID);
		} catch (err) {
			if (isCurrent(requestedWebsiteID, loadGeneration)) {
				toast.error(err instanceof Error ? err.message : translate($language, 'wss.errInstall'));
			}
		} finally {
			issuing = false;
		}
	}

	function formIsValid(): boolean {
		if (!website || !issueDomain) return false;
		if (installMode === 'custom') return !!certificatePEM.trim() && !!privateKeyPEM.trim();
		return true;
	}

	async function renewCertificate(cert: SSLCertificate) {
		if (!website) return;
		actionInProgress = true;
		try {
			await api.post(`/api/v1/websites/${website.id}/ssl/${cert.id}/renew`);
			toast.success(translate($language, 'wss.renewStarted').replace('{domain}', cert.domain));
			await loadCertificates(website.id);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wss.errRenew'));
		} finally {
			actionInProgress = false;
		}
	}

	async function revokeCertificate(id: string) {
		if (!website) return;
		revokeConfirmId = null;
		actionInProgress = true;
		try {
			await api.post(`/api/v1/websites/${website.id}/ssl/${id}/revoke`);
			toast.success(translate($language, 'wss.revoked'));
			await loadCertificates(website.id);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wss.errRevoke'));
		} finally {
			actionInProgress = false;
		}
	}

	async function deleteCertificate(id: string) {
		if (!website) return;
		deleteConfirmId = null;
		actionInProgress = true;
		try {
			await api.del(`/api/v1/websites/${website.id}/ssl/${id}`);
			toast.success(translate($language, 'wss.deleted'));
			await loadCertificates(website.id);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wss.errDelete'));
		} finally {
			actionInProgress = false;
		}
	}

	async function toggleAutoRenew(cert: SSLCertificate) {
		if (!website) return;
		try {
			await api.put(`/api/v1/websites/${website.id}/ssl/${cert.id}`, { auto_renew: !cert.auto_renew });
			cert.auto_renew = !cert.auto_renew;
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wss.errAutoRenew'));
		}
	}

	function openIssueForm() {
		installMode = 'letsencrypt';
		issueDomain = issueDomains[0] || '';
		certificatePEM = '';
		privateKeyPEM = '';
		wildcardIssue = false;
		wildcardToken = '';
		showIssueForm = true;
	}

	function openReplaceForm(cert: SSLCertificate) {
		installMode = 'custom';
		issueDomain = cert.domain;
		certificatePEM = '';
		privateKeyPEM = '';
		showIssueForm = true;
	}

	function closeIssueForm() {
		showIssueForm = false;
		issueDomain = '';
		installMode = 'letsencrypt';
		certificatePEM = '';
		privateKeyPEM = '';
	}

	$effect(() => {
		void loadCertificates(website?.id ?? '');
	});

	onDestroy(() => {
		loadGeneration++;
		stopPolling();
	});
</script>

<div class="space-y-6">


	<div class="flex items-center justify-between">
		<h3 class="text-lg font-semibold text-white">{translate($language, 'wss.title')}</h3>
		<button
			onclick={() => showIssueForm ? closeIssueForm() : openIssueForm()}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showIssueForm ? translate($language, 'wss.cancel') : translate($language, 'wss.installCert')}
		</button>
	</div>

	<!-- Install Certificate Form -->
	{#if showIssueForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h4 class="text-lg font-semibold text-white mb-4">{translate($language, 'wss.installTitle')}</h4>
			<div class="mb-4" role="group" aria-label={translate($language, 'wss.sourceAria')}>
				<div class="text-sm text-gray-400 mb-2">{translate($language, 'wss.certType')}</div>
				<div class="inline-flex rounded-lg border border-gray-600 p-1 bg-gray-900">
					<button
						type="button"
						onclick={() => (installMode = 'letsencrypt')}
						aria-pressed={installMode === 'letsencrypt'}
						class="px-3 py-1.5 rounded text-sm transition-colors cursor-pointer {installMode === 'letsencrypt' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
					>{translate($language, 'wss.leFree')}</button>
					<button
						type="button"
						onclick={() => (installMode = 'custom')}
						aria-pressed={installMode === 'custom'}
						class="px-3 py-1.5 rounded text-sm transition-colors cursor-pointer {installMode === 'custom' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
					>{translate($language, 'wss.customSsl')}</button>
				</div>
			</div>
			<div>
				<label for="ssl-domain" class="block text-sm text-gray-400 mb-1">{translate($language, 'wss.domain')}</label>
				<select
					id="ssl-domain"
					bind:value={issueDomain}
					class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					disabled={!website}
				>
					<option value="">{translate($language, 'wss.selectDomain')}</option>
					{#each issueDomains as domain}
						<option value={domain}>{domain}</option>
					{/each}
				</select>
			</div>
			{#if installMode === 'letsencrypt'}
				<div class="flex items-center gap-2 mt-2">
					<input id="ssl-wildcard" type="checkbox" bind:checked={wildcardIssue}
						class="h-4 w-4 rounded border-gray-600 bg-gray-900 text-blue-600 focus:ring-blue-500" />
					<label for="ssl-wildcard" class="text-xs text-gray-300">
						{translate($language, 'wss.wildcardIssues')} <code class="font-mono">{issueDomain}</code> + <code class="font-mono">*.{issueDomain}</code> {translate($language, 'wss.viaDns01')}
					</label>
				</div>
				{#if wildcardIssue}
					<div class="mt-2">
						<label for="ssl-cf-token" class="block text-xs text-gray-400 mb-1">{translate($language, 'wss.cfToken')}</label>
						<input id="ssl-cf-token" type="password" bind:value={wildcardToken}
							placeholder={translate($language, 'wss.cfTokenPlaceholder')}
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
					</div>
				{/if}
			{/if}
			{#if installMode === 'custom'}
				<div class="mt-4 grid grid-cols-1 gap-4">
					<div>
						<label for="ssl-certificate-pem" class="block text-sm text-gray-400 mb-1">{translate($language, 'wss.certPem')}</label>
						<textarea
							id="ssl-certificate-pem"
							bind:value={certificatePEM}
							rows={8}
							spellcheck="false"
							placeholder="-----BEGIN CERTIFICATE-----"
							class="w-full px-3 py-2 bg-gray-950 border border-gray-600 rounded text-gray-200 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
						></textarea>
					</div>
					<div>
						<label for="ssl-private-key-pem" class="block text-sm text-gray-400 mb-1">{translate($language, 'wss.keyPem')}</label>
						<textarea
							id="ssl-private-key-pem"
							bind:value={privateKeyPEM}
							rows={8}
							spellcheck="false"
							placeholder="-----BEGIN PRIVATE KEY-----"
							class="w-full px-3 py-2 bg-gray-950 border border-gray-600 rounded text-gray-200 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
						></textarea>
						<p class="mt-1 text-xs text-gray-500">{translate($language, 'wss.keyHint')}</p>
					</div>
				</div>
			{/if}
			<div class="mt-4">
				<button
					onclick={issueCertificate}
					disabled={issuing || !formIsValid()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{issuing ? translate($language, 'wss.installing') : translate($language, 'wss.installEnable')}
				</button>
			</div>
		</div>
	{/if}

	<!-- Certificates Table -->
	{#if loading}
		<div class="text-gray-400">{translate($language, 'wss.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if certificates.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">{translate($language, 'wss.none')}</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.domain')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.issuer')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.status')}</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.expires')}</th>
							<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.autoRenew')}</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'wss.actions')}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each certificates as cert}
							<tr class="hover:bg-gray-750 {expiryRowClass(cert)}">
								<td class="px-4 py-3 text-sm text-white font-medium">{cert.domain}</td>
								<td class="px-4 py-3 text-sm text-gray-400">{cert.issuer || '-'}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(cert.status)}">
										{cert.status}
									</span>
								</td>
								<td class="px-4 py-3 text-sm {expiryTextClass(cert)}">
									{formatDate(cert.expires_at)}
									{#if cert.status === 'active'}
										{@const days = daysUntilExpiry(cert.expires_at)}
										{#if days < 30}
											<span class="ml-1 text-xs">{translate($language, 'wss.daysLeft').replace('{days}', String(days))}</span>
										{/if}
									{/if}
								</td>
								<td class="px-4 py-3 text-center">
									{#if cert.issuer === 'letsencrypt'}
										<button
											onclick={() => toggleAutoRenew(cert)}
											class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none {cert.auto_renew ? 'bg-blue-600' : 'bg-gray-600'}"
											role="switch"
											aria-checked={cert.auto_renew}
											aria-label={translate($language, 'wss.autoRenewAria').replace('{domain}', cert.domain)}
										>
											<span
												class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 {cert.auto_renew ? 'translate-x-4' : 'translate-x-0'}"
											></span>
										</button>
									{:else}
										<span class="text-xs text-gray-500">{translate($language, 'wss.manual')}</span>
									{/if}
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if cert.issuer === 'letsencrypt' && (cert.status === 'active' || cert.status === 'expired')}
											<button
												onclick={() => renewCertificate(cert)}
												disabled={actionInProgress}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'wss.renew')}
											</button>
										{/if}
										{#if cert.issuer === 'custom'}
											<button
												onclick={() => openReplaceForm(cert)}
												disabled={actionInProgress}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'wss.replace')}
											</button>
										{/if}
										{#if cert.issuer === 'letsencrypt' && cert.status === 'active'}
											{#if revokeConfirmId === cert.id}
												<span class="text-xs text-yellow-400">{translate($language, 'wss.confirmQ')}</span>
												<button
													onclick={() => revokeCertificate(cert.id)}
													class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.yesRevoke')}
												</button>
												<button
													onclick={() => (revokeConfirmId = null)}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.cancel')}
												</button>
											{:else}
												<button
													onclick={() => (revokeConfirmId = cert.id)}
													disabled={actionInProgress}
													class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.revoke')}
												</button>
											{/if}
										{/if}
										{#if deleteConfirmId === cert.id}
											<span class="text-xs text-red-400">{translate($language, 'wss.deleteQ')}</span>
												<button
													onclick={() => deleteCertificate(cert.id)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.yesDelete')}
												</button>
												<button
													onclick={() => (deleteConfirmId = null)}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.cancel')}
												</button>
										{:else}
												<button
													onclick={() => (deleteConfirmId = cert.id)}
													disabled={actionInProgress}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'wss.delete')}
												</button>
										{/if}
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
</div>
