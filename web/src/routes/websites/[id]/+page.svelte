<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';

	interface WebsiteDomain {
		id: string;
		name: string;
		type: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		php_version: string;
		status: string;
		error_message?: string;
		domains?: WebsiteDomain[];
		created_at: string;
	}

	let website = $state<Website | null>(null);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Domains
	let addDomainName = $state('');
	let addDomainType = $state('alias');
	let addingDomain = $state(false);

	// Config editor
	let configContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configSaveMsg = $state('');
	let showConfig = $state(false);

	// Logs
	let logTab = $state<'access' | 'error'>('access');
	let accessLogs = $state('');
	let errorLogs = $state('');
	let logsLoading = $state(false);
	let logsError = $state('');
	let showLogs = $state(false);

	// Delete confirm
	let deleteConfirm = $state(false);

	const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'active':
				return 'bg-green-900 text-green-300';
			case 'pending':
			case 'installing':
			case 'configuring':
			case 'validating':
				return 'bg-yellow-900 text-yellow-300 animate-pulse';
			case 'failed':
				return 'bg-red-900 text-red-300';
			case 'suspended':
			case 'disabled':
				return 'bg-gray-700 text-gray-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function domainTypeBadgeClass(type: string): string {
		switch (type) {
			case 'primary':
				return 'bg-blue-900 text-blue-300';
			case 'alias':
				return 'bg-purple-900 text-purple-300';
			case 'subdomain':
				return 'bg-cyan-900 text-cyan-300';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	async function loadWebsite() {
		loading = true;
		error = '';
		try {
			website = await api.get<Website>(`/api/v1/websites/${page.params.id}`);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load website';
		} finally {
			loading = false;
		}
	}

	async function addDomain() {
		if (!website || !addDomainName.trim()) return;
		addingDomain = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/domains`, {
				name: addDomainName,
				type: addDomainType
			});
			actionMsg = `Domain "${addDomainName}" added.`;
			addDomainName = '';
			addDomainType = 'alias';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to add domain';
		} finally {
			addingDomain = false;
		}
	}

	async function removeDomain(domainId: string) {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}/domains/${domainId}`);
			actionMsg = 'Domain removed.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to remove domain';
		}
	}

	async function loadConfig() {
		if (!website) return;
		configLoading = true;
		configError = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/websites/${website.id}/config`);
			configContent = data.content || '';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to load config';
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		if (!website) return;
		configSaveMsg = '';
		configError = '';
		try {
			await api.put(`/api/v1/websites/${website.id}/config`, { content: configContent });
			configSaveMsg = 'Configuration saved successfully.';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to save config';
		}
	}

	async function loadLogs(type: 'access' | 'error') {
		if (!website) return;
		logsLoading = true;
		logsError = '';
		try {
			const data = await api.get<{ content: string }>(
				`/api/v1/websites/${website.id}/logs/${type}?lines=100`
			);
			if (type === 'access') {
				accessLogs = data.content || '';
			} else {
				errorLogs = data.content || '';
			}
		} catch (err) {
			logsError = err instanceof Error ? err.message : 'Failed to load logs';
		} finally {
			logsLoading = false;
		}
	}

	async function suspendWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/suspend`);
			actionMsg = 'Website suspended.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to suspend website';
		}
	}

	async function enableWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/enable`);
			actionMsg = 'Website enabled.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to enable website';
		}
	}

	async function retryWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/retry`);
			actionMsg = 'Retry initiated.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to retry';
		}
	}

	async function deleteWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}`);
			goto('/websites');
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete website';
			deleteConfirm = false;
		}
	}

	onMount(loadWebsite);
</script>

<div class="space-y-6">
	<!-- Back link -->
	<a href="/websites" class="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white transition-colors">
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
			<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
		</svg>
		Back to Websites
	</a>

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

	{#if loading}
		<div class="text-gray-400">Loading website details...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if website}
		<!-- Header -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<h2 class="text-2xl font-bold text-white">{website.domain}</h2>
				<span class="inline-block px-2.5 py-0.5 rounded text-xs font-medium {statusBadgeClass(website.status)}">
					{website.status}
				</span>
			</div>
			<div class="flex flex-wrap gap-4 text-sm text-gray-400">
				<span>App Type: <span class="text-gray-200 capitalize">{website.app_type}</span></span>
				{#if website.app_type !== 'static'}
					<span>PHP Version: <span class="text-gray-200">{website.php_version}</span></span>
				{/if}
			</div>

			{#if website.status === 'failed' && website.error_message}
				<div class="mt-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{website.error_message}
				</div>
			{/if}

			{#if pendingStatuses.includes(website.status)}
				<div class="mt-3 p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-yellow-300 text-sm">
					Provisioning in progress: <span class="font-medium">{website.status}</span>
				</div>
			{/if}

			<!-- Actions -->
			<div class="mt-4 flex flex-wrap gap-2">
				{#if website.status === 'active'}
					<button
						onclick={suspendWebsite}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Suspend
					</button>
				{/if}
				{#if website.status === 'suspended' || website.status === 'disabled'}
					<button
						onclick={enableWebsite}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Enable
					</button>
				{/if}
				{#if website.status === 'failed'}
					<button
						onclick={retryWebsite}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Retry
					</button>
				{/if}

				{#if deleteConfirm}
					<div class="flex items-center gap-2 p-2 bg-red-900/30 border border-red-700 rounded-lg">
						<span class="text-sm text-red-300">Are you sure? This cannot be undone.</span>
						<button
							onclick={deleteWebsite}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Yes, Delete
						</button>
						<button
							onclick={() => (deleteConfirm = false)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Cancel
						</button>
					</div>
				{:else}
					<button
						onclick={() => (deleteConfirm = true)}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Delete
					</button>
				{/if}
			</div>
		</div>

		<!-- Domains -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Domains</h3>

			{#if website.domains && website.domains.length > 0}
				<div class="space-y-2 mb-4">
					{#each website.domains as domain}
						<div class="flex items-center justify-between p-3 bg-gray-900 rounded-lg">
							<div class="flex items-center gap-3">
								<span class="text-sm text-gray-200">{domain.name}</span>
								<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {domainTypeBadgeClass(domain.type)}">
									{domain.type}
								</span>
							</div>
							{#if domain.type !== 'primary'}
								<button
									onclick={() => removeDomain(domain.id)}
									class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
								>
									Remove
								</button>
							{/if}
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-sm text-gray-400 mb-4">No additional domains configured.</p>
			{/if}

			<!-- Add Domain Form -->
			<div class="flex flex-wrap items-end gap-3">
				<div>
					<label for="add-domain-name" class="block text-sm text-gray-400 mb-1">Domain Name</label>
					<input
						id="add-domain-name"
						type="text"
						bind:value={addDomainName}
						placeholder="sub.example.com"
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="add-domain-type" class="block text-sm text-gray-400 mb-1">Type</label>
					<select
						id="add-domain-type"
						bind:value={addDomainType}
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="alias">Alias</option>
						<option value="subdomain">Subdomain</option>
					</select>
				</div>
				<button
					onclick={addDomain}
					disabled={addingDomain || !addDomainName.trim()}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{addingDomain ? 'Adding...' : 'Add Domain'}
				</button>
			</div>
		</div>

		<!-- Nginx Config -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-3">
				<h3 class="text-lg font-semibold text-white">Nginx Configuration</h3>
				{#if !showConfig}
					<button
						onclick={() => { showConfig = true; loadConfig(); }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Edit Config
					</button>
				{:else}
					<button
						onclick={() => (showConfig = false)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Close
					</button>
				{/if}
			</div>

			{#if showConfig}
				{#if configError}
					<div class="mb-2 text-red-400 text-sm">{configError}</div>
				{/if}
				{#if configSaveMsg}
					<div class="mb-2 text-green-400 text-sm">
						{configSaveMsg}
						<button onclick={() => (configSaveMsg = '')} class="ml-2 hover:underline cursor-pointer">Dismiss</button>
					</div>
				{/if}

				{#if configLoading}
					<div class="text-gray-400 text-sm">Loading configuration...</div>
				{:else}
					<textarea
						bind:value={configContent}
						rows={20}
						class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
					></textarea>
					<div class="mt-2">
						<button
							onclick={saveConfig}
							class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
						>
							Save Configuration
						</button>
					</div>
				{/if}
			{/if}
		</div>

		<!-- Logs -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-3">
				<h3 class="text-lg font-semibold text-white">Logs</h3>
				{#if !showLogs}
					<button
						onclick={() => { showLogs = true; loadLogs(logTab); }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						View Logs
					</button>
				{:else}
					<button
						onclick={() => (showLogs = false)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Close
					</button>
				{/if}
			</div>

			{#if showLogs}
				<div class="flex gap-2 mb-4">
					<button
						onclick={() => { logTab = 'access'; loadLogs('access'); }}
						class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'access'
							? 'bg-blue-600 text-white'
							: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
					>
						Access Log
					</button>
					<button
						onclick={() => { logTab = 'error'; loadLogs('error'); }}
						class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'error'
							? 'bg-blue-600 text-white'
							: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
					>
						Error Log
					</button>
				</div>

				{#if logsError}
					<div class="mb-2 text-red-400 text-sm">{logsError}</div>
				{/if}

				<div class="flex items-center justify-end mb-2">
					<button
						onclick={() => loadLogs(logTab)}
						disabled={logsLoading}
						class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer"
					>
						{logsLoading ? 'Loading...' : 'Refresh'}
					</button>
				</div>

				<textarea
					readonly
					value={logTab === 'access' ? accessLogs : errorLogs}
					class="w-full h-64 bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-xs font-mono resize-y focus:outline-none"
				></textarea>
			{/if}
		</div>
	{/if}
</div>
