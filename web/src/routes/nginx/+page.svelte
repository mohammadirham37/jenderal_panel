<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import LogViewer from '$lib/components/LogViewer.svelte';
	import { parseSimpleNginxConfig, switchNginxConfigMode, updateSimpleNginxConfig } from '$lib/nginx-config.js';
import { toast } from '$lib/stores/toast';

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
	const simpleFields = [
		{ key: 'worker_processes', label: 'Worker Processes', help: 'auto or a positive number', type: 'text' },
		{ key: 'worker_connections', label: 'Worker Connections', help: 'Maximum connections per worker', type: 'number' },
		{ key: 'client_max_body_size', label: 'Maximum Upload Size', help: 'For example 64M', type: 'text' },
		{ key: 'keepalive_timeout', label: 'Keepalive Timeout', help: 'Seconds', type: 'number' },
		{ key: 'server_tokens', label: 'Show Nginx Version', help: 'Controls server_tokens', type: 'toggle' },
		{ key: 'gzip', label: 'Gzip Compression', help: 'Compress supported responses', type: 'toggle' }
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
			configError = err instanceof Error ? err.message : 'Invalid configuration value';
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
			error = err instanceof Error ? err.message : 'Failed to load Nginx status';
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
			configError = err instanceof Error ? err.message : 'Failed to load config';
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		configSaveMsg = '';
		configError = '';
		try {
			await api.put('/api/v1/nginx/config', { content: configContent });
			configSaveMsg = 'Configuration saved successfully.';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to save config';
		}
	}

	async function loadSites() {
		sitesLoading = true;
		sitesError = '';
		try {
			sites = (await api.get<NginxSite[]>('/api/v1/nginx/sites')) || [];
		} catch (err) {
			sitesError = err instanceof Error ? err.message : 'Failed to load sites';
		} finally {
			sitesLoading = false;
		}
	}

	async function nginxAction(action: string) {
		actionInProgress = action;
		try {
			await api.post(`/api/v1/nginx/${action}`);
			toast.success(`Nginx ${action} completed successfully.`);
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : `Failed to ${action} Nginx`);
		} finally {
			actionInProgress = null;
		}
	}

	async function testConfig() {
		testResult = '';
		actionInProgress = 'test';
		try {
			const data = await api.post<{ ok: boolean; output: string }>('/api/v1/nginx/test');
			testResult = data.output || (data.ok ? 'Configuration test passed.' : 'Configuration test failed.');
			testResultOk = data.ok;
		} catch (err) {
			testResult = err instanceof Error ? err.message : 'Failed to test config';
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
			siteActionMsg = `Site "${site.name}" ${action}d successfully.`;
			await loadSites();
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : 'Failed to toggle site';
		}
	}

	async function editSite(site: NginxSite) {
		editingSite = site.name;
		try {
			const data = await api.get<{ content: string }>(`/api/v1/nginx/sites/${encodeURIComponent(site.name)}`);
			editingSiteConfig = data.content || '';
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : 'Failed to load site config';
			editingSite = null;
		}
	}

	async function saveSiteConfig() {
		if (!editingSite) return;
		siteActionMsg = '';
		siteActionError = '';
		try {
			await api.put(`/api/v1/nginx/sites/${encodeURIComponent(editingSite)}`, { content: editingSiteConfig });
			siteActionMsg = `Site "${editingSite}" config saved.`;
			editingSite = null;
			editingSiteConfig = '';
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : 'Failed to save site config';
		}
	}

	async function deleteSite(siteName: string) {
		siteActionMsg = '';
		siteActionError = '';
		try {
			await api.del(`/api/v1/nginx/sites/${encodeURIComponent(siteName)}`);
			siteActionMsg = `Site "${siteName}" deleted.`;
			await loadSites();
		} catch (err) {
			siteActionError = err instanceof Error ? err.message : 'Failed to delete site';
		}
	}

	onMount(() => {
		loadStatus();
		loadConfig();
		loadSites();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Nginx</h2>



	<!-- Status Card -->
	{#if loading}
		<div class="text-gray-400">Loading Nginx status...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if status}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.installed ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.installed ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.installed ? 'Installed' : 'Not Installed'}
				</span>
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.running ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.running ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.running ? 'Running' : 'Stopped'}
				</span>
				{#if status.version}
					<span class="text-xs text-gray-400">Version: <span class="text-gray-200">{status.version}</span></span>
				{/if}
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.config_ok ? 'bg-green-900/50 text-green-400' : 'bg-yellow-900/50 text-yellow-400'}">
					Config {status.config_ok ? 'OK' : 'Error'}
				</span>
			</div>

			<div class="flex flex-wrap gap-2">
				{#if !status.installed}
					<button
						onclick={() => nginxAction('install')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'install' ? 'Installing...' : 'Install'}
					</button>
				{:else}
					<button
						onclick={() => nginxAction('start')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'start' ? 'Starting...' : 'Start'}
					</button>
					<button
						onclick={() => nginxAction('stop')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'stop' ? 'Stopping...' : 'Stop'}
					</button>
					<button
						onclick={() => nginxAction('restart')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'restart' ? 'Restarting...' : 'Restart'}
					</button>
					<button
						onclick={() => nginxAction('reload')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'reload' ? 'Reloading...' : 'Reload'}
					</button>
					<button
						onclick={testConfig}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-purple-600 hover:bg-purple-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'test' ? 'Testing...' : 'Test Config'}
					</button>
				{/if}
			</div>

			{#if testResult}
				<div class="mt-3 p-3 rounded-lg text-sm {testResultOk ? 'bg-green-900/50 border border-green-700 text-green-300' : 'bg-red-900/50 border border-red-700 text-red-300'}">
					<pre class="whitespace-pre-wrap font-mono text-xs">{testResult}</pre>
					<button onclick={() => (testResult = '')} class="mt-1 text-xs hover:underline cursor-pointer">Dismiss</button>
				</div>
			{/if}
		</div>
	{/if}

	<!-- Config Editor -->
	{#if status?.installed}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
				<h3 class="text-lg font-semibold text-white">Configuration</h3>
				<div class="inline-flex rounded-lg border border-gray-600 bg-gray-900 p-1" aria-label="Nginx configuration mode">
					<button onclick={() => changeConfigMode('simple')} class="rounded-md px-3 py-1.5 text-sm {configMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}">Simple</button>
					<button onclick={() => changeConfigMode('manual')} class="rounded-md px-3 py-1.5 text-sm {configMode === 'manual' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}">Manual</button>
				</div>
			</div>

			{#if configError}
				<div class="mb-2 text-red-400 text-sm">{configError}</div>
			{/if}
			{#if configSaveMsg}
				<div class="mb-2 text-green-400 text-sm">
					{configSaveMsg}
					<button onclick={() => (configSaveMsg = '')} class="ml-2 hover:underline cursor-pointer">Dismiss</button>
				</div>
			{/if}

			{#if configLoading}
				<div class="text-gray-400 text-sm">Loading configuration...</div>
			{:else}
				{#if configMode === 'simple'}
					{#each simpleConfigErrors as parseError}
						<div class="mb-2 rounded border border-yellow-700 bg-yellow-900/40 p-3 text-sm text-yellow-300">{parseError} Switch to Manual mode to repair the structure.</div>
					{/each}
					<div class="grid gap-4 md:grid-cols-2">
						{#each simpleFields as field}
							<div>
								<label for="nginx-{field.key}" class="mb-1 block text-sm font-medium text-gray-200">{field.label}</label>
								{#if field.type === 'toggle'}
									<select id="nginx-{field.key}" value={simpleConfig[field.key]} onchange={(event) => setSimpleField(field.key, event.currentTarget.value)} class="w-full rounded border border-gray-600 bg-gray-900 px-3 py-2 text-white">
										<option value="on">On</option><option value="off">Off</option>
									</select>
								{:else}
									<input id="nginx-{field.key}" type={field.type} min={field.type === 'number' ? '0' : undefined} value={simpleConfig[field.key]} onchange={(event) => setSimpleField(field.key, event.currentTarget.value)} class="w-full rounded border border-gray-600 bg-gray-900 px-3 py-2 text-white" />
								{/if}
								<p class="mt-1 text-xs text-gray-400">{field.help}</p>
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
						Save Configuration
					</button>
				</div>
			{/if}
		</div>

		<!-- Sites -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Sites</h3>

			{#if siteActionMsg}
				<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
					{siteActionMsg}
					<button onclick={() => (siteActionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
				</div>
			{/if}
			{#if siteActionError}
				<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{siteActionError}
					<button onclick={() => (siteActionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
				</div>
			{/if}

			{#if sitesLoading}
				<div class="text-gray-400 text-sm">Loading sites...</div>
			{:else if sites.length === 0}
				<div class="text-gray-400 text-sm">No sites configured.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each sites as site}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-medium">{site.name}</td>
									<td class="px-4 py-3">
										<button
											onclick={() => toggleSite(site)}
											aria-label="{site.enabled ? 'Disable' : 'Enable'} {site.name}"
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
												Edit
											</button>
											<button
												onclick={() => deleteSite(site.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
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
					<h4 class="text-sm font-semibold text-white mb-2">Editing: {editingSite}</h4>
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
							Save
						</button>
						<button
							onclick={() => { editingSite = null; editingSiteConfig = ''; }}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Cancel
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Logs -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Logs</h3>
			<div class="flex gap-2 mb-4">
				<button
					onclick={() => (logTab = 'access')}
					class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'access'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					Access Log
				</button>
				<button
					onclick={() => (logTab = 'error')}
					class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'error'
						? 'bg-blue-600 text-white'
						: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
				>
					Error Log
				</button>
			</div>

			{#if logTab === 'access'}
				<LogViewer path="/var/log/nginx/access.log" title="Access Log" />
			{:else}
				<LogViewer path="/var/log/nginx/error.log" title="Error Log" />
			{/if}
		</div>
	{/if}
</div>
