<script lang="ts">
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';

	interface CronJob {
		id: string;
		website_id: string;
		command: string;
		schedule: string;
		enabled: boolean;
		last_run: string;
		last_status: string;
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
	let cronJobs = $state<CronJob[]>([]);
	let loading = $state(false);
	let loadingWebsite = $state(true);
	let websiteError = $state('');
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let showCreateForm = $state(false);
	let createCommand = $state('');
	let createSchedule = $state('');
	let creating = $state(false);

	let editingId = $state<string | null>(null);
	let editCommand = $state('');
	let editSchedule = $state('');
	let saving = $state(false);
	let deleteConfirmId = $state<string | null>(null);
	let websiteLoadGeneration = 0;

	const schedulePresets = [
		{ label: 'Every minute', value: '* * * * *' },
		{ label: 'Every 5 minutes', value: '*/5 * * * *' },
		{ label: 'Every 15 minutes', value: '*/15 * * * *' },
		{ label: 'Hourly', value: '0 * * * *' },
		{ label: 'Daily at midnight', value: '0 0 * * *' },
		{ label: 'Weekly (Sunday)', value: '0 0 * * 0' },
		{ label: 'Monthly', value: '0 0 1 * *' }
	];

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'success': return 'bg-green-900/50 text-green-400';
			case 'running': return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'failed': return 'bg-red-900/50 text-red-400';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '-';
		return new Date(dateStr).toLocaleString('en-US', {
			month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
		});
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
		cronJobs = [];
		loading = false;
		error = '';
		actionMsg = '';
		actionError = '';
		showCreateForm = false;
		createCommand = '';
		createSchedule = '';
		creating = false;
		editingId = null;
		editCommand = '';
		editSchedule = '';
		saving = false;
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
			await loadCronJobs(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				websiteError = err instanceof Error ? err.message : 'Failed to load website';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) loadingWebsite = false;
		}
	}

	async function loadCronJobs(
		scopedAPI = operationAPI,
		requestedWebsiteID = websiteID,
		generation = websiteLoadGeneration
	) {
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) {
			cronJobs = [];
			return;
		}
		loading = true;
		error = '';
		try {
			const nextCronJobs = (await api.get<CronJob[]>(scopedAPI.cronJobs)) || [];
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			cronJobs = nextCronJobs;
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				error = err instanceof Error ? err.message : 'Failed to load cron jobs';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) loading = false;
		}
	}

	async function createCronJob() {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation) || !createCommand.trim() || !createSchedule.trim()) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(scopedAPI.cronJobs, { command: createCommand.trim(), schedule: createSchedule.trim() });
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = 'Cron job created successfully.';
			showCreateForm = false;
			createCommand = '';
			createSchedule = '';
			await loadCronJobs(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to create cron job';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) creating = false;
		}
	}

	async function toggleEnabled(job: CronJob) {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/cron-jobs/${job.id}/${job.enabled ? 'disable' : 'enable'}`);
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			job.enabled = !job.enabled;
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to toggle cron job';
			}
		}
	}

	function startEdit(job: CronJob) {
		editingId = job.id;
		editCommand = job.command;
		editSchedule = job.schedule;
	}

	function cancelEdit() {
		editingId = null;
		editCommand = '';
		editSchedule = '';
	}

	async function saveEdit(job: CronJob) {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
		saving = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/cron-jobs/${job.id}`, { command: editCommand.trim(), schedule: editSchedule.trim() });
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = 'Cron job updated.';
			editingId = null;
			await loadCronJobs(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to update cron job';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) saving = false;
		}
	}

	async function deleteCronJob(id: string) {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/cron-jobs/${id}`);
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			actionMsg = 'Cron job deleted.';
			await loadCronJobs(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				actionError = err instanceof Error ? err.message : 'Failed to delete cron job';
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
					<h2 class="text-2xl font-bold text-white">Cron Jobs · {currentWebsite.domain}</h2>
					<span aria-label="Website status" class="inline-block px-2.5 py-0.5 rounded bg-gray-700 text-xs font-medium text-gray-300">{currentWebsite.status}</span>
				</div>
			</div>
			<button onclick={() => (showCreateForm = !showCreateForm)} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
				{showCreateForm ? 'Cancel' : 'Add Cron Job'}
			</button>
		</div>

		<WebsiteSectionNav websiteId={currentWebsite.id} currentPath={page.url.pathname} />

		{#if actionMsg}
			<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
				{actionMsg} <button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}
		{#if actionError}
			<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
				{actionError} <button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
			</div>
		{/if}

		{#if showCreateForm}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-4">New Cron Job</h3>
				<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
					<div>
						<label for="cron-command" class="block text-sm text-gray-400 mb-1">Command</label>
						<input id="cron-command" type="text" bind:value={createCommand} placeholder="php artisan schedule:run" class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
					</div>
					<div>
						<label for="cron-schedule" class="block text-sm text-gray-400 mb-1">Schedule</label>
						<input id="cron-schedule" type="text" bind:value={createSchedule} placeholder="* * * * *" class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
					</div>
				</div>
				<div class="mt-3 flex flex-wrap gap-2">
					<span class="text-xs text-gray-500 self-center">Presets:</span>
					{#each schedulePresets as preset}
						<button onclick={() => (createSchedule = preset.value)} class="px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 rounded transition-colors cursor-pointer">{preset.label}</button>
					{/each}
				</div>
				<div class="mt-4">
					<button onclick={createCronJob} disabled={creating || !createCommand.trim() || !createSchedule.trim()} class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer">{creating ? 'Adding...' : 'Add'}</button>
				</div>
			</div>
		{/if}

		{#if loading}
			<div class="text-gray-400">Loading cron jobs...</div>
		{:else if error}
			<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
		{:else if cronJobs.length === 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center"><p class="text-gray-400">No cron jobs configured yet.</p></div>
		{:else}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden"><div class="overflow-x-auto"><table class="w-full">
				<thead><tr class="border-b border-gray-700">
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Command</th>
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Schedule</th>
					<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Last Run</th>
					<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
					<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
				</tr></thead>
				<tbody class="divide-y divide-gray-700">
					{#each cronJobs as job}
						<tr class="hover:bg-gray-750">
							<td class="px-4 py-3 text-sm">
								{#if editingId === job.id}<input type="text" bind:value={editCommand} class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
								{:else}<code class="text-gray-200 bg-gray-900 px-1.5 py-0.5 rounded text-xs">{job.command}</code>{/if}
							</td>
							<td class="px-4 py-3 text-sm">
								{#if editingId === job.id}<input type="text" bind:value={editSchedule} class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
								{:else}<code class="text-gray-400 text-xs">{job.schedule}</code>{/if}
							</td>
							<td class="px-4 py-3 text-center"><button onclick={() => toggleEnabled(job)} class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none {job.enabled ? 'bg-blue-600' : 'bg-gray-600'}" role="switch" aria-checked={job.enabled} aria-label="Toggle cron job"><span class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 {job.enabled ? 'translate-x-4' : 'translate-x-0'}"></span></button></td>
							<td class="px-4 py-3 text-sm text-gray-400">{formatDate(job.last_run)}</td>
							<td class="px-4 py-3">{#if job.last_status}<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(job.last_status)}">{job.last_status}</span>{:else}<span class="text-xs text-gray-500">-</span>{/if}</td>
							<td class="px-4 py-3 text-right"><div class="flex items-center justify-end gap-2">
								{#if editingId === job.id}
									<button onclick={() => saveEdit(job)} disabled={saving} class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer">{saving ? 'Saving...' : 'Save'}</button>
									<button onclick={cancelEdit} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
								{:else if deleteConfirmId === job.id}
									<span class="text-xs text-red-400">Delete?</span><button onclick={() => deleteCronJob(job.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Yes</button><button onclick={() => (deleteConfirmId = null)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
								{:else}
									<button onclick={() => startEdit(job)} class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer">Edit</button><button onclick={() => (deleteConfirmId = job.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
								{/if}
							</div></td>
						</tr>
					{/each}
				</tbody>
			</table></div></div>
		{/if}
	{/if}
</div>
