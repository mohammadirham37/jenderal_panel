<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';

	// ── Types ──────────────────────────────────────────────────────
	interface Backup {
		id: string;
		type: string;
		target: string;
		storage: string;
		size: number;
		status: string;
		created_at: string;
	}

	interface BackupSchedule {
		id: string;
		type: string;
		target: string;
		schedule: string;
		retention_days: number;
		enabled: boolean;
		last_run: string;
	}

	// ── State ──────────────────────────────────────────────────────
	let activeTab = $state<'backups' | 'schedules'>('backups');

	// Backups
	let backups = $state<Backup[]>([]);
	let loadingBackups = $state(true);
	let backupError = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let createType = $state('website');
	let createTarget = $state('');
	let creatingBackup = $state(false);

	let deleteConfirmId = $state<string | null>(null);
	let restoreConfirmId = $state<string | null>(null);

	let pollTimer: ReturnType<typeof setInterval> | null = null;

	// Schedules
	let schedules = $state<BackupSchedule[]>([]);
	let loadingSchedules = $state(true);
	let scheduleError = $state('');

	let showScheduleForm = $state(false);
	let scheduleType = $state('website');
	let scheduleTarget = $state('');
	let scheduleCron = $state('0 0 * * *');
	let scheduleRetention = $state(30);
	let creatingSchedule = $state(false);

	let editingScheduleId = $state<string | null>(null);
	let editScheduleType = $state('');
	let editScheduleTarget = $state('');
	let editScheduleCron = $state('');
	let editScheduleRetention = $state(30);
	let savingSchedule = $state(false);

	let deleteScheduleConfirmId = $state<string | null>(null);

	const backupTypes = ['website', 'database', 'config', 'full'];

	const schedulePresets = [
		{ label: 'Daily', value: '0 0 * * *' },
		{ label: 'Weekly', value: '0 0 * * 0' },
		{ label: 'Monthly', value: '0 0 1 * *' }
	];

	// ── Helpers ────────────────────────────────────────────────────
	function typeBadgeClass(type: string): string {
		switch (type) {
			case 'website':
				return 'bg-blue-900/50 text-blue-400';
			case 'database':
				return 'bg-green-900/50 text-green-400';
			case 'config':
				return 'bg-purple-900/50 text-purple-400';
			case 'full':
				return 'bg-orange-900/50 text-orange-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'completed':
				return 'bg-green-900/50 text-green-400';
			case 'running':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			case 'pending':
				return 'bg-gray-700 text-gray-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function formatSize(bytes: number): string {
		if (!bytes || bytes === 0) return '0 B';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(1024));
		return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
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

	function needsTarget(type: string): boolean {
		return type === 'website' || type === 'database';
	}

	function hasActiveJobs(): boolean {
		return backups.some((b) => b.status === 'pending' || b.status === 'running');
	}

	// ── Polling ───────────────────────────────────────────────────
	function startPolling() {
		stopPolling();
		pollTimer = setInterval(async () => {
			if (hasActiveJobs()) {
				try {
					backups = (await api.get<Backup[]>('/api/v1/backups')) || [];
				} catch {
					// silent
				}
			} else {
				stopPolling();
			}
		}, 5000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	// ── Loaders ───────────────────────────────────────────────────
	async function loadBackups() {
		loadingBackups = true;
		backupError = '';
		try {
			backups = (await api.get<Backup[]>('/api/v1/backups')) || [];
			if (hasActiveJobs()) startPolling();
		} catch (err) {
			backupError = err instanceof Error ? err.message : 'Failed to load backups';
		} finally {
			loadingBackups = false;
		}
	}

	async function loadSchedules() {
		loadingSchedules = true;
		scheduleError = '';
		try {
			schedules = (await api.get<BackupSchedule[]>('/api/v1/backup-schedules')) || [];
		} catch (err) {
			scheduleError = err instanceof Error ? err.message : 'Failed to load backup schedules';
		} finally {
			loadingSchedules = false;
		}
	}

	// ── Backup Actions ────────────────────────────────────────────
	async function createBackup() {
		if (needsTarget(createType) && !createTarget.trim()) return;
		creatingBackup = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/backups', {
				type: createType,
				target: needsTarget(createType) ? createTarget.trim() : undefined
			});
			actionMsg = 'Backup created successfully.';
			createTarget = '';
			await loadBackups();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create backup';
		} finally {
			creatingBackup = false;
		}
	}

	async function deleteBackup(id: string) {
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/backups/${id}`);
			actionMsg = 'Backup deleted.';
			await loadBackups();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete backup';
		}
	}

	async function restoreBackup(id: string) {
		restoreConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/backups/${id}/restore`);
			actionMsg = 'Restore initiated. Check status for progress.';
			await loadBackups();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to restore backup';
		}
	}

	// ── Schedule Actions ──────────────────────────────────────────
	async function createScheduleEntry() {
		if (needsTarget(scheduleType) && !scheduleTarget.trim()) return;
		if (!scheduleCron.trim()) return;
		creatingSchedule = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/backup-schedules', {
				type: scheduleType,
				target: needsTarget(scheduleType) ? scheduleTarget.trim() : undefined,
				schedule: scheduleCron.trim(),
				retention_days: scheduleRetention
			});
			actionMsg = 'Backup schedule created.';
			showScheduleForm = false;
			scheduleTarget = '';
			scheduleCron = '0 0 * * *';
			scheduleRetention = 30;
			await loadSchedules();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create backup schedule';
		} finally {
			creatingSchedule = false;
		}
	}

	function startEditSchedule(s: BackupSchedule) {
		editingScheduleId = s.id;
		editScheduleType = s.type;
		editScheduleTarget = s.target;
		editScheduleCron = s.schedule;
		editScheduleRetention = s.retention_days;
	}

	function cancelEditSchedule() {
		editingScheduleId = null;
	}

	async function saveScheduleEdit(id: string) {
		savingSchedule = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/backup-schedules/${id}`, {
				type: editScheduleType,
				target: needsTarget(editScheduleType) ? editScheduleTarget.trim() : undefined,
				schedule: editScheduleCron.trim(),
				retention_days: editScheduleRetention
			});
			actionMsg = 'Schedule updated.';
			editingScheduleId = null;
			await loadSchedules();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update schedule';
		} finally {
			savingSchedule = false;
		}
	}

	async function deleteSchedule(id: string) {
		deleteScheduleConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/backup-schedules/${id}`);
			actionMsg = 'Schedule deleted.';
			await loadSchedules();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete schedule';
		}
	}

	async function toggleScheduleEnabled(s: BackupSchedule) {
		actionMsg = '';
		actionError = '';
		try {
			if (s.enabled) {
				await api.post(`/api/v1/backup-schedules/${s.id}/disable`);
			} else {
				await api.post(`/api/v1/backup-schedules/${s.id}/enable`);
			}
			s.enabled = !s.enabled;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to toggle schedule';
		}
	}

	// ── Lifecycle ─────────────────────────────────────────────────
	onMount(() => {
		loadBackups();
		loadSchedules();
	});

	onDestroy(() => {
		stopPolling();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Backups</h2>

	<!-- Feedback messages -->
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

	<!-- ═══════════════════════════ TABS ═══════════════════════════ -->
	<div class="border-b border-gray-700">
		<nav class="flex gap-0 -mb-px">
			{#each [
				{ key: 'backups', label: 'Backups' },
				{ key: 'schedules', label: 'Schedules' }
			] as tab}
				<button
					onclick={() => (activeTab = tab.key as typeof activeTab)}
					class="px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer
					{activeTab === tab.key
						? 'border-blue-500 text-blue-400'
						: 'border-transparent text-gray-400 hover:text-gray-200 hover:border-gray-600'}"
				>
					{tab.label}
				</button>
			{/each}
		</nav>
	</div>

	<!-- ═══════════════════════════ BACKUPS TAB ═══════════════════════════ -->
	{#if activeTab === 'backups'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Create Backup</h3>
			<div class="flex flex-wrap items-end gap-3">
				<div>
					<label for="backup-type" class="block text-sm text-gray-400 mb-1">Type</label>
					<select
						id="backup-type"
						bind:value={createType}
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
					>
						{#each backupTypes as t}
							<option value={t}>{t.charAt(0).toUpperCase() + t.slice(1)}</option>
						{/each}
					</select>
				</div>
				{#if needsTarget(createType)}
					<div>
						<label for="backup-target" class="block text-sm text-gray-400 mb-1">
							{createType === 'website' ? 'Domain' : 'Database Name'}
						</label>
						<input
							id="backup-target"
							type="text"
							bind:value={createTarget}
							placeholder={createType === 'website' ? 'example.com' : 'my_database'}
							class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-56"
						/>
					</div>
				{/if}
				<button
					onclick={createBackup}
					disabled={creatingBackup || (needsTarget(createType) && !createTarget.trim())}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creatingBackup ? 'Creating...' : 'Create Backup'}
				</button>
			</div>
		</div>

		<!-- Backups table -->
		{#if loadingBackups}
			<div class="text-gray-400 text-sm">Loading backups...</div>
		{:else if backupError}
			<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{backupError}</div>
		{:else if backups.length === 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
				<p class="text-gray-400">No backups found.</p>
			</div>
		{:else}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Type</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Target</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Storage</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Size</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Created</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each backups as backup}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3">
										<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {typeBadgeClass(backup.type)}">
											{backup.type}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-300">{backup.target || '-'}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{backup.storage || '-'}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{formatSize(backup.size)}</td>
									<td class="px-4 py-3">
										<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(backup.status)}">
											{backup.status}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-400">{formatDate(backup.created_at)}</td>
									<td class="px-4 py-3 text-right">
										{#if deleteConfirmId === backup.id}
											<span class="text-xs text-red-400 mr-1">Delete?</span>
											<button
												onclick={() => deleteBackup(backup.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												Cancel
											</button>
										{:else if restoreConfirmId === backup.id}
											<div class="inline-flex flex-col items-end gap-1">
												<span class="text-xs text-red-400 font-medium">Restore will overwrite current data!</span>
												<div class="flex gap-1">
													<button
														onclick={() => restoreBackup(backup.id)}
														class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
													>
														Confirm Restore
													</button>
													<button
														onclick={() => (restoreConfirmId = null)}
														class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
													>
														Cancel
													</button>
												</div>
											</div>
										{:else}
											<div class="flex items-center justify-end gap-1.5">
												{#if backup.status === 'completed'}
													<button
														onclick={() => (restoreConfirmId = backup.id)}
														class="px-2.5 py-1 bg-orange-600 hover:bg-orange-700 text-white text-xs rounded transition-colors cursor-pointer"
													>
														Restore
													</button>
												{/if}
												<button
													onclick={() => (deleteConfirmId = backup.id)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Delete
												</button>
											</div>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}
	{/if}

	<!-- ═══════════════════════════ SCHEDULES TAB ═══════════════════════════ -->
	{#if activeTab === 'schedules'}
		<div class="flex items-center justify-between">
			<div></div>
			<button
				onclick={() => (showScheduleForm = !showScheduleForm)}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
			>
				{showScheduleForm ? 'Cancel' : 'Add Schedule'}
			</button>
		</div>

		<!-- Create schedule form -->
		{#if showScheduleForm}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<h3 class="text-lg font-semibold text-white mb-4">New Backup Schedule</h3>
				<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
					<div>
						<label for="sched-type" class="block text-sm text-gray-400 mb-1">Type</label>
						<select
							id="sched-type"
							bind:value={scheduleType}
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						>
							{#each backupTypes as t}
								<option value={t}>{t.charAt(0).toUpperCase() + t.slice(1)}</option>
							{/each}
						</select>
					</div>
					{#if needsTarget(scheduleType)}
						<div>
							<label for="sched-target" class="block text-sm text-gray-400 mb-1">
								{scheduleType === 'website' ? 'Domain' : 'Database Name'}
							</label>
							<input
								id="sched-target"
								type="text"
								bind:value={scheduleTarget}
								placeholder={scheduleType === 'website' ? 'example.com' : 'my_database'}
								class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
							/>
						</div>
					{/if}
					<div>
						<label for="sched-cron" class="block text-sm text-gray-400 mb-1">Schedule</label>
						<input
							id="sched-cron"
							type="text"
							bind:value={scheduleCron}
							placeholder="0 0 * * *"
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
					<div>
						<label for="sched-retention" class="block text-sm text-gray-400 mb-1">Retention (days)</label>
						<input
							id="sched-retention"
							type="number"
							bind:value={scheduleRetention}
							min="1"
							max="365"
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						/>
					</div>
				</div>
				<div class="mt-3 flex flex-wrap gap-2">
					<span class="text-xs text-gray-500 self-center">Presets:</span>
					{#each schedulePresets as preset}
						<button
							onclick={() => (scheduleCron = preset.value)}
							class="px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 rounded transition-colors cursor-pointer"
						>
							{preset.label}
						</button>
					{/each}
				</div>
				<div class="mt-4">
					<button
						onclick={createScheduleEntry}
						disabled={creatingSchedule || !scheduleCron.trim() || (needsTarget(scheduleType) && !scheduleTarget.trim())}
						class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{creatingSchedule ? 'Adding...' : 'Add'}
					</button>
				</div>
			</div>
		{/if}

		<!-- Schedules table -->
		{#if loadingSchedules}
			<div class="text-gray-400 text-sm">Loading schedules...</div>
		{:else if scheduleError}
			<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{scheduleError}</div>
		{:else if schedules.length === 0}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
				<p class="text-gray-400">No backup schedules configured yet.</p>
			</div>
		{:else}
			<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Type</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Target</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Schedule</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Retention</th>
								<th class="text-center px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Last Run</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each schedules as sched}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3">
										{#if editingScheduleId === sched.id}
											<select
												bind:value={editScheduleType}
												class="px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-xs focus:outline-none focus:ring-2 focus:ring-blue-500 w-24"
											>
												{#each backupTypes as t}
													<option value={t}>{t.charAt(0).toUpperCase() + t.slice(1)}</option>
												{/each}
											</select>
										{:else}
											<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {typeBadgeClass(sched.type)}">
												{sched.type}
											</span>
										{/if}
									</td>
									<td class="px-4 py-3 text-sm">
										{#if editingScheduleId === sched.id}
											{#if needsTarget(editScheduleType)}
												<input
													type="text"
													bind:value={editScheduleTarget}
													class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
												/>
											{:else}
												<span class="text-gray-500 text-xs">N/A</span>
											{/if}
										{:else}
											<span class="text-gray-300">{sched.target || '-'}</span>
										{/if}
									</td>
									<td class="px-4 py-3 text-sm">
										{#if editingScheduleId === sched.id}
											<input
												type="text"
												bind:value={editScheduleCron}
												class="w-full px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
											/>
										{:else}
											<code class="text-gray-400 text-xs">{sched.schedule}</code>
										{/if}
									</td>
									<td class="px-4 py-3 text-sm">
										{#if editingScheduleId === sched.id}
											<input
												type="number"
												bind:value={editScheduleRetention}
												min="1"
												max="365"
												class="w-20 px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
											/>
										{:else}
											<span class="text-gray-400">{sched.retention_days} days</span>
										{/if}
									</td>
									<td class="px-4 py-3 text-center">
										<button
											onclick={() => toggleScheduleEnabled(sched)}
											class="relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none {sched.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
											role="switch"
											aria-checked={sched.enabled}
											aria-label="Toggle schedule"
										>
											<span
												class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 {sched.enabled ? 'translate-x-4' : 'translate-x-0'}"
											></span>
										</button>
									</td>
									<td class="px-4 py-3 text-sm text-gray-400">{formatDate(sched.last_run)}</td>
									<td class="px-4 py-3 text-right">
										<div class="flex items-center justify-end gap-2">
											{#if editingScheduleId === sched.id}
												<button
													onclick={() => saveScheduleEdit(sched.id)}
													disabled={savingSchedule}
													class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{savingSchedule ? 'Saving...' : 'Save'}
												</button>
												<button
													onclick={cancelEditSchedule}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Cancel
												</button>
											{:else}
												<button
													onclick={() => startEditSchedule(sched)}
													class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Edit
												</button>
												{#if deleteScheduleConfirmId === sched.id}
													<span class="text-xs text-red-400">Delete?</span>
													<button
														onclick={() => deleteSchedule(sched.id)}
														class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
													>
														Yes
													</button>
													<button
														onclick={() => (deleteScheduleConfirmId = null)}
														class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
													>
														Cancel
													</button>
												{:else}
													<button
														onclick={() => (deleteScheduleConfirmId = sched.id)}
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
	{/if}
</div>
