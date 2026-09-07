<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';

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

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let certificates = $state<SSLCertificate[]>([]);
	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let actionInProgress = $state(false);

	// Issue form
	let showIssueForm = $state(false);
	let issueWebsiteId = $state('');
	let issueDomain = $state('');
	let issuing = $state(false);

	// Confirm dialogs
	let revokeConfirmId = $state<string | null>(null);
	let deleteConfirmId = $state<string | null>(null);

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;

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

	function startPolling() {
		stopPolling();
		pollTimer = setInterval(async () => {
			try {
				certificates = (await api.get<SSLCertificate[]>('/api/v1/ssl')) || [];
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

	async function loadCertificates() {
		loading = true;
		error = '';
		try {
			certificates = (await api.get<SSLCertificate[]>('/api/v1/ssl')) || [];
			if (hasPending(certificates)) {
				startPolling();
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load SSL certificates';
		} finally {
			loading = false;
		}
	}

	async function loadWebsites() {
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
		} catch {
			// Non-critical, just means dropdown won't populate
		}
	}

	function onWebsiteSelected() {
		const selected = websites.find((w) => w.id === issueWebsiteId);
		if (selected) {
			issueDomain = selected.domain;
		}
	}

	async function issueCertificate() {
		if (!issueWebsiteId || !issueDomain.trim()) return;
		issuing = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/ssl', {
				website_id: issueWebsiteId,
				domain: issueDomain.trim()
			});
			actionMsg = `SSL certificate issuance started for "${issueDomain}".`;
			showIssueForm = false;
			issueWebsiteId = '';
			issueDomain = '';
			await loadCertificates();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to issue certificate';
		} finally {
			issuing = false;
		}
	}

	async function renewCertificate(cert: SSLCertificate) {
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			await api.post(`/api/v1/ssl/${cert.id}/renew`);
			actionMsg = `Renewal started for "${cert.domain}".`;
			await loadCertificates();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to renew certificate';
		} finally {
			actionInProgress = false;
		}
	}

	async function revokeCertificate(id: string) {
		revokeConfirmId = null;
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			await api.post(`/api/v1/ssl/${id}/revoke`);
			actionMsg = 'Certificate revoked.';
			await loadCertificates();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to revoke certificate';
		} finally {
			actionInProgress = false;
		}
	}

	async function deleteCertificate(id: string) {
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			await api.del(`/api/v1/ssl/${id}`);
			actionMsg = 'Certificate deleted.';
			await loadCertificates();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete certificate';
		} finally {
			actionInProgress = false;
		}
	}

	async function toggleAutoRenew(cert: SSLCertificate) {
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/ssl/${cert.id}`, { auto_renew: !cert.auto_renew });
			cert.auto_renew = !cert.auto_renew;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update auto-renew';
		}
	}

	function openIssueForm() {
		showIssueForm = true;
		loadWebsites();
	}

	onMount(loadCertificates);

	onDestroy(() => {
		stopPolling();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">SSL Certificates</h2>
		<button
			onclick={() => showIssueForm ? (showIssueForm = false) : openIssueForm()}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showIssueForm ? 'Cancel' : 'Issue Certificate'}
		</button>
	</div>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	<!-- Issue Certificate Form -->
	{#if showIssueForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Issue New Certificate</h3>
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="ssl-website" class="block text-sm text-gray-400 mb-1">Website</label>
					<select
						id="ssl-website"
						bind:value={issueWebsiteId}
						onchange={onWebsiteSelected}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="">Select a website...</option>
						{#each websites as website}
							<option value={website.id}>{website.domain}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="ssl-domain" class="block text-sm text-gray-400 mb-1">Domain</label>
					<input
						id="ssl-domain"
						type="text"
						bind:value={issueDomain}
						placeholder="example.com"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			</div>
			<div class="mt-4">
				<button
					onclick={issueCertificate}
					disabled={issuing || !issueWebsiteId || !issueDomain.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{issuing ? 'Issuing...' : 'Issue'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Certificates Table -->
	{#if loading}
		<div class="text-gray-400">Loading SSL certificates...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if certificates.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No SSL certificates configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Domain</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Issuer</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Expires</th>
							<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Auto Renew</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
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
											<span class="ml-1 text-xs">({days}d left)</span>
										{/if}
									{/if}
								</td>
								<td class="px-4 py-3 text-center">
									<button
										onclick={() => toggleAutoRenew(cert)}
										class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none {cert.auto_renew ? 'bg-blue-600' : 'bg-gray-600'}"
										role="switch"
										aria-checked={cert.auto_renew}
										aria-label="Toggle auto-renew for {cert.domain}"
									>
										<span
											class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 {cert.auto_renew ? 'translate-x-4' : 'translate-x-0'}"
										></span>
									</button>
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if cert.status === 'active' || cert.status === 'expired'}
											<button
												onclick={() => renewCertificate(cert)}
												disabled={actionInProgress}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Renew
											</button>
										{/if}
										{#if cert.status === 'active'}
											{#if revokeConfirmId === cert.id}
												<span class="text-xs text-yellow-400">Confirm?</span>
												<button
													onclick={() => revokeCertificate(cert.id)}
													class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Yes, Revoke
												</button>
												<button
													onclick={() => (revokeConfirmId = null)}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Cancel
												</button>
											{:else}
												<button
													onclick={() => (revokeConfirmId = cert.id)}
													disabled={actionInProgress}
													class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Revoke
												</button>
											{/if}
										{/if}
										{#if deleteConfirmId === cert.id}
											<span class="text-xs text-red-400">Delete?</span>
											<button
												onclick={() => deleteCertificate(cert.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes, Delete
											</button>
											<button
												onclick={() => (deleteConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteConfirmId = cert.id)}
												disabled={actionInProgress}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
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
