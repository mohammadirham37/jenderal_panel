<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	// Build & restart controls for Go sites on the Deployment tab: the
	// auto-build-on-deploy switch, the build/start commands, and a manual
	// Build & Restart that runs the same pipeline as a git deploy.
	interface AppStatus {
		runtime: string;
		port: number;
		start_command: string;
		build_command: string;
		active: boolean;
		auto_build: boolean;
	}

	interface Props {
		websiteID: string;
		canUpdate?: boolean;
		/** Mirrors the loaded switch so the page can label the repo card. */
		autoBuild?: boolean | null;
	}

	let { websiteID, canUpdate = true, autoBuild = $bindable(null) }: Props = $props();

	let status = $state<AppStatus | null>(null);
	let loading = $state(true);
	let error = $state('');
	let buildCommand = $state('');
	let startCommand = $state('');
	let saving = $state(false);
	let toggling = $state(false);
	let building = $state(false);
	let taskId = $state('');

	let isBinary = $derived(status?.runtime === 'go-binary');
	let dirty = $derived(!!status && (buildCommand.trim() !== status.build_command || startCommand.trim() !== status.start_command));
	let busy = $derived(saving || building || !!taskId);
	// The default build writes ./app; a repository with an app/ directory
	// turns that into a directory and the start command cannot exec it.
	let usesDefaultName = $derived(/-o\s+\.?\/?app(\s|$)/.test(buildCommand) && /^\.\/app(\s|$)/.test(startCommand.trim()));

	function appAPI(action = ''): string {
		const base = `/api/v1/websites/${encodeURIComponent(websiteID)}/app`;
		return action ? `${base}/${action}` : base;
	}

	async function load() {
		error = '';
		try {
			const s = await api.get<AppStatus>(appAPI());
			status = s;
			autoBuild = s.auto_build;
			if (!dirty) {
				buildCommand = s.build_command;
				startCommand = s.start_command;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wd.gobuild.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function toggleAutoBuild() {
		if (!status || toggling) return;
		const next = !status.auto_build;
		toggling = true;
		try {
			await api.put(appAPI('auto-build'), { auto_build: next });
			status = { ...status, auto_build: next };
			autoBuild = next;
			toast.success(translate($language, next ? 'wd.gobuild.autoOnToast' : 'wd.gobuild.autoOffToast'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.gobuild.saveFailed'));
		} finally {
			toggling = false;
		}
	}

	async function saveCommands() {
		if (!status || saving || !startCommand.trim()) return;
		saving = true;
		try {
			const s = await api.put<AppStatus>(appAPI(), {
				start_command: startCommand.trim(),
				build_command: buildCommand.trim()
			});
			status = s;
			buildCommand = s.build_command;
			startCommand = s.start_command;
			toast.success(translate($language, 'wd.gobuild.saved'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.gobuild.saveFailed'));
		} finally {
			saving = false;
		}
	}

	function useSafeName() {
		buildCommand = 'go build -o server .';
		startCommand = './server';
	}

	async function buildAndRestart() {
		if (busy || dirty) return;
		building = true;
		try {
			const res = await api.post<{ task_id: string }>(appAPI('build'), {});
			taskId = res.task_id;
			toast.success(translate($language, 'wd.gobuild.started'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'wd.gobuild.startFailed'));
		} finally {
			building = false;
		}
	}

	onMount(load);

	const inputClass =
		'w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40 disabled:opacity-60';
</script>

<section class="rounded-lg border border-gray-700 bg-gray-800 p-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h3 class="text-lg font-semibold text-white">{translate($language, 'wd.gobuild.title')}</h3>
			<p class="mt-0.5 text-sm text-gray-400">{translate($language, isBinary ? 'wd.gobuild.subtitleBinary' : 'wd.gobuild.subtitle')}</p>
		</div>
		{#if status}
			<span class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium {status.active ? 'bg-green-900/50 text-green-300' : 'bg-gray-700 text-gray-300'}">
				<span class="h-1.5 w-1.5 rounded-full {status.active ? 'bg-green-400' : 'bg-gray-500'}"></span>
				{status.active ? translate($language, 'wd.gobuild.running') : translate($language, 'wd.gobuild.stopped')}
				{#if status.port}<span class="font-mono text-gray-400">· :{status.port}</span>{/if}
			</span>
		{/if}
	</div>

	{#if loading}
		<div class="mt-4 h-24 animate-pulse rounded-lg bg-gray-700/40"></div>
	{:else if error}
		<div class="mt-4 flex items-center justify-between gap-2 rounded-lg border border-red-700 bg-red-900/30 px-3 py-2 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={load} class="cursor-pointer underline">{translate($language, 'wd.gobuild.retry')}</button>
		</div>
	{:else if status}
		<div class="mt-4 space-y-4">
			{#if !isBinary}
				<!-- Auto build switch -->
				<div class="flex items-start justify-between gap-4 rounded-lg border border-gray-700 bg-gray-900/40 px-4 py-3">
					<div>
						<p class="text-sm font-medium text-white">{translate($language, 'wd.gobuild.autoTitle')}</p>
						<p class="mt-0.5 text-xs text-gray-400">
							{translate($language, status.auto_build ? 'wd.gobuild.autoOnHint' : 'wd.gobuild.autoOffHint')}
						</p>
					</div>
					<button
						type="button"
						role="switch"
						aria-checked={status.auto_build}
						aria-label={translate($language, 'wd.gobuild.autoTitle')}
						onclick={toggleAutoBuild}
						disabled={!canUpdate || toggling}
						class="relative mt-0.5 inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition disabled:cursor-default disabled:opacity-50 {status.auto_build ? 'bg-green-600' : 'bg-gray-600'}"
					>
						<span class="inline-block h-5 w-5 rounded-full bg-white shadow transition {status.auto_build ? 'translate-x-5.5' : 'translate-x-0.5'}"></span>
					</button>
				</div>
			{/if}

			<!-- Commands -->
			<div class="grid gap-3 sm:grid-cols-2">
				{#if !isBinary}
					<label class="space-y-1 text-sm text-gray-300">
						<span class="text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wd.gobuild.buildCommand')}</span>
						<input bind:value={buildCommand} disabled={!canUpdate || busy} spellcheck="false" placeholder="go build -o server ." class={inputClass} />
					</label>
				{/if}
				<label class="space-y-1 text-sm text-gray-300 {isBinary ? 'sm:col-span-2' : ''}">
					<span class="text-xs font-medium uppercase tracking-wider text-gray-400">{translate($language, 'wd.gobuild.startCommand')}</span>
					<input bind:value={startCommand} disabled={!canUpdate || busy} spellcheck="false" placeholder="./server" class={inputClass} />
				</label>
			</div>
			<p class="text-xs text-gray-500">{translate($language, 'wd.gobuild.portHint').replace('{port}', String(status.port || '—'))}</p>

			{#if !isBinary && usesDefaultName}
				<div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-yellow-700/50 bg-yellow-900/20 px-3 py-2 text-xs text-yellow-200">
					<span>{translate($language, 'wd.gobuild.nameTip')}</span>
					{#if canUpdate}
						<button type="button" onclick={useSafeName} disabled={busy} class="cursor-pointer rounded-md border border-yellow-600/60 px-2.5 py-1 font-medium transition hover:bg-yellow-600/20 disabled:opacity-50">
							{translate($language, 'wd.gobuild.useSafeName')}
						</button>
					{/if}
				</div>
			{/if}

			{#if canUpdate}
				<div class="flex flex-wrap items-center justify-end gap-2 border-t border-gray-700 pt-4">
					{#if dirty}
						<span class="mr-auto text-xs text-yellow-300">{translate($language, 'wd.gobuild.unsaved')}</span>
						<button type="button" onclick={() => { buildCommand = status?.build_command ?? ''; startCommand = status?.start_command ?? ''; }} disabled={saving} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-gray-200 transition hover:bg-gray-600 disabled:opacity-50">
							{translate($language, 'wd.gobuild.discard')}
						</button>
						<button type="button" onclick={saveCommands} disabled={saving || !startCommand.trim()} class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50">
							{saving ? translate($language, 'wd.gobuild.saving') : translate($language, 'wd.gobuild.save')}
						</button>
					{:else}
						<button
							type="button"
							onclick={buildAndRestart}
							disabled={busy}
							class="inline-flex cursor-pointer items-center gap-2 rounded-lg bg-green-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-green-700 disabled:opacity-50"
						>
							<svg class="h-4 w-4 {building || taskId ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182m0-4.991v4.99" />
							</svg>
							{taskId ? translate($language, 'wd.gobuild.building') : translate($language, isBinary ? 'wd.gobuild.restartBinary' : 'wd.gobuild.buildRestart')}
						</button>
					{/if}
				</div>
			{/if}

			<TaskProgress
				bind:taskId
				storageKey={`jenderal_go_build_${websiteID}`}
				onComplete={(task) => {
					taskId = '';
					void load();
					if (task.status === 'completed') toast.success(translate($language, 'wd.gobuild.done'));
				}}
				onMissing={() => { taskId = ''; }}
			/>
		</div>
	{/if}
</section>
