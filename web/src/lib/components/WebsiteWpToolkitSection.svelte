<script lang="ts">
	import { onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';

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
			error = err instanceof Error ? err.message : 'Failed to load WordPress status';
		} finally {
			loading = false;
		}
	}

	async function runAction(action: string, label: string) {
		if (activeAction) return;
		activeAction = action;
		try {
			const res = await api.post<{ task_id: string }>(actionAPI(action), {});
			actionTaskId = res.task_id;
			toast.success(label + ' started.');
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `Failed to run ${label}`);
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
			{domain ? `WordPress toolkit for ${domain}` : 'WordPress toolkit'} · maintenance runs as background tasks
		</p>
		<button
			type="button"
			onclick={loadStatus}
			class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
		>
			Refresh
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
			<p class="text-sm text-gray-400">No WordPress installation detected.</p>
		</div>
	{:else if !status.installed}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-10 text-center">
			<p class="text-sm text-gray-400">WordPress is not installed on this site yet.</p>
			<p class="mt-1 text-xs text-gray-500">
				Create the site with the WordPress template (automatic install), or install WordPress manually.
			</p>
		</div>
	{:else}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<div class="flex flex-wrap items-center gap-3">
				<span class="rounded-md bg-blue-900/50 px-2.5 py-1 text-xs font-semibold text-blue-300">
					WordPress {status.core_version || '?'}
				</span>
				{#if status.core_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">core update available</span>
				{/if}
				{#if status.plugin_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">plugin updates</span>
				{/if}
				{#if status.theme_update}
					<span class="rounded-md bg-yellow-900/50 px-2 py-0.5 text-[11px] font-medium text-yellow-300">theme updates</span>
				{/if}
				{#if !hasUpdateBadge()}
					<span class="rounded-md bg-green-900/50 px-2 py-0.5 text-[11px] font-medium text-green-400">everything up to date</span>
				{/if}
			</div>
		</div>

		{#if actionTaskId}
			<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
				<p class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400">Task progress</p>
				<TaskProgress bind:taskId={actionTaskId} storageKey={'wp-task-' + websiteID} onComplete={loadStatus} />
			</div>
		{/if}

		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="mb-3 text-sm font-semibold text-white">Maintenance</h3>
			<div class="flex flex-wrap gap-2">
				<button
					type="button"
					onclick={() => runAction('core-update', 'Core update')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-blue-600 px-3 py-2 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
				>
					Update core
				</button>
				<button
					type="button"
					onclick={() => runAction('plugin-update', 'Plugin update')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					Update all plugins
				</button>
				<button
					type="button"
					onclick={() => runAction('theme-update', 'Theme update')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					Update all themes
				</button>
				<button
					type="button"
					onclick={() => runAction('core-update-db', 'Database update')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					Update database
				</button>
				<button
					type="button"
					onclick={() => runAction('cache-flush', 'Cache flush')}
					disabled={activeAction !== null}
					class="cursor-pointer rounded-lg bg-gray-700 px-3 py-2 text-xs font-semibold text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
				>
					Flush cache
				</button>
			</div>
			<p class="mt-3 text-[11px] text-gray-500">
				Actions run wp-cli as this site's system user. Major core upgrades are safer after a backup.
			</p>
		</div>
	{/if}
</div>
