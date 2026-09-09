<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';

	interface QueueWorker {
		id: string;
		website_id: string;
		command: string;
		num_workers: number;
		status: string;
		created_at: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let websiteID = $derived(page.params.id ?? '');
	let operationAPI = $derived(websiteOperationAPI(websiteID));
	let website = $state<Website | null>(null);
	let loadedWebsiteID = $state('');
	let currentWebsite = $derived(loadedWebsiteID === websiteID ? website : null);
	let workers = $state<QueueWorker[]>([]);
	let loading = $state(false);
	let loadingWebsite = $state(true);
	let websiteError = $state('');
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let showCreateForm = $state(false);
	let createCommand = $state('php artisan queue:work');
	let createNumWorkers = $state(1);
	let creating = $state(false);
	let deleteConfirmId = $state<string | null>(null);
	let websiteLoadGeneration = 0;

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running': return 'bg-green-900/50 text-green-400';
			case 'stopped': return 'bg-gray-700 text-gray-400';
			case 'failed': return 'bg-red-900/50 text-red-400';
			case 'starting':
			case 'restarting': return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function isCurrentRequest(requestedWebsiteID: string, generation: number): boolean {
		return websiteID === requestedWebsiteID && websiteLoadGeneration === generation;
	}

	function isCurrentRouteWebsite(requestedWebsiteID: string, generation: number): boolean {
		return isCurrentRequest(requestedWebsiteID, generation) && loadedWebsiteID === requestedWebsiteID && website !== null;
	}

	function resetForWebsiteChange() {
		website = null;
		loadedWebsiteID = '';
		workers = [];
		loading = false;
		error = '';
		actionMsg = '';
		actionError = '';
		showCreateForm = false;
		createCommand = 'php artisan queue:work';
		createNumWorkers = 1;
		creating = false;
		deleteConfirmId = null;
	}

	async function loadWebsite(requestedWebsiteID: string) {
		const generation = ++websiteLoadGeneration;
		loadingWebsite = true;
		websiteError = '';
		resetForWebsiteChange();
		if (!requestedWebsiteID) {
			websiteError = 'Website ID is required';
			loadingWebsite = false;
			return;
		}
		const scopedAPI = websiteOperationAPI(requestedWebsiteID);
		try {
			const loadedWebsite = await api.get<Website>(scopedAPI.website);
			if (!isCurrentRequest(requestedWebsiteID, generation)) return;
			website = loadedWebsite;
			loadedWebsiteID = requestedWebsiteID;
			await loadWorkers(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				websiteError = err instanceof Error ? err.message : 'Failed to load website';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) loadingWebsite = false;
		}
	}

	async function loadWorkers(
		scopedAPI = operationAPI,
		requestedWebsiteID = websiteID,
		generation = websiteLoadGeneration
	) {
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) {
			workers = [];
			return;
		}
		loading = true;
		error = '';
		try {
			const nextWorkers = (await api.get<QueueWorker[]>(scopedAPI.queueWorkers)) || [];
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			workers = nextWorkers;
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				error = err instanceof Error ? err.message : 'Failed to load queue workers';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) loading = false;
		}
	}

	async function createWorker() {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation) || !createCommand.trim() || createNumWorkers < 1) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(scopedAPI.queueWorkers, { command: createCommand.trim(), num_workers: createNumWorkers });
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = 'Queue worker created successfully.';
			showCreateForm = false;
			createCommand = 'php artisan queue:work';
			createNumWorkers = 1;
			await loadWorkers(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to create queue worker';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) creating = false;
		}
	}

	async function runWorkerAction(id: string, action: 'start' | 'stop' | 'restart', success: string, failure: string) {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/queue-workers/${id}/${action}`);
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = success;
			await loadWorkers(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : failure;
			}
		}
	}

	async function deleteWorker(id: string) {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/queue-workers/${id}`);
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = 'Queue worker deleted.';
			await loadWorkers(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to delete queue worker';
			}
		}
	}

	$effect(() => {
		void loadWebsite(websiteID);
	});
