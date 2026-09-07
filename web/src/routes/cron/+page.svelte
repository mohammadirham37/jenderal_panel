<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface CronJob {
		id: string;
		website_id: string;
		website_domain?: string;
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

	let cronJobs = $state<CronJob[]>([]);
	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let showCreateForm = $state(false);
	let createWebsiteId = $state('');
	let createCommand = $state('');
	let createSchedule = $state('');
	let creating = $state(false);

	// Edit state
	let editingId = $state<string | null>(null);
	let editCommand = $state('');
	let editSchedule = $state('');
	let saving = $state(false);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

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
			case 'success':
				return 'bg-green-900/50 text-green-400';
			case 'running':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '-';
		const d = new Date(dateStr);
		return d.toLocaleString('en-US', {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	function getWebsiteDomain(websiteId: string): string {
		const w = websites.find((ws) => ws.id === websiteId);
		return w ? w.domain : websiteId;
	}

	async function loadCronJobs() {
		loading = true;
		error = '';
		try {
			cronJobs = (await api.get<CronJob[]>('/api/v1/cron-jobs')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load cron jobs';
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

	async function createCronJob() {
		if (!createWebsiteId || !createCommand.trim() || !createSchedule.trim()) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/cron-jobs', {
				website_id: createWebsiteId,
				command: createCommand.trim(),
				schedule: createSchedule.trim()
			});
			actionMsg = 'Cron job created successfully.';
			showCreateForm = false;
			createWebsiteId = '';
			createCommand = '';
			createSchedule = '';
			await loadCronJobs();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create cron job';
		} finally {
			creating = false;
		}
	}

	async function toggleEnabled(job: CronJob) {
		actionMsg = '';
		actionError = '';
		try {
			if (job.enabled) {
				await api.post(`/api/v1/cron-jobs/${job.id}/disable`);
			} else {
				await api.post(`/api/v1/cron-jobs/${job.id}/enable`);
			}
			job.enabled = !job.enabled;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to toggle cron job';
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
		saving = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/cron-jobs/${job.id}`, {
				command: editCommand.trim(),
				schedule: editSchedule.trim()
			});
			actionMsg = 'Cron job updated.';
			editingId = null;
			await loadCronJobs();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update cron job';
		} finally {
			saving = false;
		}
	}

	async function deleteCronJob(id: string) {
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/cron-jobs/${id}`);
			actionMsg = 'Cron job deleted.';
			await loadCronJobs();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete cron job';
		}
	}

	function applyPreset(value: string) {
		createSchedule = value;
	}

	function openCreateForm() {
		showCreateForm = true;
		loadWebsites();
	}

	onMount(() => {
		loadCronJobs();
		loadWebsites();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Cron Jobs</h2>
		<button
			onclick={() => (showCreateForm ? (showCreateForm = false) : openCreateForm())}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showCreateForm ? 'Cancel' : 'Add Cron Job'}
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
			<h3 class="text-lg font-semibold text-white mb-4">New Cron Job</h3>
			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="cron-website" class="block text-sm text-gray-400 mb-1">Website</label>
					<select
						id="cron-website"
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
					<label for="cron-command" class="block text-sm text-gray-400 mb-1">Command</label>
					<input
						id="cron-command"
						type="text"
						bind:value={createCommand}
						placeholder="php artisan schedule:run"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="cron-schedule" class="block text-sm text-gray-400 mb-1">Schedule</label>
					<input
						id="cron-schedule"
						type="text"
						bind:value={createSchedule}
						placeholder="* * * * *"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			</div>
			<div class="mt-3 flex flex-wrap gap-2">
				<span class="text-xs text-gray-500 self-center">Presets:</span>
				{#each schedulePresets as preset}
					<button
						onclick={() => applyPreset(preset.value)}
						class="px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 rounded transition-colors cursor-pointer"
					>
						{preset.label}
					</button>
				{/each}
			</div>
			<div class="mt-4">
				<button
					onclick={createCronJob}
					disabled={creating || !createWebsiteId || !createCommand.trim() || !createSchedule.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Adding...' : 'Add'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Cron Jobs Table -->
	{#if loading}
		<div class="text-gray-400">Loading cron jobs...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if cronJobs.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No cron jobs configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Website</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Command</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Schedule</th>
							<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Last Run</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each cronJobs as job}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-gray-300">{job.website_domain || getWebsiteDomain(job.website_id)}</td>
								<td class="px-4 py-3 text-sm">
									{#if editingId === job.id}
										<input
											type="text"
											bind:value={editCommand}
											class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
										/>
									{:else}
										<code class="text-gray-200 bg-gray-900 px-1.5 py-0.5 rounded text-xs">{job.command}</code>
									{/if}
								</td>
								<td class="px-4 py-3 text-sm">
									{#if editingId === job.id}
										<input
											type="text"
											bind:value={editSchedule}
											class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
										/>
									{:else}
										<code class="text-gray-400 text-xs">{job.schedule}</code>
									{/if}
								</td>
								<td class="px-4 py-3 text-center">
									<button
										onclick={() => toggleEnabled(job)}
										class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none {job.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
										role="switch"
										aria-checked={job.enabled}
										aria-label="Toggle cron job"
									>
										<span
											class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 {job.enabled ? 'translate-x-4' : 'translate-x-0'}"
										></span>
									</button>
								</td>
								<td class="px-4 py-3 text-sm text-gray-400">{formatDate(job.last_run)}</td>
								<td class="px-4 py-3">
									{#if job.last_status}
										<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(job.last_status)}">
											{job.last_status}
										</span>
									{:else}
										<span class="text-xs text-gray-500">-</span>
									{/if}
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if editingId === job.id}
											<button
												onclick={() => saveEdit(job)}
												disabled={saving}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{saving ? 'Saving...' : 'Save'}
											</button>
											<button
												onclick={cancelEdit}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => startEdit(job)}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Edit
											</button>
											{#if deleteConfirmId === job.id}
												<span class="text-xs text-red-400">Delete?</span>
												<button
													onclick={() => deleteCronJob(job.id)}
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
													onclick={() => (deleteConfirmId = job.id)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Delete
												</button>
											{/if}
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
