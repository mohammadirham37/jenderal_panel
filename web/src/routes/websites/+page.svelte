<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		php_version: string;
		status: string;
		error_message?: string;
		created_at: string;
	}

	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let showCreateForm = $state(false);
	let createDomain = $state('');
	let createAppType = $state('php');
	let createPhpVersion = $state('8.3');
	let creating = $state(false);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	const phpVersions = ['8.1', '8.2', '8.3', '8.4'];
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

	function shouldPoll(sites: Website[]): boolean {
		return sites.some((w) => pendingStatuses.includes(w.status));
	}

	function startPolling() {
		stopPolling();
		pollTimer = setInterval(async () => {
			try {
				websites = (await api.get<Website[]>('/api/v1/websites')) || [];
				if (!shouldPoll(websites)) {
					stopPolling();
				}
			} catch {
				// Silently ignore polling errors
			}
		}, 3000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	async function loadWebsites() {
		loading = true;
		error = '';
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
			if (shouldPoll(websites)) {
				startPolling();
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load websites';
		} finally {
			loading = false;
		}
	}

	async function createWebsite() {
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			const body: Record<string, string> = {
				domain: createDomain,
				app_type: createAppType
			};
			if (createAppType !== 'static') {
				body.php_version = createPhpVersion;
			}
			await api.post('/api/v1/websites', body);
			actionMsg = `Website "${createDomain}" creation started.`;
			showCreateForm = false;
			createDomain = '';
			createAppType = 'php';
			createPhpVersion = '8.3';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create website';
		} finally {
			creating = false;
		}
	}

	async function suspendWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/suspend`);
			actionMsg = 'Website suspended.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to suspend website';
		}
	}

	async function enableWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/enable`);
			actionMsg = 'Website enabled.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to enable website';
		}
	}

	async function deleteWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		deleteConfirmId = null;
		try {
			await api.del(`/api/v1/websites/${id}`);
			actionMsg = 'Website deleted.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete website';
		}
	}

	async function retryWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/retry`);
			actionMsg = 'Website retry initiated.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to retry website';
		}
	}

	onMount(loadWebsites);

	onDestroy(() => {
		stopPolling();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Websites</h2>
		<button
			onclick={() => (showCreateForm = !showCreateForm)}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showCreateForm ? 'Cancel' : 'Create Website'}
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

	<!-- Create Form -->
	{#if showCreateForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">New Website</h3>
			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="create-domain" class="block text-sm text-gray-400 mb-1">Domain</label>
					<input
						id="create-domain"
						type="text"
						bind:value={createDomain}
						placeholder="example.com"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="create-app-type" class="block text-sm text-gray-400 mb-1">App Type</label>
					<select
						id="create-app-type"
						bind:value={createAppType}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="php">PHP</option>
						<option value="static">Static</option>
						<option value="laravel">Laravel</option>
					</select>
				</div>
				{#if createAppType !== 'static'}
					<div>
						<label for="create-php-version" class="block text-sm text-gray-400 mb-1">PHP Version</label>
						<select
							id="create-php-version"
							bind:value={createPhpVersion}
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						>
							{#each phpVersions as v}
								<option value={v}>{v}</option>
							{/each}
						</select>
					</div>
				{/if}
			</div>
			<div class="mt-4">
				<button
					onclick={createWebsite}
					disabled={creating || !createDomain.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Creating...' : 'Create'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Websites Table -->
	{#if loading}
		<div class="text-gray-400">Loading websites...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if websites.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No websites configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Domain</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">App Type</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">PHP Version</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each websites as website}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3">
									<a href="/websites/{website.id}" class="text-sm text-blue-400 hover:text-blue-300 font-medium hover:underline">
										{website.domain}
									</a>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300 capitalize">{website.app_type}</td>
								<td class="px-4 py-3 text-sm text-gray-300">{website.app_type === 'static' ? '-' : website.php_version}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(website.status)}">
										{website.status}
									</span>
									{#if website.status === 'failed' && website.error_message}
										<p class="mt-1 text-xs text-red-400">{website.error_message}</p>
									{/if}
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if website.status === 'failed'}
											<button
												onclick={() => retryWebsite(website.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Retry
											</button>
										{/if}
										{#if website.status === 'active'}
											<button
												onclick={() => suspendWebsite(website.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Suspend
											</button>
										{/if}
										{#if website.status === 'suspended' || website.status === 'disabled'}
											<button
												onclick={() => enableWebsite(website.id)}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Enable
											</button>
										{/if}
										{#if deleteConfirmId === website.id}
											<span class="text-xs text-red-400">Confirm?</span>
											<button
												onclick={() => deleteWebsite(website.id)}
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
												onclick={() => (deleteConfirmId = website.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
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
