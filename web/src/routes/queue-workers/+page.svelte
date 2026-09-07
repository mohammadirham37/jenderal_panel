<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface QueueWorker {
		id: string;
		website_id: string;
		website_domain?: string;
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

	let workers = $state<QueueWorker[]>([]);
	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let showCreateForm = $state(false);
	let createWebsiteId = $state('');
	let createCommand = $state('php artisan queue:work');
	let createNumWorkers = $state(1);
	let creating = $state(false);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running':
				return 'bg-green-900/50 text-green-400';
			case 'stopped':
				return 'bg-gray-700 text-gray-400';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			case 'starting':
			case 'restarting':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function getWebsiteDomain(websiteId: string): string {
		const w = websites.find((ws) => ws.id === websiteId);
		return w ? w.domain : websiteId;
	}

	async function loadWorkers() {
		loading = true;
		error = '';
		try {
			workers = (await api.get<QueueWorker[]>('/api/v1/queue-workers')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load queue workers';
		} finally {
			loading = false;
		}
	}

	async function loadWebsites() {
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
		} catch {
			// Non-critical
		}
	}

	async function createWorker() {
		if (!createWebsiteId || !createCommand.trim() || createNumWorkers < 1) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/queue-workers', {
				website_id: createWebsiteId,
				command: createCommand.trim(),
				num_workers: createNumWorkers
			});
			actionMsg = 'Queue worker created successfully.';
			showCreateForm = false;
			createWebsiteId = '';
			createCommand = 'php artisan queue:work';
			createNumWorkers = 1;
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create queue worker';
		} finally {
			creating = false;
		}
	}

	async function startWorker(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/queue-workers/${id}/start`);
			actionMsg = 'Worker started.';
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to start worker';
		}
	}

	async function stopWorker(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/queue-workers/${id}/stop`);
			actionMsg = 'Worker stopped.';
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to stop worker';
		}
	}

	async function restartWorker(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/queue-workers/${id}/restart`);
			actionMsg = 'Worker restarted.';
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to restart worker';
		}
	}

	async function deleteWorker(id: string) {
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/queue-workers/${id}`);
			actionMsg = 'Queue worker deleted.';
			await loadWorkers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete queue worker';
		}
	}

	function openCreateForm() {
		showCreateForm = true;
		loadWebsites();
	}

	onMount(() => {
		loadWorkers();
		loadWebsites();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Queue Workers</h2>
		<button
			onclick={() => (showCreateForm ? (showCreateForm = false) : openCreateForm())}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showCreateForm ? 'Cancel' : 'Add Worker'}
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
			<h3 class="text-lg font-semibold text-white mb-4">New Queue Worker</h3>
			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="qw-website" class="block text-sm text-gray-400 mb-1">Website</label>
					<select
						id="qw-website"
						bind:value={createWebsiteId}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="">Select a website...</option>
						{#each websites as website}
							<option value={website.id}>{website.domain}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="qw-command" class="block text-sm text-gray-400 mb-1">Command</label>
					<input
						id="qw-command"
						type="text"
						bind:value={createCommand}
						placeholder="php artisan queue:work"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="qw-workers" class="block text-sm text-gray-400 mb-1">Number of Workers</label>
					<input
						id="qw-workers"
						type="number"
						bind:value={createNumWorkers}
						min="1"
						max="10"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			</div>
			<div class="mt-4">
				<button
					onclick={createWorker}
					disabled={creating || !createWebsiteId || !createCommand.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Creating...' : 'Create'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Workers Table -->
	{#if loading}
		<div class="text-gray-400">Loading queue workers...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if workers.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No queue workers configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Website</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Command</th>
							<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Workers</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each workers as worker}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-gray-300">{worker.website_domain || getWebsiteDomain(worker.website_id)}</td>
								<td class="px-4 py-3 text-sm">
									<code class="text-gray-200 bg-gray-900 px-1.5 py-0.5 rounded text-xs">{worker.command}</code>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300 text-center">{worker.num_workers}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(worker.status)}">
										{worker.status}
									</span>
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if worker.status === 'stopped' || worker.status === 'failed'}
											<button
												onclick={() => startWorker(worker.id)}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Start
											</button>
										{/if}
										{#if worker.status === 'running'}
											<button
												onclick={() => stopWorker(worker.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Stop
											</button>
										{/if}
										{#if worker.status === 'running' || worker.status === 'failed'}
											<button
												onclick={() => restartWorker(worker.id)}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Restart
											</button>
										{/if}
										{#if deleteConfirmId === worker.id}
											<span class="text-xs text-red-400">Delete?</span>
											<button
												onclick={() => deleteWorker(worker.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteConfirmId = worker.id)}
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