</script>

<div class="space-y-6">
	{#if loadingWebsite}
		<div class="text-gray-400">Loading website...</div>
	{:else if websiteError}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{websiteError}</div>
	{:else if currentWebsite}
		<div class="flex items-center justify-between">
			<div>
				<a href="/websites" class="text-sm text-blue-400 hover:text-blue-300">Websites</a>
				<div class="flex flex-wrap items-center gap-3">
					<h2 class="text-2xl font-bold text-white">Queue Workers · {currentWebsite.domain}</h2>
					<span aria-label="Website status" class="inline-block px-2.5 py-0.5 rounded bg-gray-700 text-xs font-medium text-gray-300">{currentWebsite.status}</span>
				</div>
			</div>
			<button onclick={() => (showCreateForm = !showCreateForm)} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">{showCreateForm ? 'Cancel' : 'Add Worker'}</button>
		</div>

		<WebsiteSectionNav websiteId={currentWebsite.id} currentPath={page.url.pathname} />

		{#if actionMsg}
			<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">{actionMsg} <button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button></div>
		{/if}
		{#if actionError}
			<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">{actionError} <button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button></div>
		{/if}

		{#if showCreateForm}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-4">New Queue Worker</h3>
				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div>
						<label for="qw-command" class="block text-sm text-gray-400 mb-1">Command</label>
						<input id="qw-command" type="text" bind:value={createCommand} placeholder="php artisan queue:work" class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
					</div>
					<div>
						<label for="qw-workers" class="block text-sm text-gray-400 mb-1">Number of Workers</label>
						<input id="qw-workers" type="number" bind:value={createNumWorkers} min="1" max="10" class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
					</div>
				</div>
				<div class="mt-4"><button onclick={createWorker} disabled={creating || !createCommand.trim() || createNumWorkers < 1} class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer">{creating ? 'Creating...' : 'Create'}</button></div>
			</div>
		{/if}

		{#if loading}
			<div class="text-gray-400">Loading queue workers...</div>
		{:else if error}
			<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
		{:else if workers.length === 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center"><p class="text-gray-400">No queue workers configured yet.</p></div>
		{:else}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden"><div class="overflow-x-auto"><table class="w-full">
				<thead><tr class="border-b border-gray-700">
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Command</th>
					<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Workers</th>
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
					<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
				</tr></thead>
				<tbody class="divide-y divide-gray-700">
					{#each workers as worker}
						<tr class="hover:bg-gray-750">
							<td class="px-4 py-3 text-sm"><code class="text-gray-200 bg-gray-900 px-1.5 py-0.5 rounded text-xs">{worker.command}</code></td>
							<td class="px-4 py-3 text-sm text-gray-300 text-center">{worker.num_workers}</td>
							<td class="px-4 py-3"><span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(worker.status)}">{worker.status}</span></td>
							<td class="px-4 py-3 text-right"><div class="flex items-center justify-end gap-2">
								{#if worker.status === 'stopped' || worker.status === 'failed'}<button onclick={() => runWorkerAction(worker.id, 'start', 'Worker started.', 'Failed to start worker')} class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer">Start</button>{/if}
								{#if worker.status === 'running'}<button onclick={() => runWorkerAction(worker.id, 'stop', 'Worker stopped.', 'Failed to stop worker')} class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer">Stop</button>{/if}
								{#if worker.status === 'running' || worker.status === 'failed'}<button onclick={() => runWorkerAction(worker.id, 'restart', 'Worker restarted.', 'Failed to restart worker')} class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer">Restart</button>{/if}
								{#if deleteConfirmId === worker.id}
									<span class="text-xs text-red-400">Delete?</span><button onclick={() => deleteWorker(worker.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Yes</button><button onclick={() => (deleteConfirmId = null)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
								{:else}
									<button onclick={() => (deleteConfirmId = worker.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
								{/if}
							</div></td>
						</tr>
					{/each}
				</tbody>
			</table></div></div>
		{/if}
	{/if}
</div>
