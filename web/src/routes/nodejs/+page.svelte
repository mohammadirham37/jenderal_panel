<script lang="ts">
	import { onMount } from 'svelte';
	import { api, apiRaw } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface Runtime {
		error_message?: string;
		website_id: string; domain: string; web_user: string; selected_version: string;
		installed: boolean; installed_version: string; npm_version: string; nvm_version: string; nvm_state: string;
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
