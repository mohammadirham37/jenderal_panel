<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface AppStatus {
		runtime: string;
		port: number;
		start_command: string;
		build_command: string;
		unit: string;
		active: boolean;
		node_version: string;
	}

	interface Props {
		websiteID: string;
		domain?: string;
		nodeVersion?: string;
		runtime?: string;
	}

	let { websiteID, domain = '', nodeVersion = '', runtime = 'node' }: Props = $props();

	let status = $state<AppStatus | null>(null);
	let loading = $state(false);
	let error = $state('');

	let startCommand = $state('');
	let buildCommand = $state('');
	let saving = $state(false);
	let busyAction = $state('');
	let buildTaskId = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	let refreshTimer: ReturnType<typeof setInterval> | null = null;

	function appAPI(action = ''): string {
		const base = `/api/v1/websites/${encodeURIComponent(websiteID)}/app`;
		return action ? `${base}/${action}` : base;
	}

	function statusBadgeClass(active: boolean): string {
		return active ? 'bg-green-900/50 text-green-400' : 'bg-gray-700 text-gray-400';
	}

	async function loadStatus() {
		if (!websiteID) return;
		loading = true;
		error = '';
		try {
			const s = await api.get<AppStatus>(appAPI());
			status = s;
			startCommand = s.start_command;
			buildCommand = s.build_command;
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wsapp.error.load');
		} finally {
			loading = false;
		}
	}

	async function saveConfig() {
		if (saving || !startCommand.trim()) return;
		saving = true;
		actionMsg = '';
		actionError = '';
		try {
			status = await api.put<AppStatus>(appAPI(), {
				start_command: startCommand.trim(),
				build_command: buildCommand.trim()
			});
			toast.success(translate($language, 'wsapp.toast.saved'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wsapp.error.save'));
		} finally {
			saving = false;
		}
	}

	async function runAction(action: 'start' | 'stop' | 'restart') {
		if (busyAction) return;
		busyAction = action;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(appAPI(action), {});
			toast.success(
				action === 'restart'
					? translate($language, 'wsapp.toast.restarted')
					: action === 'stop'
						? translate($language, 'wsapp.toast.stopped')
						: translate($language, 'wsapp.toast.started')
			);
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wsapp.error.action').replace('{action}', action));
		} finally {
			busyAction = '';
		}
	}

	async function runBuild() {
		if (busyAction) return;
		busyAction = 'build';
		actionMsg = '';
		actionError = '';
		try {
			const res = await api.post<{ task_id: string }>(appAPI('build'), {});
			buildTaskId = res.task_id;
			toast.success(translate($language, 'wsapp.toast.buildStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wsapp.error.build'));
		} finally {
			busyAction = '';
		}
	}

	$effect(() => {
		if (!websiteID) return;
		loadStatus();
		refreshTimer = setInterval(loadStatus, 20000);
		return () => {
			if (refreshTimer) clearInterval(refreshTimer);
		};
	});

	onDestroy(() => {
		if (refreshTimer) clearInterval(refreshTimer);
	});
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-gray-500">
			{domain
				? translate($language, 'wsapp.serviceFor').replace('{domain}', domain)
				: translate($language, 'wsapp.service')}
			{#if status?.port}· port {status.port}{/if}
			· Node {nodeVersion || translate($language, 'wsapp.default')}{status?.unit ? ` · ${status.unit}` : ''}
		</p>
		<button
			type="button"
			onclick={loadStatus}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
		>
			{translate($language, 'wsapp.refresh')}
		</button>
	</div>

	{#if actionError}
		<div class="rounded-lg border border-red-700 bg-red-900/30 px-4 py-2.5 text-sm text-red-300">{actionError}</div>
	{/if}

	{#if buildTaskId}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
			<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'wsapp.buildProgress')}</p>
			<TaskProgress bind:taskId={buildTaskId} storageKey={'app-build-' + websiteID} onComplete={loadStatus} />
		</div>
	{/if}

	{#if loading}
		<div class="space-y-2 rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#each Array(4) as _}
				<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
	{:else if status}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<h3 class="text-sm font-semibold text-white">{translate($language, 'wsapp.serviceHeading')}</h3>
				<span class="rounded-full px-2.5 py-0.5 text-[11px] font-medium {statusBadgeClass(status.active)}">
					{status.active ? translate($language, 'wsapp.running') : translate($language, 'wsapp.stopped')}
				</span>
			</div>
			<div class="mt-3 flex flex-wrap gap-2">
				{#if status.active}
					<button type="button" onclick={() => runAction('restart')} disabled={busyAction !== ''}
						class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50">{translate($language, 'wsapp.restart')}</button>
					<button type="button" onclick={() => runAction('stop')} disabled={busyAction !== ''}
						class="cursor-pointer rounded-lg bg-yellow-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-yellow-500 disabled:opacity-50">{translate($language, 'wsapp.stop')}</button>
				{:else}
					<button type="button" onclick={() => runAction('start')} disabled={busyAction !== ''}
						class="cursor-pointer rounded-lg bg-green-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-green-700 disabled:opacity-50">{translate($language, 'wsapp.start')}</button>
				{/if}
				{#if status.build_command}
					<button type="button" onclick={runBuild} disabled={busyAction !== ''}
						class="cursor-pointer rounded-lg bg-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50">{translate($language, 'wsapp.buildRestart')}</button>
				{/if}
			</div>
		</div>

		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-3 text-sm font-semibold text-white">{translate($language, 'wsapp.configuration')}</h3>
			<div class="grid gap-3">
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="app-start">{translate($language, 'wsapp.startCommand')}</label>
					<input id="app-start" type="text" bind:value={startCommand} placeholder="npm run start"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-[10px] text-gray-500">
				{#if runtime === 'python'}
					{translate($language, 'wsapp.hint.python')}
				{:else}
					{translate($language, 'wsapp.hint.node').replace('{version}', nodeVersion || translate($language, 'wsapp.default'))}
				{/if}
			</p>
				</div>
				<div>
					<label class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400" for="app-build">{translate($language, 'wsapp.buildCommand')}</label>
					<input id="app-build" type="text" bind:value={buildCommand} placeholder="npm run build"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none" />
				</div>
			</div>
			<button
				type="button"
				onclick={saveConfig}
				disabled={saving || !startCommand.trim()}
				class="mt-3 cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
			>
				{saving ? translate($language, 'wsapp.saving') : translate($language, 'wsapp.saveConfig')}
			</button>
		</div>
	{/if}
</div>
