<script lang="ts">
	import { onMount } from 'svelte';
	import { api, apiRaw } from '#lib/api.js';
	import TaskProgress from '#lib/components/TaskProgress.svelte';
import { toast } from '#lib/stores/toast.js';
	import { language, translate } from '#lib/stores/language.js';
	import { hasPermission, permissions } from '#lib/stores/auth.js';
	import { nodeRuntimeState, summarizeNodeRuntimes } from '#lib/node-runtimes.js';

	interface Runtime {
		error_message?: string;
		website_id: string; domain: string; web_user: string; selected_version: string;
		installed: boolean; installed_version: string; npm_version: string; nvm_version: string; nvm_state: string;
		security_baseline?: string; vulnerable?: boolean; end_of_life?: boolean; advisory_url?: string;
	}
	interface NodeApp {
		id: string; website_id: string; node_version: string; package_mgr: string;
		build_cmd: string; start_cmd: string; port: number; status: string;
	}
	let runtimes = $state<Runtime[]>([]);
	let apps = $state<NodeApp[]>([]);
	let globalNode = $state({installed: false, version: '', package: ''});
	let loading = $state(true);
	let error = $state('');
	let currentTaskId = $state('');
	let acting = $state(false);
	let busy = $derived(acting || !!currentTaskId);
	let confirmGlobal = $state(false);
	let deleteConfirmId = $state('');
	let showCreateForm = $state(false);
	let createWebsiteId = $state('');
	let createPackageMgr = $state('npm');
	let createBuildCmd = $state('');
	let createStartCmd = $state('server.js');
	let createPort = $state(3000);
	let availableWebsites = $derived(runtimes.filter((runtime) => runtime.installed));
	let selectedRuntime = $derived(runtimes.find((runtime) => runtime.website_id === createWebsiteId));
	let runtimeSummary = $derived(summarizeNodeRuntimes(runtimes));
	let canManage = $derived(hasPermission($permissions, 'nodejs.manage'));
	let updateAllConfirm = $state(false);
	let pruneConfirm = $state(false);

	async function updateRuntime(websiteID: string) {
		if (busy) return;
		acting = true;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/nodejs/runtimes/${encodeURIComponent(websiteID)}/update`);
			currentTaskId = result.task_id;
			toast.success(translate($language, 'nd.update.started'));
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.update.failed')); }
		finally { acting = false; }
	}

	async function updateAll() {
		if (busy) return;
		acting = true; updateAllConfirm = false;
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/nodejs/runtimes/update');
			currentTaskId = result.task_id;
			toast.success(translate($language, 'nd.update.started'));
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.update.failed')); }
		finally { acting = false; }
	}

	async function pruneAll() {
		if (busy) return;
		acting = true; pruneConfirm = false;
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/nodejs/runtimes/prune');
			currentTaskId = result.task_id;
			toast.success(translate($language, 'nd.prune.started'));
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.prune.failed')); }
		finally { acting = false; }
	}

	async function load() {
		loading = true; error = '';
		try {
			const [runtimeData, appData, globalData] = await Promise.all([
				api.get<Runtime[]>('/api/v1/nodejs/runtimes'),
				api.get<NodeApp[]>('/api/v1/nodejs/apps'),
				api.get<typeof globalNode>('/api/v1/nodejs/global')
			]);
			runtimes = runtimeData || []; apps = appData || []; globalNode = globalData;
		} catch (err) { error = err instanceof Error ? err.message : translate($language, 'nd.loadFailed'); }
		finally { loading = false; }
	}

	async function removeGlobal() {
		if (busy || !confirmGlobal) return;
		acting = true; confirmGlobal = false;
		try {
			const result = await apiRaw<{task_id: string}>('DELETE', '/api/v1/nodejs/global', {confirm: true});
			currentTaskId = result.data.task_id;
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.toast.removeGlobalFailed')); }
		finally { acting = false; }
	}

	async function createApp() {
		if (busy || !selectedRuntime?.installed || !createStartCmd.trim()) return;
		acting = true; 
		try {
			await api.post('/api/v1/nodejs/apps', {
				website_id: createWebsiteId, package_mgr: createPackageMgr,
				build_cmd: createBuildCmd.trim(), start_cmd: createStartCmd.trim(), port: createPort
			});
			showCreateForm = false; createWebsiteId = ''; toast.success(translate($language, 'nd.toast.created'));
			await load();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.toast.createFailed')); }
		finally { acting = false; }
	}

	async function appAction(id: string, action: string) {
		if (busy) return;
		acting = true; deleteConfirmId = '';
		try {
			if (action === 'delete') await api.del('/api/v1/nodejs/apps/' + id);
			else await api.post('/api/v1/nodejs/apps/' + id + '/' + action);
			toast.success(translate($language, 'nd.toast.updated')); await load();
		} catch (err) { toast.error(err instanceof Error ? err.message : translate($language, 'nd.toast.actionFailed')); }
		finally { acting = false; }
	}
	onMount(() => { void load(); });
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'nd.title')}</h2>
	<p class="text-sm text-gray-400">{translate($language, 'nd.hint')}</p>
	{#if error}<div role="alert" class="rounded border border-red-800 p-4 text-red-400">{error} <button onclick={load} disabled={busy} class="underline">{translate($language, 'nd.retry')}</button></div>{/if}
	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_nodejs_task" onComplete={() => { currentTaskId = ''; void load(); }} />

	{#if runtimeSummary.vulnerable.length > 0 || runtimeSummary.endOfLife.length > 0}
		<section role="alert" class="rounded-xl border border-red-700/70 bg-red-950/40 p-4 space-y-2">
			<p class="font-semibold text-red-200">{translate($language, 'nd.sec.title')}</p>
			<p class="text-sm text-red-200/80">{translate($language, 'nd.sec.body')}</p>
			<ul class="font-mono text-xs text-red-100 space-y-0.5">
				{#each runtimeSummary.vulnerable as r (r.website_id)}
					<li>{r.domain}: {r.installed_version} → {translate($language, 'nd.sec.needs')} ≥ v{r.security_baseline}</li>
				{/each}
				{#each runtimeSummary.endOfLife as r (r.website_id)}
					<li>{r.domain}: Node.js {r.selected_version} — {translate($language, 'nd.sec.eolLine')}</li>
				{/each}
			</ul>
			<div class="flex flex-wrap items-center gap-3 pt-1">
				{#if canManage && runtimeSummary.vulnerable.length > 0}
					<button onclick={() => (updateAllConfirm = true)} disabled={busy} class="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:opacity-50">{translate($language, 'nd.update.all')}</button>
				{/if}
				<a href="https://nodejs.org/en/blog/vulnerability/october-2026-security-releases" target="_blank" rel="noopener noreferrer" class="text-xs font-medium text-red-300 underline hover:text-red-100">{translate($language, 'nd.sec.advisory')} ↗</a>
			</div>
		</section>
	{/if}

	<!-- Website runtimes: patch level and updates -->
	<section class="rounded-lg border border-gray-700 bg-gray-800 p-5 space-y-3">
		<div class="flex flex-wrap items-center justify-between gap-3">
			<div>
				<h3 class="text-lg font-semibold text-white">{translate($language, 'nd.rt.title')}</h3>
				<p class="text-sm text-gray-400">{translate($language, 'nd.rt.hint')}</p>
			</div>
			{#if canManage && runtimeSummary.selected.length > 0}
				{#if pruneConfirm}
					<div class="flex flex-wrap items-center gap-2 text-sm">
						<span class="text-yellow-300">{translate($language, 'nd.prune.confirm').replace('{count}', String(runtimeSummary.selected.length))}</span>
						<button onclick={pruneAll} disabled={busy} class="rounded bg-blue-600 px-3 py-1.5 font-semibold text-white disabled:opacity-50">{translate($language, 'nd.prune.yes')}</button>
						<button onclick={() => (pruneConfirm = false)} class="px-2 py-1.5 text-gray-300">{translate($language, 'nd.cancel')}</button>
					</div>
				{:else if updateAllConfirm}
					<div class="flex items-center gap-2 text-sm">
						<span class="text-yellow-300">{translate($language, 'nd.update.allConfirm').replace('{count}', String(runtimeSummary.selected.length))}</span>
						<button onclick={updateAll} disabled={busy} class="rounded bg-blue-600 px-3 py-1.5 font-semibold text-white disabled:opacity-50">{translate($language, 'nd.update.yes')}</button>
						<button onclick={() => (updateAllConfirm = false)} class="px-2 py-1.5 text-gray-300">{translate($language, 'nd.cancel')}</button>
					</div>
				{:else}
					<div class="flex flex-wrap items-center gap-2">
						<button onclick={() => (pruneConfirm = true)} disabled={busy} title={translate($language, 'nd.prune.hint')} class="rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50">{translate($language, 'nd.prune.button')}</button>
						<button onclick={() => (updateAllConfirm = true)} disabled={busy} class="rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50">{translate($language, 'nd.update.all')}</button>
					</div>
				{/if}
			{/if}
		</div>
		{#if !loading && runtimeSummary.selected.length === 0}
			<p class="text-sm text-gray-400">{translate($language, 'nd.rt.empty')}</p>
		{:else}
			<ul class="divide-y divide-gray-700/60">
				{#each runtimeSummary.selected as r (r.website_id)}
					{@const state = nodeRuntimeState(r)}
					<li class="flex flex-wrap items-center justify-between gap-3 py-2.5">
						<div class="min-w-0">
							<a href="/websites/{r.website_id}" class="truncate font-medium text-blue-400 hover:underline">{r.domain}</a>
							<p class="text-xs text-gray-400">
								Node.js {r.selected_version}
								{#if r.installed_version}<span class="font-mono text-gray-300"> · {r.installed_version}</span>{/if}
								{#if r.npm_version}<span class="text-gray-500"> · npm {r.npm_version}</span>{/if}
							</p>
						</div>
						<div class="flex items-center gap-2">
							<span class="rounded-full px-2 py-0.5 text-[11px] font-semibold {state === 'ok' ? 'bg-green-900/50 text-green-300' : state === 'vulnerable' ? 'bg-red-900/60 text-red-200' : state === 'eol' ? 'bg-gray-700 text-gray-200' : 'bg-yellow-900/50 text-yellow-200'}">
								{translate($language, `nd.rt.state.${state}`)}
							</span>
							{#if canManage && state !== 'eol'}
								<button onclick={() => updateRuntime(r.website_id)} disabled={busy} class="rounded-md px-3 py-1 text-xs font-semibold text-white transition disabled:opacity-50 {state === 'vulnerable' ? 'bg-red-600 hover:bg-red-700' : 'bg-gray-700 hover:bg-gray-600'}">
									{translate($language, state === 'missing' ? 'nd.update.install' : 'nd.update.one')}
								</button>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		{/if}
		<p class="text-xs text-gray-500">{translate($language, 'nd.rt.panelNote')}</p>
	</section>

	{#if globalNode.installed}
		<section class="rounded-lg border border-yellow-800 p-5 space-y-3">
			<h3 class="font-semibold text-white">{translate($language, 'nd.legacy.title').replace('{version}', globalNode.version)}</h3>
			<p class="text-sm text-gray-400">{translate($language, 'nd.legacy.hint')}</p>
			{#if confirmGlobal}
				<p class="text-yellow-300">{translate($language, 'nd.legacy.confirm')}</p>
				<button onclick={removeGlobal} disabled={busy} class="rounded bg-red-600 px-4 py-2 text-white disabled:opacity-50">{translate($language, 'nd.legacy.confirmRemoval')}</button>
				<button onclick={() => confirmGlobal = false} disabled={busy} class="px-4 py-2 text-gray-300">{translate($language, 'nd.cancel')}</button>
			{:else}<button onclick={() => confirmGlobal = true} disabled={busy} class="rounded border border-red-700 px-4 py-2 text-red-400 disabled:opacity-50">{translate($language, 'nd.legacy.remove')}</button>{/if}
		</section>
	{/if}

	<section class="rounded-lg border border-gray-700 bg-gray-800 p-5 space-y-4">
		<div class="flex items-center justify-between"><h3 class="text-lg font-semibold text-white">{translate($language, 'nd.apps.title')}</h3><button onclick={() => showCreateForm = !showCreateForm} disabled={busy || loading} class="rounded bg-blue-600 px-4 py-2 text-white disabled:opacity-50">{translate($language, 'nd.apps.create')}</button></div>
		{#if showCreateForm}
			<form onsubmit={(event) => { event.preventDefault(); void createApp(); }} class="space-y-3 rounded border border-gray-700 p-4">
				<label class="block text-sm text-gray-300">{translate($language, 'nd.form.website')}
					<select bind:value={createWebsiteId} required disabled={busy} class="mt-1 block w-full rounded border border-gray-600 bg-gray-900 p-2">
						<option value="">{translate($language, 'nd.form.selectWebsite')}</option>{#each availableWebsites as runtime}<option value={runtime.website_id}>{runtime.domain} — Node.js {runtime.selected_version}</option>{/each}
					</select>
				</label>
				<p class="text-xs text-gray-400">{selectedRuntime ? translate($language, 'nd.form.inheritedRuntime').replace('{version}', selectedRuntime.selected_version) : translate($language, 'nd.form.onlyInstalled')}</p>
				<label class="block text-sm text-gray-300">{translate($language, 'nd.form.packageManager')}<select bind:value={createPackageMgr} disabled={busy} class="ml-3 rounded bg-gray-900 p-2"><option value="npm">npm</option><option value="yarn">yarn</option><option value="pnpm">pnpm</option></select></label>
				<label class="block text-sm text-gray-300">{translate($language, 'nd.form.buildCommand')}<input bind:value={createBuildCmd} disabled={busy} class="mt-1 block w-full rounded bg-gray-900 p-2" placeholder="npm run build" /></label>
				<label class="block text-sm text-gray-300">{translate($language, 'nd.form.startArgs')}<input bind:value={createStartCmd} required disabled={busy} class="mt-1 block w-full rounded bg-gray-900 p-2" placeholder={createPackageMgr === 'npm' ? 'server.js' : 'start'} /></label>
				<p class="text-xs text-gray-400">{translate($language, 'nd.form.startArgsHint')}</p>
				<label class="block text-sm text-gray-300">{translate($language, 'nd.form.port')}<input type="number" min="1" max="65535" required bind:value={createPort} disabled={busy} class="ml-3 rounded bg-gray-900 p-2" /></label>
				<button type="submit" disabled={busy || !selectedRuntime?.installed} class="rounded bg-green-600 px-4 py-2 text-white disabled:opacity-50">{translate($language, 'nd.form.create')}</button>
			</form>
		{/if}
		{#if !loading && apps.length === 0}<p class="text-gray-400">{translate($language, 'nd.apps.empty')}</p>{/if}
		{#each apps as app (app.id)}
			<div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-700 pt-3">
				<div><p class="text-white">{runtimes.find((runtime) => runtime.website_id === app.website_id)?.domain || app.website_id}</p><p class="text-sm text-gray-400">Node.js {app.node_version} · Port {app.port} · {app.status}</p></div>
				<div class="flex flex-wrap gap-2">
					{#each ['start', 'stop', 'restart'] as action}<button onclick={() => appAction(app.id, action)} disabled={busy} class="rounded border border-gray-600 px-3 py-1 text-gray-200 disabled:opacity-50">{action}</button>{/each}
					{#if deleteConfirmId === app.id}<button onclick={() => appAction(app.id, 'delete')} disabled={busy} class="text-red-400">{translate($language, 'nd.apps.confirmDelete')}</button><button onclick={() => deleteConfirmId = ''} class="text-gray-400">{translate($language, 'nd.cancel')}</button>
					{:else}<button onclick={() => deleteConfirmId = app.id} disabled={busy} class="text-red-400 disabled:opacity-50">{translate($language, 'nd.apps.delete')}</button>{/if}
				</div>
			</div>
		{/each}
	</section>
</div>
