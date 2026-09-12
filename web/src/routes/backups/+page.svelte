<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	// ── Types ──────────────────────────────────────────────────────
	interface Backup {
		id: string;
		type: string;
		target: string;
		storage: string;
		size_bytes: number;
		status: string;
		error_msg?: string;
		kind: string;
		created_by: string;
		remote_path?: string;
		task_id?: string;
		started_at?: string;
		finished_at?: string;
		created_at: string;
	}

	interface BackupSchedule {
		id: string;
		type: string;
		target: string;
		schedule: string;
		retention_days: number;
		retention_keep: number;
		enabled: boolean;
		last_run: string;
		last_run_status: string;
	}

	interface BackupStats {
		count: number;
		total_bytes: number;
		disk_free: number;
	}

	interface WebsiteLite {
		id: string;
		domain: string;
	}
	interface DatabaseLite {
		id: string;
		name: string;
		engine: string;
	}

	// ── State ──────────────────────────────────────────────────────
	let activeTab = $state<'backups' | 'schedules'>('backups');

	// Backups
	let backups = $state<Backup[]>([]);
	let loadingBackups = $state(true);
	let backupError = $state('');

	let stats = $state<BackupStats | null>(null);

	let filterType = $state('all');
	let filterStatus = $state('all');

	let createType = $state('website');
	let createTarget = $state('');
	let creatingBackup = $state(false);
	let createTaskId = $state('');

	let websites = $state<WebsiteLite[]>([]);
	let databases = $state<DatabaseLite[]>([]);

	let deleteConfirmId = $state<string | null>(null);
	let restoreConfirmId = $state<string | null>(null);
	let restoreComponent = $state('');
	let restoreTaskId = $state('');
	let restoring = $state(false);

	// Schedules
	let schedules = $state<BackupSchedule[]>([]);
	let loadingSchedules = $state(true);
	let scheduleError = $state('');

	let showScheduleForm = $state(false);
	let scheduleType = $state('website');
	let scheduleTarget = $state('');
	let scheduleCron = $state('0 0 * * *');
	let scheduleRetention = $state(30);
	let scheduleKeep = $state(0);
	let creatingSchedule = $state(false);

	let editingScheduleId = $state<string | null>(null);
	let editScheduleType = $state('');
	let editScheduleTarget = $state('');
	let editScheduleCron = $state('');
	let editScheduleRetention = $state(30);
	let editScheduleKeep = $state(0);
	let savingSchedule = $state(false);

	let deleteScheduleConfirmId = $state<string | null>(null);
	let pruneBusy = $state(false);

	const backupTypes = ['website', 'database', 'config', 'full'];
	const schedulePresets = [
		{ label: 'bk.presetHourly', value: '0 * * * *' },
		{ label: 'bk.presetDaily', value: '0 0 * * *' },
		{ label: 'bk.presetWeekly', value: '0 0 * * 0' },
		{ label: 'bk.presetMonthly', value: '0 0 1 * *' }
	];

	// ── Helpers ────────────────────────────────────────────────────
	function typeBadgeClass(type: string): string {
		switch (type) {
			case 'website': return 'bg-blue-900/50 text-blue-300';
			case 'database': return 'bg-green-900/50 text-green-300';
			case 'config': return 'bg-yellow-900/50 text-yellow-300';
			case 'full': return 'bg-purple-900/50 text-purple-300';
			default: return 'bg-gray-700 text-gray-300';
		}
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'completed': return 'bg-green-900/50 text-green-400';
			case 'running': return 'bg-blue-900/50 text-blue-300';
			case 'pending': return 'bg-yellow-900/50 text-yellow-300';
			case 'failed': return 'bg-red-900/50 text-red-400';
			default: return 'bg-gray-700 text-gray-300';
		}
	}

	function kindBadge(kind: string): string {
		if (kind === 'safety') return 'bg-teal-900/50 text-teal-300';
		if (kind === 'scheduled') return 'bg-indigo-900/50 text-indigo-300';
		return '';
	}

	function formatSize(bytes: number): string {
		if (!bytes || bytes <= 0) return '—';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '—';
		return new Date(dateStr).toLocaleString();
	}

	function needsTarget(type: string): boolean {
		return type === 'website' || type === 'database';
	}

	function safetyNote(b: Backup): string {
		if (b.type === 'website' || b.type === 'database' || b.type === 'full') {
			return ' ' + translate($language, 'bk.safetyNote');
		}
		return '';
	}

	function duration(b: Backup): string {
		if (!b.started_at || !b.finished_at) return '—';
		const ms = new Date(b.finished_at).getTime() - new Date(b.started_at).getTime();
		if (ms < 0) return '—';
		if (ms < 1000) return `${ms}ms`;
		const s = Math.round(ms / 1000);
		if (s < 60) return `${s}s`;
		return `${Math.floor(s / 60)}m ${s % 60}s`;
	}

	function nextRunEstimate(schedule: string, lastRun: string): string {
		const intervals: Record<string, number> = {
			'0 * * * *': 3600,
			'0 0 * * *': 86400,
			'0 0 * * 0': 604800,
			'0 0 1 * *': 2592000
		};
		const seconds = intervals[schedule.trim().toLowerCase()] ?? 86400;
		const base = lastRun ? new Date(lastRun).getTime() : Date.now();
		return new Date(base + seconds * 1000).toLocaleString();
	}

	function flash(msg: string, error = false) {
		if (error) { toast.error(msg); } else { toast.success(msg); }
	}

	let filteredBackups = $derived(
		backups.filter((b) =>
			(filterType === 'all' || b.type === filterType) &&
			(filterStatus === 'all' || b.status === filterStatus)
		)
	);

	let lastSuccessful = $derived.by(() => {
		const done = backups.filter((b) => b.status === 'completed');
		return done.length > 0 ? done[0].created_at : '';
	});

	// ── Data loading ───────────────────────────────────────────────
	async function loadAll() {
		await Promise.all([loadBackups(), loadSchedules(), loadStats(), loadPickers()]);
	}

	async function loadBackups() {
		try {
			backupError = '';
			backups = (await api.get<Backup[]>('/api/v1/backups')) || [];
		} catch (err) {
			backupError = err instanceof Error ? err.message : translate($language, 'bk.errorLoad');
		} finally {
			loadingBackups = false;
		}
	}

	async function loadStats() {
		try {
			stats = await api.get<BackupStats>('/api/v1/backups/stats');
		} catch {
			stats = null;
		}
	}

	async function loadSchedules() {
		try {
			scheduleError = '';
			schedules = (await api.get<BackupSchedule[]>('/api/v1/backup-schedules')) || [];
		} catch (err) {
			scheduleError = err instanceof Error ? err.message : translate($language, 'bk.errorLoadSchedules');
		} finally {
			loadingSchedules = false;
		}
	}

	async function loadPickers() {
		try {
			websites = ((await api.get<WebsiteLite[]>('/websites')) || []).map((w) => ({ id: w.id, domain: w.domain }));
		} catch { websites = []; }
		try {
			databases = ((await api.get<DatabaseLite[]>('/databases')) || []).filter((d) => d.engine !== 'redis');
		} catch { databases = []; }
	}

	// ── Actions ────────────────────────────────────────────────────
	async function createBackup() {
		if (creatingBackup) return;
		creatingBackup = true;
		try {
			const b = await api.post<{ task_id?: string }>('/api/v1/backups', {
				type: createType,
				target: needsTarget(createType) ? createTarget : ''
			});
			flash(translate($language, 'bk.toastQueued'));
			if (b?.task_id) createTaskId = b.task_id;
			await loadBackups();
			await loadStats();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorCreate'), true);
		} finally {
			creatingBackup = false;
		}
	}

	async function deleteBackup(id: string) {
		try {
			await api.del(`/api/v1/backups/${id}`);
			flash(translate($language, 'bk.toastDeleted'));
			await Promise.all([loadBackups(), loadStats()]);
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorDelete'), true);
		}
	}

	async function restoreBackup(id: string) {
		if (restoring) return;
		restoring = true;
		try {
			const res = await api.post<{ task_id?: string }>(`/api/v1/backups/${id}/restore`, {
				component: restoreComponent
			});
			flash(translate($language, 'bk.toastRestoreStarted'));
			if (res?.task_id) restoreTaskId = res.task_id;
			restoreConfirmId = null;
			restoreComponent = '';
			// The restore task updates the safety + target state; refresh now
			// (safety backup row) and again when the task completes.
			await loadBackups();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorRestore'), true);
		} finally {
			restoring = false;
		}
	}

	async function pruneNow() {
		if (pruneBusy) return;
		pruneBusy = true;
		try {
			const res = await api.post<{ pruned: number }>('/api/v1/backups/prune', {});
			flash(translate($language, 'bk.toastPruned').replace('{count}', String(res?.pruned ?? 0)));
			await Promise.all([loadBackups(), loadStats()]);
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorPrune'), true);
		} finally {
			pruneBusy = false;
		}
	}

	async function createScheduleEntry() {
		if (creatingSchedule) return;
		creatingSchedule = true;
		try {
			await api.post('/api/v1/backup-schedules', {
				type: scheduleType,
				target: needsTarget(scheduleType) ? scheduleTarget : '',
				schedule: scheduleCron,
				retention_days: scheduleRetention,
				retention_keep: scheduleKeep
			});
			flash(translate($language, 'bk.toastScheduleCreated'));
			showScheduleForm = false;
			await loadSchedules();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorScheduleCreate'), true);
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
		editScheduleKeep = s.retention_keep;
	}

	function cancelEditSchedule() {
		editingScheduleId = null;
	}

	async function saveScheduleEdit(id: string) {
		if (savingSchedule) return;
		savingSchedule = true;
		try {
			await api.put(`/api/v1/backup-schedules/${id}`, {
				type: editScheduleType,
				target: needsTarget(editScheduleType) ? editScheduleTarget : '',
				schedule: editScheduleCron,
				retention_days: editScheduleRetention,
				retention_keep: editScheduleKeep
			});
			editingScheduleId = null;
			await loadSchedules();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorScheduleUpdate'), true);
		} finally {
			savingSchedule = false;
		}
	}

	async function toggleSchedule(id: string, enabled: boolean) {
		try {
			await api.post(`/api/v1/backup-schedules/${id}/${enabled ? 'enable' : 'disable'}`, {});
			await loadSchedules();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorScheduleToggle'), true);
		}
	}

	async function deleteSchedule(id: string) {
		try {
			await api.del(`/api/v1/backup-schedules/${id}`);
			await loadSchedules();
		} catch (err) {
			flash(err instanceof Error ? err.message : translate($language, 'bk.errorScheduleDelete'), true);
		}
	}

	onMount(loadAll);
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<h2 class="text-2xl font-bold text-white">{translate($language, 'bk.title')}</h2>
		<div class="flex items-center gap-2">
			<button
				type="button"
				onclick={pruneNow}
				disabled={pruneBusy}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
			>
				{pruneBusy ? translate($language, 'bk.pruning') : translate($language, 'bk.pruneNow')}
			</button>
			<button
				type="button"
				onclick={() => (showScheduleForm = !showScheduleForm)}
				class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
			>
				{showScheduleForm ? translate($language, 'bk.closeScheduleForm') : translate($language, 'bk.newSchedule')}
			</button>
		</div>
	</div>


	<!-- Summary cards -->
	<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-500">{translate($language, 'bk.statCompleted')}</p>
			<p class="mt-1 text-xl font-bold text-white">{stats?.count ?? '—'}</p>
		</div>
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-500">{translate($language, 'bk.statTotalSize')}</p>
			<p class="mt-1 text-xl font-bold text-white">{formatSize(stats?.total_bytes ?? 0)}</p>
		</div>
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-500">{translate($language, 'bk.statDiskFree')}</p>
			<p class="mt-1 text-xl font-bold text-white">{formatSize(stats?.disk_free ?? 0)}</p>
		</div>
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-500">{translate($language, 'bk.statLastBackup')}</p>
			<p class="mt-1 truncate text-sm font-semibold text-white">{lastSuccessful ? formatDate(lastSuccessful) : '—'}</p>
		</div>
	</div>

	{#if createTaskId}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'bk.progressBackup')}</p>
			<TaskProgress bind:taskId={createTaskId} storageKey="backup-task" onComplete={loadBackups} />
		</div>
	{/if}

	{#if restoreTaskId}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'bk.progressRestore')}</p>
			<TaskProgress bind:taskId={restoreTaskId} storageKey="backup-restore-task" onComplete={loadBackups} />
		</div>
	{/if}

	{#if showScheduleForm}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-4 text-lg font-semibold text-white">{translate($language, 'bk.newScheduleTitle')}</h3>
			<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="sched-type">{translate($language, 'bk.labelType')}</label>
					<select id="sched-type" bind:value={scheduleType} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none">
						{#each backupTypes as t}<option value={t}>{t}</option>{/each}
					</select>
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="sched-target">{translate($language, 'bk.labelTarget')}</label>
					{#if needsTarget(scheduleType)}
						<select id="sched-target" bind:value={scheduleTarget} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none">
							<option value="">{translate($language, 'bk.selectPlaceholder')}</option>
							{#if scheduleType === 'website'}
								{#each websites as w (w.id)}<option value={w.domain}>{w.domain}</option>{/each}
							{:else}
								{#each databases as d (d.id)}<option value={d.name}>{d.name}</option>{/each}
							{/if}
						</select>
					{:else}
						<input disabled value={translate($language, 'bk.targetAll')} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-500" />
					{/if}
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="sched-cron">{translate($language, 'bk.labelSchedule')}</label>
					<select id="sched-cron" bind:value={scheduleCron} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none">
						{#each schedulePresets as p}<option value={p.value}>{translate($language, p.label)}</option>{/each}
					</select>
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="sched-days">{translate($language, 'bk.keepDays')}</label>
					<input id="sched-days" type="number" min="1" bind:value={scheduleRetention} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none" />
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="sched-keep">{translate($language, 'bk.orKeepLast')}</label>
					<input id="sched-keep" type="number" min="0" bind:value={scheduleKeep} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-[10px] text-gray-500">{translate($language, 'bk.zeroOff')}</p>
				</div>
			</div>
			<button
				type="button"
				onclick={createScheduleEntry}
				disabled={creatingSchedule || (needsTarget(scheduleType) && !scheduleTarget)}
				class="mt-4 cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
			>
				{creatingSchedule ? translate($language, 'bk.creating') : translate($language, 'bk.createSchedule')}
			</button>
		</div>
	{/if}

	<div class="flex gap-1">
		<button
			type="button"
			onclick={() => (activeTab = 'backups')}
			class="cursor-pointer rounded-t-lg px-4 py-2 text-sm font-semibold transition {activeTab === 'backups' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
		>{translate($language, 'bk.title')}</button>
		<button
			type="button"
			onclick={() => { activeTab = 'schedules'; loadSchedules(); }}
			class="cursor-pointer rounded-t-lg px-4 py-2 text-sm font-semibold transition {activeTab === 'schedules' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
		>{translate($language, 'bk.tabSchedules')}</button>
	</div>

	{#if activeTab === 'backups'}
		<div class="rounded-xl border border-gray-700 bg-gray-800">
			<!-- Filters -->
			<div class="flex flex-wrap items-center gap-2 border-b border-gray-700 px-4 py-3">
				<span class="text-[11px] font-semibold uppercase tracking-wider text-gray-500">{translate($language, 'bk.filterType')}</span>
				{#each ['all', ...backupTypes] as t}
					<button
						type="button"
						onclick={() => (filterType = t)}
						class="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition {filterType === t ? 'bg-blue-500/20 text-blue-200' : 'text-gray-400 hover:bg-gray-700'}"
					>{t}</button>
				{/each}
				<span class="ml-3 text-[11px] font-semibold uppercase tracking-wider text-gray-500">{translate($language, 'bk.filterStatus')}</span>
				{#each ['all', 'completed', 'running', 'pending', 'failed'] as s}
					<button
						type="button"
						onclick={() => (filterStatus = s)}
						class="rounded-full px-2.5 py-0.5 text-[11px] font-medium transition {filterStatus === s ? 'bg-blue-500/20 text-blue-200' : 'text-gray-400 hover:bg-gray-700'}"
					>{s}</button>
				{/each}
			</div>

			{#if loadingBackups}
				<div class="space-y-2 p-5">
					{#each Array(4) as _}
						<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
					{/each}
				</div>
			{:else if backupError}
				<div class="m-5 rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{backupError}</div>
			{:else if filteredBackups.length === 0}
				<div class="p-10 text-center">
					<p class="text-sm text-gray-400">{filterType !== 'all' || filterStatus !== 'all' ? translate($language, 'bk.emptyFiltered') : translate($language, 'bk.emptyNone')}</p>
					<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.emptyHint')}</p>
				</div>
			{:else}
				<div class="divide-y divide-gray-700/40">
					{#each filteredBackups as b (b.id)}
						<div class="flex flex-wrap items-center gap-3 px-5 py-3 transition hover:bg-gray-750">
							<span class="rounded-md px-2 py-0.5 text-[11px] font-semibold {typeBadgeClass(b.type)}">{b.type}</span>
							{#if b.kind && b.kind !== 'manual'}
								<span class="rounded-md px-1.5 py-0.5 text-[10px] font-semibold {kindBadge(b.kind)}">{b.kind}</span>
							{/if}
							{#if b.remote_path}
								<span class="rounded-md bg-teal-900/50 px-1.5 py-0.5 text-[10px] font-semibold text-teal-300" title={b.remote_path}>{translate($language, 'bk.offSite')}</span>
							{/if}
							<div class="min-w-0 flex-1">
								<p class="truncate font-mono text-sm text-gray-100">{b.target || translate($language, 'bk.targetAll')}</p>
								<p class="text-[11px] text-gray-500">
									{formatDate(b.created_at)}
									· {formatSize(b.size_bytes)}
									· {translate($language, 'bk.took').replace('{duration}', duration(b))}
								</p>
								{#if b.error_msg}
									<p class="truncate text-[11px] text-red-400" title={b.error_msg}>{b.error_msg}</p>
								{/if}
							</div>
							<span class="rounded-full px-2.5 py-0.5 text-[11px] font-medium {statusBadgeClass(b.status)}">{b.status}</span>
							<div class="flex shrink-0 items-center gap-1">
								{#if restoreConfirmId === b.id}
									<span class="mr-1 text-[11px] text-yellow-400">
										{translate($language, 'bk.restoreConfirm')}{safetyNote(b)}?
									</span>
									<button
										type="button"
										onclick={() => restoreBackup(b.id)}
										disabled={restoring}
										class="cursor-pointer rounded-lg bg-yellow-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-yellow-500 disabled:opacity-50"
									>
										{restoring ? '…' : translate($language, 'bk.yesRestore')}
									</button>
									<button
										type="button"
										onclick={() => { restoreConfirmId = null; restoreComponent = ''; }}
										class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600"
									>
										{translate($language, 'bk.no')}
									</button>
								{:else}
									{#if b.type === 'full'}
										<select
											bind:value={restoreComponent}
											class="rounded-lg border border-gray-600 bg-gray-900 px-1.5 py-1 text-[10px] text-gray-300"
											aria-label={translate($language, 'bk.restoreComponent')}
										>
											<option value="">{translate($language, 'bk.optAll')}</option>
											<option value="websites">{translate($language, 'bk.optWebsites')}</option>
											<option value="databases">{translate($language, 'bk.optDatabases')}</option>
											<option value="config">{translate($language, 'bk.optConfig')}</option>
										</select>
									{/if}
									<button
										type="button"
										onclick={() => { restoreConfirmId = b.id; }}
										disabled={b.status !== 'completed'}
										title={b.status !== 'completed' ? translate($language, 'bk.titleOnlyCompleted') : translate($language, 'bk.titleRestore')}
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-yellow-600 hover:text-white disabled:cursor-not-allowed disabled:opacity-30"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M9 15L3 9m0 0l6-6M3 9h12a6 6 0 010 12h-3" /></svg>
									</button>
									<a
										href={"/api/v1/backups/" + b.id + "/download"}
										download
										title={translate($language, 'bk.titleDownload')}
										class="rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-600 hover:text-white"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v2a2 2 0 002 2h12a2 2 0 002-2v-2M12 4v12m0 0l-4-4m4 4l4-4" /></svg>
									</a>
									{#if deleteConfirmId === b.id}
										<span class="text-[11px] text-red-400">{translate($language, 'bk.deleteQuestion')}</span>
										<button
											type="button"
											onclick={() => deleteBackup(b.id)}
											class="cursor-pointer rounded-lg bg-red-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-red-700"
										>
											{translate($language, 'bk.yes')}
										</button>
										<button
											type="button"
											onclick={() => (deleteConfirmId = null)}
											class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600"
										>
											{translate($language, 'bk.no')}
										</button>
									{:else}
										<button
											type="button"
											onclick={() => (deleteConfirmId = b.id)}
											title={translate($language, 'bk.titleDeleteBackup')}
											class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white"
										>
											<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
										</button>
									{/if}
								{/if}
								</div>
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{:else}
		<div class="rounded-xl border border-gray-700 bg-gray-800">
			{#if loadingSchedules}
				<div class="space-y-2 p-5">
					{#each Array(3) as _}
						<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
					{/each}
				</div>
			{:else if scheduleError}
				<div class="m-5 rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{scheduleError}</div>
			{:else if schedules.length === 0}
				<div class="p-10 text-center">
					<p class="text-sm text-gray-400">{translate($language, 'bk.emptyNoSchedules')}</p>
					<p class="mt-1 text-xs text-gray-500">{translate($language, 'bk.emptyScheduleHint')}</p>
				</div>
			{:else}
				<div class="divide-y divide-gray-700/40">
					{#each schedules as s (s.id)}
						<div class="flex flex-wrap items-center gap-3 px-5 py-3">
							{#if editingScheduleId === s.id}
								<div class="grid w-full gap-2 sm:grid-cols-6">
									<select bind:value={editScheduleType} class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200">
										{#each backupTypes as t}<option value={t}>{t}</option>{/each}
									</select>
									<select bind:value={editScheduleTarget} class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200">
										<option value="">{translate($language, 'bk.targetPlaceholder')}</option>
										{#if editScheduleType === 'website'}
											{#each websites as w (w.id)}<option value={w.domain}>{w.domain}</option>{/each}
										{:else}
											{#each databases as d (d.id)}<option value={d.name}>{d.name}</option>{/each}
										{/if}
									</select>
									<select bind:value={editScheduleCron} class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200">
										{#each schedulePresets as p}<option value={p.value}>{translate($language, p.label)}</option>{/each}
									</select>
									<input type="number" min="1" bind:value={editScheduleRetention} class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200" title={translate($language, 'bk.keepDays')} />
									<input type="number" min="0" bind:value={editScheduleKeep} class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200" title={translate($language, 'bk.keepLastN')} />
									<div class="flex gap-1">
										<button type="button" onclick={() => saveScheduleEdit(s.id)} disabled={savingSchedule} class="cursor-pointer rounded-md bg-blue-600 px-2.5 py-1.5 text-[11px] font-semibold text-white hover:bg-blue-700 disabled:opacity-50">{translate($language, 'bk.save')}</button>
										<button type="button" onclick={cancelEditSchedule} class="cursor-pointer rounded-md bg-gray-700 px-2.5 py-1.5 text-[11px] text-gray-200 hover:bg-gray-600">{translate($language, 'bk.cancel')}</button>
									</div>
								</div>
							{:else}
								<span class="rounded-md px-2 py-0.5 text-[11px] font-semibold {typeBadgeClass(s.type)}">{s.type}</span>
								<div class="min-w-0 flex-1">
									<p class="truncate font-mono text-sm text-gray-100">{s.target || translate($language, 'bk.targetAll')}</p>
									<p class="text-[11px] text-gray-500">
										{s.schedule} · {translate($language, 'bk.keepDaysInfo').replace('{days}', String(s.retention_days))}{s.retention_keep > 0 ? translate($language, 'bk.keepLastInfo').replace('{n}', String(s.retention_keep)) : ''}
										· {translate($language, 'bk.nextRun').replace('{time}', nextRunEstimate(s.schedule, s.last_run))}
									</p>
								</div>
								{#if s.last_run_status}
									<span class="rounded-full px-2 py-0.5 text-[10px] font-semibold {s.last_run_status === 'success' ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
										{translate($language, 'bk.lastRun')} {s.last_run_status}
									</span>
								{/if}
								<span class="rounded-full px-2 py-0.5 text-[10px] font-medium {s.enabled ? 'bg-green-900/50 text-green-400' : 'bg-gray-700 text-gray-400'}">
									{s.enabled ? translate($language, 'bk.enabled') : translate($language, 'bk.disabled')}
								</span>
								<div class="flex shrink-0 items-center gap-1">
									<button
										type="button"
										onclick={() => toggleSchedule(s.id, !s.enabled)}
										class="cursor-pointer rounded-lg px-2 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700"
									>
										{s.enabled ? translate($language, 'bk.disable') : translate($language, 'bk.enable')}
									</button>
									<button
										type="button"
										onclick={() => startEditSchedule(s)}
										class="cursor-pointer rounded-lg px-2 py-1 text-[11px] text-gray-300 transition hover:bg-gray-700"
									>
										{translate($language, 'bk.edit')}
									</button>
									{#if deleteScheduleConfirmId === s.id}
										<button
											type="button"
											onclick={() => deleteSchedule(s.id)}
											class="cursor-pointer rounded-lg bg-red-600 px-2 py-1 text-[11px] font-semibold text-white hover:bg-red-700"
										>
											{translate($language, 'bk.deleteQuestion')}
										</button>
										<button
											type="button"
											onclick={() => (deleteScheduleConfirmId = null)}
											class="cursor-pointer rounded-lg bg-gray-700 px-2 py-1 text-[11px] text-gray-200 hover:bg-gray-600"
										>
											{translate($language, 'bk.no')}
										</button>
									{:else}
										<button
											type="button"
											onclick={() => (deleteScheduleConfirmId = s.id)}
											class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white"
											title={translate($language, 'bk.titleDeleteSchedule')}
										>
											<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
										</button>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
