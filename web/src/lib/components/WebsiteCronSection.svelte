<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';

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

	interface Props {
		websiteID: string;
		domain?: string;
	}

	let { websiteID, domain = '' }: Props = $props();

	let cronJobs = $state<CronJob[]>([]);
	let loading = $state(false);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let showCreateForm = $state(false);
	let createCommand = $state('');
	let createSchedule = $state('* * * * *');
	let creating = $state(false);

	let editingId = $state<string | null>(null);
	let editCommand = $state('');
	let editSchedule = $state('');
	let saving = $state(false);
	let deleteConfirmId = $state<string | null>(null);

	const schedulePresets = [
		{ label: 'Every minute', value: '* * * * *' },
		{ label: 'Every 5 minutes', value: '*/5 * * * *' },
		{ label: 'Every 15 minutes', value: '*/15 * * * *' },
		{ label: 'Every hour', value: '0 * * * *' },
		{ label: 'Daily at midnight', value: '0 0 * * *' },
		{ label: 'Daily at 02:00', value: '0 2 * * *' },
		{ label: 'Weekly (Sunday 00:00)', value: '0 0 * * 0' },
		{ label: 'Monthly (1st, 00:00)', value: '0 0 1 * *' },
		{ label: 'Custom…', value: 'custom' }
	];

	let createSchedulePreset = $state('* * * * *');
	let createScheduleCustom = $state('');
	let editSchedulePreset = $state('custom');
	let editScheduleCustom = $state('');

	function describeSchedule(expr: string): string {
		const preset = schedulePresets.find((p) => p.value === expr && p.value !== 'custom');
		return preset ? preset.label : expr;
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'success': return 'bg-green-900/50 text-green-400';
			case 'running': return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'failed': return 'bg-red-900/50 text-red-400';
			default: return 'bg-gray-700 text-gray-400';
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return 'never';
		return new Date(dateStr).toLocaleString(undefined, {
			month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
		});
	}

	function jobsBase(): string {
		return `/api/v1/websites/${encodeURIComponent(websiteID)}/cron-jobs`;
	}

	async function loadCronJobs() {
		if (!websiteID) return;
		loading = true;
		error = '';
		try {
			cronJobs = (await api.get<CronJob[]>(jobsBase())) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load cron jobs';
		} finally {
			loading = false;
		}
	}

	async function createCronJob() {
		const schedule = createSchedulePreset === 'custom' ? createScheduleCustom.trim() : createSchedulePreset;
		if (creating || !createCommand.trim() || !schedule) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(jobsBase(), { command: createCommand.trim(), schedule });
			actionMsg = 'Cron job created.';
			showCreateForm = false;
			createCommand = '';
			createSchedulePreset = '* * * * *';
			createScheduleCustom = '';
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
			await api.post(`${jobsBase()}/${job.id}/${job.enabled ? 'disable' : 'enable'}`, {});
			job.enabled = !job.enabled;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to toggle cron job';
		}
	}

	function startEdit(job: CronJob) {
		editingId = job.id;
		editCommand = job.command;
		editSchedule = job.schedule;
		const preset = schedulePresets.find((p) => p.value === job.schedule && p.value !== 'custom');
		editSchedulePreset = preset ? preset.value : 'custom';
		editScheduleCustom = preset ? '' : job.schedule;
	}

	function cancelEdit() {
		editingId = null;
		editCommand = '';
		editSchedule = '';
	}

	async function saveEdit(job: CronJob) {
		const schedule = editSchedulePreset === 'custom' ? editScheduleCustom.trim() : editSchedulePreset;
		if (saving || !editCommand.trim() || !schedule) return;
		saving = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`${jobsBase()}/${job.id}`, { command: editCommand.trim(), schedule });
			actionMsg = 'Cron job updated.';
			editingId = null;
			await loadCronJobs();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update cron job';
		} finally {
			saving = false;
		}
	}

	async function deleteCronJob(job: CronJob) {
		if (deleteConfirmId !== job.id) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`${jobsBase()}/${job.id}`);
			actionMsg = 'Cron job deleted.';
			deleteConfirmId = null;
			await loadCronJobs();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete cron job';
		}
	}

	$effect(() => {
		if (websiteID) loadCronJobs();
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-gray-500">
			{domain ? `Scheduled jobs for ${domain}` : 'Scheduled jobs'} · written to the site's crontab
		</p>
		<div class="flex items-center gap-2">
			<button
				type="button"
				onclick={loadCronJobs}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
			>
				Refresh
			</button>
			<button
				type="button"
				onclick={() => (showCreateForm = !showCreateForm)}
				class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700"
			>
				{showCreateForm ? 'Close' : 'New cron job'}
			</button>
		</div>
	</div>

	{#if actionMsg || actionError}
		<div class="rounded-lg px-4 py-2.5 text-sm {actionError ? 'bg-red-900/50 border border-red-700 text-red-300' : 'bg-green-900/40 border border-green-700 text-green-300'}">
			{actionError || actionMsg}
		</div>
	{/if}

	{#if showCreateForm}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-4 text-sm font-semibold text-white">New cron job</h3>
			<div class="grid gap-3 lg:grid-cols-2">
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="cron-command">Command</label>
					<input
						id="cron-command"
						type="text"
						bind:value={createCommand}
						placeholder="php artisan schedule:run"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
					/>
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="cron-schedule">Schedule</label>
					<select
						id="cron-schedule"
						bind:value={createSchedulePreset}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
					>
						{#each schedulePresets as p}<option value={p.value}>{p.label}</option>{/each}
					</select>
					{#if createSchedulePreset === 'custom'}
						<input
							type="text"
							bind:value={createScheduleCustom}
							placeholder="*/10 3-5 * * *"
							class="mt-2 w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
						/>
					{/if}
				</div>
			</div>
			<button
				type="button"
				onclick={createCronJob}
				disabled={creating || !createCommand.trim() || (createSchedulePreset === 'custom' && !createScheduleCustom.trim())}
				class="mt-4 cursor-pointer rounded-lg bg-green-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-green-700 disabled:cursor-not-allowed disabled:opacity-40"
			>
				{creating ? 'Creating…' : 'Create cron job'}
			</button>
		</div>
	{/if}

	<div class="rounded-xl border border-gray-700 bg-gray-800">
		{#if loading}
			<div class="space-y-2 p-5">
				{#each Array(3) as _}
					<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
				{/each}
			</div>
		{:else if error}
			<div class="m-5 rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
		{:else if cronJobs.length === 0}
			<div class="p-10 text-center">
				<p class="text-sm text-gray-400">No cron jobs yet.</p>
				<p class="mt-1 text-xs text-gray-500">Create one to run commands on a schedule.</p>
			</div>
		{:else}
			<div class="divide-y divide-gray-700/40">
				{#each cronJobs as job (job.id)}
					<div class="px-5 py-3">
						{#if editingId === job.id}
							<div class="grid gap-2 lg:grid-cols-2">
								<input
									type="text"
									bind:value={editCommand}
									class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
								/>
								<div class="flex gap-2">
									<select bind:value={editSchedulePreset} class="flex-1 rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-xs text-gray-200">
										{#each schedulePresets as p}<option value={p.value}>{p.label}</option>{/each}
									</select>
									{#if editSchedulePreset === 'custom'}
										<input type="text" bind:value={editScheduleCustom} placeholder="cron expression"
											class="flex-1 rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200" />
									{/if}
								</div>
							</div>
							<div class="mt-2 flex gap-1.5">
								<button type="button" onclick={() => saveEdit(job)} disabled={saving}
									class="cursor-pointer rounded-md bg-blue-600 px-3 py-1.5 text-[11px] font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50">
									{saving ? 'Saving…' : 'Save'}
								</button>
								<button type="button" onclick={cancelEdit}
									class="cursor-pointer rounded-md bg-gray-700 px-3 py-1.5 text-[11px] text-gray-200 transition hover:bg-gray-600">
									Cancel
								</button>
							</div>
						{:else}
							<div class="flex flex-wrap items-center gap-3">
								<button
									type="button"
									onclick={() => toggleEnabled(job)}
									title={job.enabled ? 'Click to disable' : 'Click to enable'}
									class="cursor-pointer rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide {job.enabled ? 'bg-green-900/50 text-green-400' : 'bg-gray-700 text-gray-400'}"
								>
									{job.enabled ? 'enabled' : 'disabled'}
								</button>
								<div class="min-w-0 flex-1">
									<p class="truncate font-mono text-sm text-gray-100">{job.command}</p>
									<p class="text-[11px] text-gray-500">
										<span class="font-mono text-gray-400">{job.schedule}</span> · {describeSchedule(job.schedule)}
										· last run {formatDate(job.last_run)}
									</p>
								</div>
								{#if job.last_status}
									<span class="rounded-full px-2.5 py-0.5 text-[11px] font-medium {statusBadgeClass(job.last_status)}">{job.last_status}</span>
								{/if}
								<div class="flex shrink-0 items-center gap-1">
									<button type="button" onclick={() => startEdit(job)}
										class="cursor-pointer rounded-lg px-2.5 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700">Edit</button>
									{#if deleteConfirmId === job.id}
										<span class="text-[11px] text-red-400">Delete?</span>
										<button type="button" onclick={() => deleteCronJob(job)}
											class="cursor-pointer rounded-lg bg-red-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-red-700">Yes</button>
										<button type="button" onclick={() => (deleteConfirmId = null)}
											class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600">No</button>
									{:else}
										<button type="button" onclick={() => (deleteConfirmId = job.id)} title="Delete cron job"
											class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white">
											<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
										</button>
									{/if}
								</div>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>
