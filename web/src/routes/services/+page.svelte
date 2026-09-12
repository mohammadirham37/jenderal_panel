<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { composerActionPath } from '$lib/developer-dependencies.js';
	import type { ServiceStatus } from '$lib/types';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface DependencyStatus {
		name: string;
		version: string;
		installed: boolean;
	}

	let services = $state<ServiceStatus[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionInProgress = $state<string | null>(null);
	let dependencies = $state<DependencyStatus[]>([]);
	let dependenciesLoading = $state(true);
	let dependencyError = $state('');
	let currentTaskId = $state('');
	let composer = $derived(dependencies.find((item) => item.name === 'composer'));
	let composerInProgress = $derived(!!currentTaskId);

	async function loadServices() {
		try {
			services = (await api.get<ServiceStatus[]>('/api/v1/services')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'svc.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function loadDependencies() {
		dependenciesLoading = true;
		dependencyError = '';
		try {
			dependencies = (await api.get<DependencyStatus[]>('/api/v1/services/dependencies')) || [];
		} catch (err) {
			dependencyError = err instanceof Error ? err.message : translate($language, 'svc.deps.loadFailed');
		} finally {
			dependenciesLoading = false;
		}
	}

	async function manageComposer() {
		if (!composer || composerInProgress) return;
		try {
			const result = await api.post<{ task_id: string }>(composerActionPath(composer));
			currentTaskId = result.task_id;
			toast.success(
				composer.installed
				? translate($language, 'svc.toast.composerUpdateStarted')
				: translate($language, 'svc.toast.composerInstallStarted')
			);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'svc.toast.composerStartFailed'));
		}
	}

	function onComposerComplete(task: { status?: string; error?: string }) {
		if (task?.status === 'completed') {
			window.location.reload();
		} else {
			toast.error(task?.error || translate($language, 'svc.toast.composerFailed'));
		}
	}

	async function serviceAction(name: string, action: 'start' | 'stop' | 'restart') {
		actionInProgress = `${name}-${action}`;

		try {
			await api.post(`/api/v1/services/${encodeURIComponent(name)}/${action}`);
			toast.success(translate($language, `svc.toast.${action}`).replace('{name}', name));
			// Reload services list
			loading = true;
			await loadServices();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, `svc.toast.${action}Failed`).replace('{name}', name));
		} finally {
			actionInProgress = null;
		}
	}

	onMount(() => {
		void Promise.all([loadServices(), loadDependencies()]);
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'svc.title')}</h2>



	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_composer_task" onComplete={onComposerComplete} />

	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
			<div>
				<h3 class="text-lg font-semibold text-white">{translate($language, 'svc.deps.title')}</h3>
				<p class="mt-1 text-sm text-gray-400">{translate($language, 'svc.deps.hint')}</p>
			</div>
			<a href="/nodejs" class="text-sm text-blue-400 hover:text-blue-300">{translate($language, 'svc.deps.manageNode')}</a>
		</div>

		{#if dependenciesLoading}
			<p class="mt-4 text-sm text-gray-400">{translate($language, 'svc.deps.checking')}</p>
		{:else if dependencyError}
			<p class="mt-4 text-sm text-red-400">{dependencyError}</p>
		{:else if composer}
			<div class="mt-4 flex flex-col gap-3 rounded-lg border border-gray-700 bg-gray-900/60 p-4 sm:flex-row sm:items-center sm:justify-between">
				<div>
					<div class="flex items-center gap-2">
						<span class="font-medium text-white">Composer</span>
						<span class="rounded-full px-2 py-0.5 text-xs font-medium {composer.installed ? 'bg-green-900/50 text-green-400' : 'bg-gray-700 text-gray-400'}">
							{composer.installed ? translate($language, 'svc.installed') : translate($language, 'svc.notInstalled')}
						</span>
					</div>
					<p class="mt-1 text-sm text-gray-400">{translate($language, 'svc.deps.version').replace('{version}', composer.version || '-')}</p>
				</div>
				<button
					onclick={manageComposer}
					disabled={composerInProgress}
					class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
				>
					{composerInProgress ? translate($language, 'svc.deps.processing') : composer.installed ? translate($language, 'svc.deps.update') : translate($language, 'svc.deps.install')}
				</button>
			</div>
		{/if}
	</div>

	{#if loading}
		<div class="text-gray-400">{translate($language, 'svc.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if services.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
			{translate($language, 'svc.empty')}
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<table class="w-full">
				<thead>
					<tr class="border-b border-gray-700 bg-gray-800/80">
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>{translate($language, 'svc.table.service')}</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>{translate($language, 'svc.table.status')}</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>{translate($language, 'svc.table.enabled')}</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>PID</th
						>
						<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>{translate($language, 'svc.table.actions')}</th
						>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-700">
					{#each services as svc}
						<tr class="hover:bg-gray-750">
							<td class="px-4 py-3 text-sm text-white font-medium">{svc.name}</td>
							<td class="px-4 py-3">
									{#if !svc.installed}
										<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-700 text-gray-400">
											<span class="w-1.5 h-1.5 rounded-full bg-gray-500"></span>
											{translate($language, 'svc.notInstalled')}
										</span>
									{:else if svc.running}
										<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-900/50 text-green-400">
											<span class="w-1.5 h-1.5 rounded-full bg-green-400"></span>
											{translate($language, 'svc.running')}
										</span>
									{:else}
										<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-900/50 text-red-400">
											<span class="w-1.5 h-1.5 rounded-full bg-red-400"></span>
											{translate($language, 'svc.stopped')}
										</span>
									{/if}
							</td>
							<td class="px-4 py-3 text-sm text-gray-400">
								{svc.installed ? (svc.enabled ? translate($language, 'svc.yes') : translate($language, 'svc.no')) : '-'}
							</td>
							<td class="px-4 py-3 text-sm text-gray-400 font-mono">
								{svc.pid || '-'}
							</td>
							<td class="px-4 py-3 text-right">
								{#if svc.installed}
									<div class="flex items-center justify-end gap-2">
										{#if !svc.running}
											<button
												onclick={() => serviceAction(svc.name, 'start')}
												disabled={actionInProgress !== null}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{actionInProgress === `${svc.name}-start` ? '...' : translate($language, 'svc.action.start')}
											</button>
										{:else}
											<button
												onclick={() => serviceAction(svc.name, 'stop')}
												disabled={actionInProgress !== null}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{actionInProgress === `${svc.name}-stop` ? '...' : translate($language, 'svc.action.stop')}
											</button>
										{/if}
										<button
											onclick={() => serviceAction(svc.name, 'restart')}
											disabled={actionInProgress !== null}
											class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
										>
											{actionInProgress === `${svc.name}-restart` ? '...' : translate($language, 'svc.action.restart')}
										</button>
									</div>
								{:else}
									<span class="text-xs text-gray-500">{translate($language, 'svc.installViaDatabases')}</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
