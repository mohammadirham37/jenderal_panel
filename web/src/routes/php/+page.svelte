<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { parseSimplePHPConfig, switchPHPConfigMode, updateSimplePHPConfig } from '$lib/php-config.js';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface PhpVersion {
		version: string;
		installed: boolean;
		running: boolean;
	}

	let phpVersions = $state<PhpVersion[]>([]);
	let loading = $state(true);
	let error = $state('');
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
			error = err instanceof Error ? err.message : translate($language, 'phpv.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function installPhp(version: string) {
		currentTaskId = '';
		actionInProgress = `install-${version}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/php/${version}/install`);
			currentTaskId = result.task_id;
			toast.success(translate($language, 'phpv.toast.installStarted').replace('{version}', version));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.installFailed').replace('{version}', version));
			actionInProgress = null;
		}
	}

	function onTaskComplete() {
		actionInProgress = null;
		loadPhp();
	}

	async function uninstallPhp(version: string) {
		uninstallConfirmVersion = null;
		actionInProgress = `uninstall-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/uninstall`);
			toast.success(translate($language, 'phpv.toast.uninstalled').replace('{version}', version));
			await loadPhp();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.uninstallFailed').replace('{version}', version));
		} finally {
			actionInProgress = null;
		}
	}

	async function restartPhp(version: string) {
		actionInProgress = `restart-${version}`;
		try {
			await api.post(`/api/v1/php/${version}/restart`);
			toast.success(translate($language, 'phpv.toast.restarted').replace('{version}', version));
			await loadPhp();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'phpv.toast.restartFailed').replace('{version}', version));
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
			configError = err instanceof Error ? err.message : translate($language, 'phpv.config.loadFailed');
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
			configSaveMsg = translate($language, 'phpv.config.saved');
		} catch (err) {
			configError = err instanceof Error ? err.message : translate($language, 'phpv.config.saveFailed');
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
	<h2 class="text-2xl font-bold text-white">{translate($language, 'phpv.title')}</h2>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'phpv.loading')}</div>
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
							{php.installed ? translate($language, 'phpv.installed') : translate($language, 'phpv.notInstalled')}
						</span>
						{#if php.installed}
							<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {php.running ? 'bg-green-900 text-green-300' : 'bg-red-900 text-red-300'}">
								{php.running ? translate($language, 'phpv.running') : translate($language, 'phpv.stopped')}
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
								{actionInProgress === `install-${php.version}` ? translate($language, 'phpv.installing') : translate($language, 'phpv.install')}
							</button>
						{:else}
							{#if php.running}
								<button
									onclick={() => restartPhp(php.version)}
									disabled={operationInProgress}
									class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{actionInProgress === `restart-${php.version}` ? translate($language, 'phpv.restarting') : translate($language, 'phpv.restart')}
								</button>
							{/if}

							<button
								onclick={() => loadConfig(php.version)}
								disabled={operationInProgress}
								class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded transition-colors cursor-pointer"
							>
								{translate($language, 'phpv.config')}
							</button>

							{#if uninstallConfirmVersion === php.version}
								<div class="flex items-center gap-2 mt-2 w-full p-2 bg-red-900/30 border border-red-700 rounded-lg">
									<span class="text-xs text-red-300">{translate($language, 'phpv.removeConfirm').replace('{version}', php.version)}</span>
									<button
										onclick={() => uninstallPhp(php.version)}
										disabled={operationInProgress}
										class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{actionInProgress === `uninstall-${php.version}` ? translate($language, 'phpv.removing') : translate($language, 'phpv.yes')}
									</button>
									<button
										onclick={() => (uninstallConfirmVersion = null)}
										class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{translate($language, 'phpv.no')}
									</button>
								</div>
							{:else}
								<button
									onclick={() => (uninstallConfirmVersion = php.version)}
									disabled={operationInProgress}
									class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
								>
									{translate($language, 'phpv.uninstall')}
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
					<h3 class="text-lg font-semibold text-white">{translate($language, 'phpv.config.title').replace('{version}', configVersion)}</h3>
					<div class="flex items-center gap-2">
						<div class="inline-flex rounded-lg border border-gray-600 p-0.5" aria-label={translate($language, 'phpv.config.modeAria')}>
							<button
								type="button"
								onclick={() => switchConfigMode('simple')}
								disabled={configSaving}
								class="px-3 py-1 text-sm rounded-md transition-colors cursor-pointer disabled:opacity-50 {configMode === 'simple' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
							>
								{translate($language, 'phpv.config.simple')}
							</button>
							<button
								type="button"
								onclick={() => switchConfigMode('advanced')}
								disabled={configSaving}
								class="px-3 py-1 text-sm rounded-md transition-colors cursor-pointer disabled:opacity-50 {configMode === 'advanced' ? 'bg-blue-600 text-white' : 'text-gray-300 hover:bg-gray-700'}"
							>
								{translate($language, 'phpv.config.advanced')}
							</button>
						</div>
						<button
							onclick={() => { configVersion = null; configContent = ''; configError = ''; configSaveMsg = ''; }}
							disabled={configSaving}
							class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'phpv.config.close')}
						</button>
					</div>
				</div>

				{#if configError}
					<div class="mb-2 text-red-400 text-sm">{configError}</div>
				{/if}
				{#if configSaveMsg}
					<div class="mb-2 text-green-400 text-sm">
						{configSaveMsg}
						<button onclick={() => (configSaveMsg = '')} class="ml-2 hover:underline cursor-pointer">{translate($language, 'phpv.config.dismiss')}</button>
					</div>
				{/if}

				{#if configLoading}
					<div class="text-gray-400 text-sm">{translate($language, 'phpv.config.loading')}</div>
				{:else if configMode === 'simple'}
					<form onsubmit={saveSimpleConfig}>
						<fieldset disabled={configSaving} class="space-y-4">
						<p class="text-sm text-gray-400">
							{translate($language, 'phpv.config.hint')}
						</p>
						<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.memoryLimit')}</span>
								<input bind:value={simpleConfig.memory_limit} required pattern="-1|[0-9]+[KMGkmg]?" placeholder="128M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.memoryLimitHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.uploadMaxFilesize')}</span>
								<input bind:value={simpleConfig.upload_max_filesize} required pattern="[0-9]+[KMGkmg]?" placeholder="2M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.uploadMaxFilesizeHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.postMaxSize')}</span>
								<input bind:value={simpleConfig.post_max_size} required pattern="[0-9]+[KMGkmg]?" placeholder="8M" class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.postMaxSizeHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.maxExecutionTime')}</span>
								<input type="number" min="0" step="1" bind:value={simpleConfig.max_execution_time} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxExecutionTimeHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.maxInputTime')}</span>
								<input type="number" min="-1" step="1" bind:value={simpleConfig.max_input_time} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxInputTimeHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.maxInputVars')}</span>
								<input type="number" min="0" step="1" bind:value={simpleConfig.max_input_vars} required class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500" />
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.maxInputVarsHint')}</span>
							</label>
							<label class="space-y-1 text-sm text-gray-300">
								<span>{translate($language, 'phpv.config.displayErrors')}</span>
								<select bind:value={simpleConfig.display_errors} class="w-full px-3 py-2 bg-gray-950 border border-gray-700 rounded text-white focus:outline-none focus:ring-2 focus:ring-blue-500">
									<option value="Off">{translate($language, 'phpv.config.displayErrorsOff')}</option>
									<option value="On">{translate($language, 'phpv.config.displayErrorsOn')}</option>
								</select>
								<span class="block text-xs text-gray-500">{translate($language, 'phpv.config.displayErrorsHint')}</span>
							</label>
						</div>
						<button type="submit" disabled={configSaving} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer">
							{configSaving ? translate($language, 'phpv.config.saving') : translate($language, 'phpv.config.save')}
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
							{configSaving ? translate($language, 'phpv.config.saving') : translate($language, 'phpv.config.save')}
						</button>
					</div>
				{/if}
			</div>
		{/if}
	{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_php_task" onComplete={onTaskComplete} onMissing={() => { actionInProgress = null; }} />
</div>
