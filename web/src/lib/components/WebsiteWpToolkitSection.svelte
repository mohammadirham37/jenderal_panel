<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	interface WpStatus {
		installed: boolean;
		core_version?: string;
		core_update: boolean;
		plugin_update: boolean;
		theme_update: boolean;
	}

	interface Props {
		websiteID: string;
		domain?: string;
	}

	let { websiteID, domain = '' }: Props = $props();

	let status = $state<WpStatus | null>(null);
	let loading = $state(false);
	let error = $state('');

	let activeAction = $state<string | null>(null);
	let actionTaskId = $state('');
	let refreshTimer: ReturnType<typeof setInterval> | null = null;

	function actionAPI(action: string): string {
		return `/api/v1/websites/${encodeURIComponent(websiteID)}/wp/${action}`;
	}

	async function loadStatus() {
		if (!websiteID) return;
		loading = true;
		error = '';
		try {
			status = await api.get<WpStatus>(`/api/v1/websites/${encodeURIComponent(websiteID)}/wp`);
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'wswp.error.load');
		} finally {
			loading = false;
		}
	}

	async function runAction(action: string, labelKey: string) {
		if (activeAction) return;
		activeAction = action;
		try {
			const res = await api.post<{ task_id: string }>(actionAPI(action), {});
			actionTaskId = res.task_id;
			toast.success(translate($language, 'wswp.toast.started').replace('{label}', translate($language, labelKey)));
		} catch (err) {
			toast.error(
				err instanceof Error
					? err.message
					: translate($language, 'wswp.error.action').replace('{label}', translate($language, labelKey))
			);
		} finally {
			activeAction = null;
		}
	}

	function hasUpdateBadge(): boolean {
		return !!status && (status.core_update || status.plugin_update || status.theme_update);
	}

	// Light polling keeps update availability fresh while mounted.
	$effect(() => {
		if (!websiteID) return;
		loadStatus();
		refreshTimer = setInterval(loadStatus, 30000);
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
				? translate($language, 'wswp.toolkitFor').replace('{domain}', domain)
				: translate($language, 'wswp.toolkit')}
			· {translate($language, 'wswp.subtitleNote')}
		</p>
		<button
			type="button"
			onclick={loadStatus}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
		>
			{translate($language, 'wswp.refresh')}
		</button>
	</div>


	{#if loading}
		<div class="space-y-2 rounded-xl border border-gray-700 bg-gray-800 p-5">
			{#each Array(3) as _}
				<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-3.5 text-sm text-red-300">{error}</div>
	{:else if !status}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-10 text-center">
			<p class="text-sm text-gray-400">{translate($language, 'wswp.empty.detected')}</p>
		</div>
	{:else if !status.installed}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-10 text-center">
			<p class="text-sm text-gray-400">{translate($language, 'wswp.empty.notInstalled')}</p>
			<p class="mt-1 text-xs text-gray-500">
				{translate($language, 'wswp.empty.hint')}
			</p>
		</div>
	{:else}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex flex-wrap items-center gap-3">
				<span class="rounded-md bg-blue-900/50 px-2.5 py-1 text-xs font-semibold text-blue-300">
					WordPress {status.core_version || '?'}
				</span>
				{#if status.core_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">{translate($language, 'wswp.badge.coreUpdate')}</span>
				{/if}
				{#if status.plugin_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">{translate($language, 'wswp.badge.pluginUpdates')}</span>
				{/if}
				{#if status.theme_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">{translate($language, 'wswp.badge.themeUpdates')}</span>
				{/if}
				{#if !hasUpdateBadge()}
					<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[11px] font-medium text-green-400">{translate($language, 'wswp.badge.upToDate')}</span>
				{/if}
			</div>
		</div>

		{#if actionTaskId}
			<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
				<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'wswp.taskProgress')}</p>
				<TaskProgress bind:taskId={actionTaskId} storageKey={'wp-task-' + websiteID} onComplete={loadStatus} />
			</div>
		{/if}

		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-3 text-sm font-semibold text-white">{translate($language, 'wswp.maintenance')}</h3>
			<div class="flex flex-wrap gap-2">
				<button
					type="button"
					onclick={() => runAction('core-update', 'wswp.action.coreUpdate')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
				>
					{translate($language, 'wswp.btn.updateCore')}
				</button>
				<button
					type="button"
					onclick={() => runAction('plugin-update', 'wswp.action.pluginUpdate')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					{translate($language, 'wswp.btn.updatePlugins')}
				</button>
				<button
					type="button"
					onclick={() => runAction('theme-update', 'wswp.action.themeUpdate')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					{translate($language, 'wswp.btn.updateThemes')}
				</button>
				<button
					type="button"
					onclick={() => runAction('core-update-db', 'wswp.action.dbUpdate')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					{translate($language, 'wswp.btn.updateDb')}
				</button>
				<button
					type="button"
					onclick={() => runAction('cache-flush', 'wswp.action.cacheFlush')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					{translate($language, 'wswp.btn.flushCache')}
				</button>
			</div>
			<p class="mt-3 text-[11px] text-gray-500">
				{translate($language, 'wswp.hint')}
			</p>
		</div>
	{/if}
</div>
