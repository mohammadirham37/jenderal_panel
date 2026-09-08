<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { parseSimplePHPConfig, switchPHPConfigMode, updateSimplePHPConfig } from '$lib/php-config.js';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface PhpVersion {
		version: string;
		installed: boolean;
		running: boolean;
	}

	let phpVersions = $state<PhpVersion[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let actionInProgress = $state<string | null>(null);
	let currentTaskId = $state('');
	let configSaving = $state(false);
	let operationInProgress = $derived(actionInProgress !== null || !!currentTaskId || configSaving);

	// Config editor
	let configVersion = $state<string | null>(null);
	let configContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configSaveMsg = $state('');
	let configMode = $state<'simple' | 'advanced'>('simple');
	let simpleConfig = $state<Record<string, string>>(parseSimplePHPConfig(''));

	// Uninstall confirm
	let uninstallConfirmVersion = $state<string | null>(null);

	const allVersions = ['8.1', '8.2', '8.3', '8.4'];

	async function loadPhp() {
		loading = true;
		error = '';
		try {
			const data = (await api.get<PhpVersion[]>('/api/v1/php')) || [];
			// Ensure all known versions are represented
			phpVersions = allVersions.map((v) => {
				const found = data.find((p) => p.version === v);
				return found || { version: v, installed: false, running: false };
			});
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load PHP status';
		} finally {
			loading = false;
		}
	}

	async function installPhp(version: string) {
		actionMsg = '';
		actionError = '';
		currentTaskId = '';
		actionInProgress = `install-${version}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/php/${version}/install`);
			currentTaskId = result.task_id;
			actionMsg = `PHP ${version} installation started. See progress below.`;
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to install PHP ${version}`;
			actionInProgress = null;
		}
	}

	function onTaskComplete() {
		actionInProgress = null;
		loadPhp();
	}

	async function uninstallPhp(version: string) {
		actionMsg = '';
		actionError = '';
		uninstallConfirmVersion = null;
		actionInProgress = `uninstall-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/uninstall`);
			actionMsg = `PHP ${version} uninstalled.`;
			await loadPhp();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to uninstall PHP ${version}`;
		} finally {
			actionInProgress = null;
		}
	}

	async function restartPhp(version: string) {
		actionMsg = '';
		actionError = '';
		actionInProgress = `restart-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/restart`);
			actionMsg = `PHP ${version} restarted.`;
			await loadPhp();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to restart PHP ${version}`;
		} finally {
			actionInProgress = null;
		}
	}

	async function loadConfig(version: string) {
		if (configSaving) return;
		configVersion = version;
		configMode = 'simple';
		configLoading = true;
		configError = '';
		configSaveMsg = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/php/${version}/config`);
			configContent = data.content || '';
			simpleConfig = parseSimplePHPConfig(configContent);
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to load config';
		} finally {
			configLoading = false;
		}
	}

	async function persistConfig(content: string) {
		if (!configVersion || configSaving) return;
		configSaveMsg = '';
		configError = '';
		configSaving = true;
		try {
			await api.put(`/api/v1/php/${configVersion}/config`, { content });
			configContent = content;
			simpleConfig = parseSimplePHPConfig(content);
			configSaveMsg = 'Configuration saved successfully.';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to save config';
		} finally {
			configSaving = false;
		}
	}

	function saveSimpleConfig(event: SubmitEvent) {
		event.preventDefault();
		void persistConfig(updateSimplePHPConfig(configContent, simpleConfig));
	}

	function switchConfigMode(targetMode: 'simple' | 'advanced') {
		if (configSaving) return;
		const next = switchPHPConfigMode(
			{ mode: configMode, content: configContent, simpleConfig },
			targetMode
		);
		configMode = next.mode;
		configContent = next.content;
		simpleConfig = next.simpleConfig;
	}

	onMount(loadPhp);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">PHP Management</h2>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
		</div>
	{/if}

	{#if loading}
		<div class="text-gray-400">Loading PHP versions...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<!-- PHP Version Cards Grid -->
		<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
			{#each phpVersions as php}
				<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
					<div class="flex items-center justify-between mb-3">
						<h3 class="text-lg font-bold text-white">PHP {php.version}</h3>
					</div>

					<div class="flex flex-wrap gap-2 mb-4">
						<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {php.installed ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-400'}">
							{php.installed ? 'Installed' : 'Not Installed'}
						</span>
						{#if php.installed}
							<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {php.running ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}">
								{php.running ? 'Running' : 'Stopped'}
							</span>
						{/if}
					</div>

					<div class="flex flex-wrap gap-2">
						{#if !php.installed}
							<button
								onclick={() => installPhp(php.version)}
								disabled={operationInProgress}
								class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
							>
								{actionInProgress === `install-${php.version}` ? 'Installing...' : 'Install'}
							</button>
						{:else}
							{#if php.running}
								<button
									onclick={() => restartPhp(php.version)}
									disabled={operationInProgress}
									class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{actionInProgress === `restart-${php.version}` ? 'Restarting...' : 'Restart'}
								</button>
							{/if}

							<button
								onclick={() => loadConfig(php.version)}
								disabled={operationInProgress}
								class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded transition-colors cursor-pointer"
							>
								Config
							</button>

							{#if uninstallConfirmVersion === php.version}
								<div class="flex items-center gap-2 mt-2 w-full p-2 bg-red-900/30 border border-red-700 rounded-lg">
									<span class="text-xs text-red-300">Remove PHP {php.version}?</span>
									<button
										onclick={() => uninstallPhp(php.version)}
										disabled={operationInProgress}
										class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{actionInProgress === `uninstall-${php.version}` ? 'Removing...' : 'Yes'}
									</button>
									<button
										onclick={() => (uninstallConfirmVersion = null)}
										class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
									>
										No
									</button>
								</div>
							{:else}
								<button
									onclick={() => (uninstallConfirmVersion = php.version)}
									disabled={operationInProgress}
									class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
								>
									Uninstall
								</button>
							{/if}
						{/if}
					</div>
				</div>
			{/each}
		</div>

		<!-- php.ini Config Editor -->
		{#if configVersion}
			<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
				<div class="flex flex-wrap items-center justify-between gap-3 mb-3">
					<h3 class="text-lg font-semibold text-white">PHP {configVersion} Configuration (php.ini)</h3>
					<div class="flex items-center gap-2">
						<div class="inline-flex rounded-lg border border-gray-600 p-0.5" aria-label="PHP configuration mode">
							<button
								type="button"
								onclick={() => switchConfigMode('simple')}
								disabled={configSaving}
								class="px-3 py-1 text-sm rounded-md transition-colors cursor-pointer disabled:opacity-50 {configMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
							>
								Simple
							</button>
							<button
								type="button"
								onclick={() => switchConfigMode('advanced')}
								disabled={configSaving}
								class="px-3 py-1 text-sm rounded-md transition-colors cursor-pointer disabled:opacity-50 {configMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
							>
								Advanced
							</button>
						</div>
						<button
							onclick={() => { configVersion = null; configContent = ''; configError = ''; configSaveMsg = ''; }}
							disabled={configSaving}
							class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded transition-colors cursor-pointer"
						>
							Close
						</button>
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
				{:else if configMode === 'simple'}
					<form onsubmit={saveSimpleConfig}>
						<fieldset disabled={configSaving} class="space-y-4">
						<p class="text-sm text-gray-400">
							Edit common PHP settings here. Other php.ini directives will remain unchanged.
						</p>
						<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
							<label class="space-y-1 text-sm text-gray-300">
								<span>Memory Limit</span>
								<input bind:value={simpleConfig.memory_limit} required pattern="-1|[0-9]+[KMGkmg]?" placeholder="128M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Examples: 128M, 1G, or -1 for unlimited</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Upload Max Filesize</span>
								<input bind:value={simpleConfig.upload_max_filesize} required pattern="[0-9]+[KMGkmg]?" placeholder="2M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Maximum size for one uploaded file</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Post Max Size</span>
								<input bind:value={simpleConfig.post_max_size} required pattern="[0-9]+[KMGkmg]?" placeholder="8M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Should normally be at least the upload limit</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Max Execution Time</span>
								<input type="number" min="0" step="1" bind:value={simpleConfig.max_execution_time} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Seconds; 0 means no time limit</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Max Input Time</span>
								<input type="number" min="-1" step="1" bind:value={simpleConfig.max_input_time} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Seconds; -1 follows max execution time</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Max Input Vars</span>
								<input type="number" min="0" step="1" bind:value={simpleConfig.max_input_vars} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">Maximum accepted input variables per request</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>Display Errors</span>
								<select bind:value={simpleConfig.display_errors} class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500">
									<option value="Off">Off (recommended for production)</option>
									<option value="On">On</option>
								</select>
								<span class="block text-xs text-gray-500">Controls whether errors appear in responses</span>
							</label>
						</div>
						<button type="submit" disabled={configSaving} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer">
							{configSaving ? 'Saving...' : 'Save Configuration'}
						</button>
						</fieldset>
					</form>
				{:else}
					<textarea
						bind:value={configContent}
						disabled={configSaving}
						rows={20}
						class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
					></textarea>
					<div class="mt-2">
						<button
							onclick={() => persistConfig(configContent)}
							disabled={configSaving}
							class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
						>
							{configSaving ? 'Saving...' : 'Save Configuration'}
						</button>
					</div>
				{/if}
			</div>
		{/if}
	{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_php_task" onComplete={onTaskComplete} />
</div>
