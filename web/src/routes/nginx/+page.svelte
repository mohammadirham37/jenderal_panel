<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import LogViewer from '$lib/components/LogViewer.svelte';
	import { parseSimpleNginxConfig, switchNginxConfigMode, updateSimpleNginxConfig } from '$lib/nginx-config.js';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	interface NginxStatus {
		installed: boolean;
		running: boolean;
		version: string;
		config_ok: boolean;
	}

	interface NginxSite {
		name: string;
		enabled: boolean;
		config: string;
	}

	let status = $state<NginxStatus | null>(null);
	let loading = $state(true);
	let error = $state('');
	let actionInProgress = $state<string | null>(null);

	// Config editor
	let configContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configSaveMsg = $state('');
	let configMode = $state<'simple' | 'manual'>('simple');
	let simpleConfig = $state<Record<string, string>>({});
	let simpleConfigErrors = $state<string[]>([]);
	// label/help hold i18n keys, translated at render time
	const simpleFields = [
		{ key: 'worker_processes', label: 'ngx.fWorkerProcesses', help: 'ngx.hWorkerProcesses', type: 'text' },
		{ key: 'worker_connections', label: 'ngx.fWorkerConnections', help: 'ngx.hWorkerConnections', type: 'number' },
		{ key: 'client_max_body_size', label: 'ngx.fUploadSize', help: 'ngx.hUploadSize', type: 'text' },
		{ key: 'keepalive_timeout', label: 'ngx.fKeepaliveTimeout', help: 'ngx.hKeepaliveTimeout', type: 'number' },
		{ key: 'server_tokens', label: 'ngx.fServerTokens', help: 'ngx.hServerTokens', type: 'toggle' },
		{ key: 'gzip', label: 'ngx.fGzip', help: 'ngx.hGzip', type: 'toggle' }
	];

	function refreshSimpleConfig() {
		const parsed = parseSimpleNginxConfig(configContent);
		simpleConfig = parsed.values;
		simpleConfigErrors = parsed.errors;
	}

	function changeConfigMode(target: 'simple' | 'manual') {
		const next = switchNginxConfigMode({ mode: configMode, draft: configContent }, target);
		configMode = next.mode;
		if (target === 'simple') refreshSimpleConfig();
	}

	function setSimpleField(key: string, value: string) {
		try {
			configContent = updateSimpleNginxConfig(configContent, key, value);
			refreshSimpleConfig();
			configError = '';
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'ngx.invalidConfigValue');
		}
	}

	// Sites
	let sites = $state<NginxSite[]>([]);
	let sitesLoading = $state(false);
	let sitesError = $state('');
	let editingSite = $state<string | null>(null);
	let editingSiteConfig = $state('');
	let siteActionMsg = $state('');
	let siteActionError = $state('');

	// Test config result
	let testResult = $state('');
	let testResultOk = $state(false);

	// Log tab
	let logTab = $state<'access' | 'error'>('access');

	async function loadStatus() {
		try {
			status = await api.get<NginxStatus>('/api/v1/nginx/status');
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'ngx.loadStatusFailed');
		} finally {
			loading = false;
		}
	}

	async function loadConfig() {
		configLoading = true;
		configError = '';
		try {
			const data = await api.get<{ content: string }>('/api/v1/nginx/config');
			configContent = data.content || '';
			refreshSimpleConfig();
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'ngx.loadConfigFailed');
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		configSaveMsg = '';
		configError = '';
		try {
			await api.put('/api/v1/nginx/config', { content: configContent });
			configSaveMsg = translate($language, 'ngx.configSaved');
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'ngx.saveConfigFailed');
		}
	}

	async function loadSites() {
		sitesLoading = true;
		sitesError = '';
		try {
			sites = (await api.get<NginxSite[]>('/api/v1/nginx/sites')) || [];
		} catch (err) {
			sitesError = err instanceof Error ? err.message : translate($language, 'ngx.loadSitesFailed');
		} finally {
			sitesLoading = false;
		}
	}

	// Translation key maps for install/start/stop/restart/reload actions
	const NGINX_DONE: Record<string, string> = {
		install: 'ngx.installDone',
		start: 'ngx.startDone',
		stop: 'ngx.stopDone',
		restart: 'ngx.restartDone',
		reload: 'ngx.reloadDone'
	};
	const NGINX_FAILED: Record<string, string> = {
		install: 'ngx.installFailed',
		start: 'ngx.startFailed',
		stop: 'ngx.stopFailed',
		restart: 'ngx.restartFailed',
		reload: 'ngx.reloadFailed'
	};

	async function nginxAction(action: string) {
		actionInProgress = action;
		try {
			await api.post(`/api/v1/nginx/${action}`);
			toast.success(translate($language, NGINX_DONE[action]));
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, NGINX_FAILED[action]));
		} finally {
			actionInProgress = null;
		}
	}

	async function testConfig() {
		testResult = '';
		actionInProgress = 'test';
		try {
			const data = await api.post<{ ok: boolean; output: string }>('/api/v1/nginx/test');
			testResult = data.output || translate($language, data.ok ? 'ngx.testPassed' : 'ngx.testFailed');
			testResultOk = data.ok;
		} catch (err) {
			testResult = err instanceof Error ? err.message : translate($language, 'ngx.testConfigFailed');
			testResultOk = false;
		} finally {
			actionInProgress = null;
		}
	}

	async function toggleSite(site: NginxSite) {
		siteActionMsg = '';
		siteActionError = '';
		try {
			const action = site.enabled ? 'disable' : 'enable';
			await api.post(`/api/v1/nginx/sites/${encodeURIComponent(site.name)}/${action}`);
			siteActionMsg = translate($language, action === 'enable' ? 'ngx.siteEnabled' : 'ngx.siteDisabled').replace('{name}', site.name);
			await loadSites();
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : translate($language, 'ngx.toggleSiteFailed');
		}
	}

	async function editSite(site: NginxSite) {
		editingSite = site.name;
		try {
			const data = await api.get<{ content: string }>(`/api/v1/nginx/sites/${encodeURIComponent(site.name)}`);
			editingSiteConfig = data.content || '';
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : translate($language, 'ngx.loadSiteConfigFailed');
			editingSite = null;
		}
	}

	async function saveSiteConfig() {
		if (!editingSite) return;
		siteActionMsg = '';
		siteActionError = '';
		try {
			await api.put(`/api/v1/nginx/sites/${encodeURIComponent(editingSite)}`, { content: editingSiteConfig });
			siteActionMsg = translate($language, 'ngx.siteConfigSaved').replace('{name}', editingSite);
			editingSite = null;
			editingSiteConfig = '';
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : translate($language, 'ngx.saveSiteConfigFailed');
		}
	}

	async function deleteSite(siteName: string) {
		siteActionMsg = '';
		siteActionError = '';
		try {
			await api.del(`/api/v1/nginx/sites/${encodeURIComponent(siteName)}`);
			siteActionMsg = translate($language, 'ngx.siteDeleted').replace('{name}', siteName);
			await loadSites();
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : translate($language, 'ngx.deleteSiteFailed');
		}
	}

	onMount(() => {
		loadStatus();
		loadConfig();
		loadSites();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'ngx.title')}</h2>



	<!-- Status Card -->
	{#if loading}
		<div class="text-gray-400">{translate($language, 'ngx.loadingStatus')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if status}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.installed ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.installed ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.installed ? translate($language, 'ngx.installed') : translate($language, 'ngx.notInstalled')}
				</span>
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.running ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.running ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.running ? translate($language, 'ngx.running') : translate($language, 'ngx.stopped')}
				</span>
				{#if status.version}
					<span class="text-xs text-gray-400">{translate($language, 'ngx.versionLabel')} <span class="text-gray-200">{status.version}</span></span>
				{/if}
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.config_ok ? 'bg-green-900/50 text-green-400' : 'bg-yellow-900/50 text-yellow-400'}">
					{translate($language, status.config_ok ? 'ngx.configOk' : 'ngx.configError')}
				</span>
			</div>

			<div class="flex flex-wrap gap-2">
				{#if !status.installed}
					<button
						onclick={() => nginxAction('install')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'install' ? translate($language, 'ngx.installing') : translate($language, 'ngx.install')}
					</button>
				{:else}
					<button
						onclick={() => nginxAction('start')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'start' ? translate($language, 'ngx.starting') : translate($language, 'ngx.start')}
					</button>
					<button
						onclick={() => nginxAction('stop')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'stop' ? translate($language, 'ngx.stopping') : translate($language, 'ngx.stop')}
					</button>
					<button
						onclick={() => nginxAction('restart')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'restart' ? translate($language, 'ngx.restarting') : translate($language, 'ngx.restart')}
					</button>
					<button
						onclick={() => nginxAction('reload')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'reload' ? translate($language, 'ngx.reloading') : translate($language, 'ngx.reload')}
					</button>
					<button
						onclick={testConfig}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-purple-600 hover:bg-purple-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'test' ? translate($language, 'ngx.testing') : translate($language, 'ngx.testConfig')}
					</button>
				{/if}
			</div>

			{#if testResult}
				<div class="mt-3 p-3 rounded-lg text-sm {testResultOk ? 'bg-green-900/50 border border-green-700 text-green-300' : 'bg-red-900/50 border border-red-700 text-red-300'}">
					<pre class="whitespace-pre-wrap font-mono text-xs">{testResult}</pre>
					<button onclick={() => (testResult = '')} class="mt-1 text-xs hover:underline cursor-pointer">{translate($language, 'ngx.dismiss')}</button>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Config Editor -->
	{#if status?.installed}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
				<h3 class="text-lg font-semibold text-white">{translate($language, 'ngx.configuration')}</h3>
				<div class="inline-flex rounded-lg border border-gray-600 bg-gray-900 p-1" aria-label={translate($language, 'ngx.configModeAria')}>
					<button onclick={() => changeConfigMode('simple')} class="rounded-md px-3 py-1.5 text-sm {configMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}">{translate($language, 'ngx.simple')}</button>
					<button onclick={() => changeConfigMode('manual')} class="rounded-md px-3 py-1.5 text-sm {configMode === 'manual' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}">{translate($language, 'ngx.manual')}</button>
				</div>
			</div>

			{#if configError}
				<div class="mb-2 text-red-400 text-sm">{configError}</div>
			{/if}
			{#if configSaveMsg}
				<div class="mb-2 text-green-400 text-sm">
					{configSaveMsg}
					<button onclick={() => (configSaveMsg = '')} class="ml-2 hover:underline cursor-pointer">{translate($language, 'ngx.dismiss')}</button>
				</div>
			{/if}

			{#if configLoading}
				<div class="text-gray-400 text-sm">{translate($language, 'ngx.loadingConfig')}</div>
			{:else}
				{#if configMode === 'simple'}
					{#each simpleConfigErrors as parseError}
						<div class="mb-2 rounded border border-yellow-700 bg-yellow-900/40 p-3 text-sm text-yellow-300">{parseError} {translate($language, 'ngx.repairHint')}</div>
					{/each}
					<div class="grid gap-4 md:grid-cols-2">
						{#each simpleFields as field}
							<div>
								<label for="nginx-{field.key}" class="mb-1 block text-sm font-medium text-gray-200">{translate($language, field.label)}</label>
								{#if field.type === 'toggle'}
									<select id="nginx-{field.key}" value={simpleConfig[field.key]} onchange={(event) => setSimpleField(field.key, event.currentTarget.value)} class="w-full rounded border border-gray-600 bg-gray-900 px-3 py-2 text-white">
										<option value="on">{translate($language, 'ngx.on')}</option><option value="off">{translate($language, 'ngx.off')}</option>
									</select>
								{:else}
									<input id="nginx-{field.key}" type={field.type} min={field.type === 'number' ? '0' : undefined} value={simpleConfig[field.key]} onchange={(event) => setSimpleField(field.key, event.currentTarget.value)} class="w-full rounded border border-gray-600 bg-gray-900 px-3 py-2 text-white" />
								{/if}
								<p class="mt-1 text-xs text-gray-400">{translate($language, field.help)}</p>
							</div>
						{/each}
					</div>
				{:else}
					<textarea bind:value={configContent} oninput={() => (simpleConfigErrors = [])} rows={20} class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"></textarea>
				{/if}
				<div class="mt-2">
					<button
						onclick={saveConfig}
						disabled={simpleConfigErrors.length > 0}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{translate($language, 'ngx.saveConfig')}
					</button>
				</div>
			{/if}
		</div>

		<!-- Sites -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'ngx.sites')}</h3>

			{#if siteActionMsg}
				<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
					{siteActionMsg}
					<button onclick={() => (siteActionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">{translate($language, 'ngx.dismiss')}</button>
				</div>
			{/if}
			{#if siteActionError}
				<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{siteActionError}
					<button onclick={() => (siteActionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">{translate($language, 'ngx.dismiss')}</button>
				</div>
			{/if}

			{#if sitesLoading}
				<div class="text-gray-400 text-sm">{translate($language, 'ngx.loadingSites')}</div>
			{:else if sites.length === 0}
				<div class="text-gray-400 text-sm">{translate($language, 'ngx.noSites')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'ngx.thName')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'ngx.thEnabled')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'ngx.thActions')}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each sites as site}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-medium">{site.name}</td>
									<td class="px-4 py-3">
										<button
											onclick={() => toggleSite(site)}
											aria-label={site.enabled ? translate($language, 'ngx.disable').replace('{name}', site.name) : translate($language, 'ngx.enable').replace('{name}', site.name)}
											class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors cursor-pointer {site.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
										>
											<span class="inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform {site.enabled ? 'translate-x-4.5' : 'translate-x-0.5'}"></span>
										</button>
									</td>
									<td class="px-4 py-3 text-right">
										<div class="flex items-center justify-end gap-2">
											<button
												onclick={() => editSite(site)}
												class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'ngx.edit')}
											</button>
											<button
												onclick={() => deleteSite(site.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'ngx.delete')}
											</button>
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if editingSite}
				<div class="mt-4 p-4 bg-gray-900 rounded-lg border border-gray-600">
					<h4 class="text-sm font-semibold text-white mb-2">{translate($language, 'ngx.editing').replace('{name}', editingSite)}</h4>
					<textarea
						bind:value={editingSiteConfig}
						rows={15}
						class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
					></textarea>
					<div class="mt-2 flex gap-2">
						<button
							onclick={saveSiteConfig}
							class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'ngx.save')}
						</button>
						<button
							onclick={() => { editingSite = null; editingSiteConfig = ''; }}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'ngx.cancel')}
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Logs -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'ngx.logs')}</h3>
			<div class="flex gap-2 mb-4">
				<button
					onclick={() => (logTab = 'access')}
					class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'access'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					{translate($language, 'ngx.accessLog')}
				</button>
				<button
					onclick={() => (logTab = 'error')}
					class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'error'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					{translate($language, 'ngx.errorLog')}
				</button>
			</div>

			{#if logTab === 'access'}
				<LogViewer path="/var/log/nginx/access.log" title={translate($language, 'ngx.accessLog')} />
			{:else}
				<LogViewer path="/var/log/nginx/error.log" title={translate($language, 'ngx.errorLog')} />
			{/if}
		</div>
	{/if}
</div>
