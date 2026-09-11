<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
	import { availablePHPVersions, normalizeWebsiteSelection, selectedCombination } from '$lib/website-form.js';

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		php_version: string;
		status: string;
		error_message?: string;
		framework: string;
		framework_version: string;
		frontend_stack: string;
		inertia_adapter: string;
		project_variant: string;
		setup_mode: string;
		provision_stage: string;
		provision_log: string;
		created_at: string;
	}
	interface RuntimeOption { version: string; installed: boolean; running: boolean }
	interface DependencyOption { name: string; version: string; installed: boolean; manage_url: string }
	interface ProfileOption {
		template: string; framework_version: string; frontend_stack: string; inertia_adapter: string;
		project_variant: string; setup_mode: string; enabled: boolean; reason: string; minimum_php: string;
		document_root: string; prerequisites: string[]; php_compatibility: { version: string; enabled: boolean; reason: string }[];
	}
	interface WebsiteSelection {
		node_version: string;
		template: string; php_version: string; framework_version: string; frontend_stack: string;
		inertia_adapter: string; project_variant: string; setup_mode: string;
	}
	interface WebsiteOptions {
		node_versions: string[];
		php_versions: RuntimeOption[]; dependencies: DependencyOption[]; profiles: ProfileOption[];
		inertia_adapters: string[]; defaults: WebsiteSelection;
	}

	let websites = $state<Website[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let options = $state<WebsiteOptions | null>(null);
	let optionsError = $state('');
	let repairTaskId = $state('');
	let repairing = $state(false);
	let repairConfirmId = $state<string | null>(null);
	let repairBusy = $derived(repairing || !!repairTaskId);

	async function repairLaravel(id: string) {
		if (repairBusy) return;
		repairing = true; actionError = ''; repairConfirmId = null;
		try {
			const result = await api.post<{task_id: string}>(`/api/v1/websites/${id}/repair-laravel`, {confirm: true});
			repairTaskId = result.task_id;
		} catch (err) { actionError = err instanceof Error ? err.message : 'Laravel repair failed'; repairing = false; }
	}

	// Create form
	let showCreateForm = $state(false);
	let createDomain = $state('');
	let selection = $state<WebsiteSelection>({ template: 'php', php_version: '', framework_version: '', frontend_stack: '', inertia_adapter: '', project_variant: 'empty', setup_mode: 'config-only', node_version: '' });
	let creating = $state(false);
	let installedPHP = $derived(options ? availablePHPVersions(options) as RuntimeOption[] : []);
	let combination = $derived(options ? selectedCombination(options, selection) : null);
	let laravelVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel').map((item) => item.framework_version))] : []);
	let laravelOctaneVersions = $derived(options ? [...new Set(options.profiles.filter((item) => item.template === 'laravel-octane').map((item) => item.framework_version))] : []);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;

	const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

	function normalizeSelection() {
		if (options) selection = normalizeWebsiteSelection(selection, options) as WebsiteSelection;
	}

	async function loadOptions() {
		optionsError = '';
		try {
			options = await api.get<WebsiteOptions>('/api/v1/websites/options');
			selection = normalizeWebsiteSelection(options.defaults || {}, options) as WebsiteSelection;
		} catch (err) {
			optionsError = err instanceof Error ? err.message : 'Failed to load website options';
		}
	}

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'active':
				return 'bg-green-900 text-green-300';
			case 'pending':
			case 'installing':
			case 'configuring':
			case 'validating':
				return 'bg-yellow-900 text-yellow-300 animate-pulse';
			case 'failed':
				return 'bg-red-900 text-red-300';
			case 'suspended':
			case 'disabled':
				return 'bg-gray-700 text-gray-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function shouldPoll(sites: Website[]): boolean {
		return sites.some((w) => pendingStatuses.includes(w.status));
	}

	function startPolling() {
		stopPolling();
		pollTimer = setInterval(async () => {
			try {
				websites = (await api.get<Website[]>('/api/v1/websites')) || [];
				if (!shouldPoll(websites)) {
					stopPolling();
				}
			} catch {
				// Silently ignore polling errors
			}
		}, 3000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	async function loadWebsites() {
		loading = true;
		error = '';
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
			if (shouldPoll(websites)) {
				startPolling();
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load websites';
		} finally {
			loading = false;
		}
	}

	async function createWebsite() {
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			const body: Record<string, string> = { domain: createDomain, ...selection };
			await api.post('/api/v1/websites', body);
			actionMsg = `Website "${createDomain}" creation started.`;
			showCreateForm = false;
			createDomain = '';
			if (options) selection = normalizeWebsiteSelection(options.defaults || {}, options) as WebsiteSelection;
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create website';
		} finally {
			creating = false;
		}
	}

	async function suspendWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/suspend`);
			actionMsg = 'Website suspended.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to suspend website';
		}
	}

	async function enableWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/enable`);
			actionMsg = 'Website enabled.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to enable website';
		}
	}

	async function deleteWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		deleteConfirmId = null;
		try {
			await api.del(`/api/v1/websites/${id}`);
			actionMsg = 'Website deleted.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete website';
		}
	}

	async function retryWebsite(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${id}/retry`);
			actionMsg = 'Website retry initiated.';
			await loadWebsites();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to retry website';
		}
	}

	onMount(() => { void Promise.all([loadWebsites(), loadOptions()]); });

	onDestroy(() => {
		stopPolling();
	});
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Websites</h2>
		<button
			onclick={() => (showCreateForm = !showCreateForm)}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showCreateForm ? 'Cancel' : 'Create Website'}
		</button>
	</div>

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
	<TaskProgress bind:taskId={repairTaskId} storageKey="website-laravel-repair-task" onComplete={(task) => { repairing = false; repairTaskId = ''; if (task.status === 'completed') actionMsg = 'Laravel repair completed.'; }} onMissing={() => { repairing = false; }} />

	<!-- Create Form -->
	{#if showCreateForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">New Website</h3>
			{#if optionsError}
				<div class="mb-4 p-3 bg-red-900/50 border border-red-700 rounded text-sm text-red-300">
					{optionsError} <button class="underline cursor-pointer" onclick={loadOptions}>Retry</button>
				</div>
			{/if}
			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="create-domain" class="block text-sm text-gray-400 mb-1">Domain</label>
					<input
						id="create-domain"
						type="text"
						bind:value={createDomain}
						placeholder="example.com"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="create-app-type" class="block text-sm text-gray-400 mb-1">Template</label>
					<select
						id="create-app-type"
						value={selection.template}
						onchange={(event) => { selection.template = event.currentTarget.value; normalizeSelection(); }}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="static">Static HTML</option>
						<option value="php">Native PHP</option>
						<option value="codeigniter3">CodeIgniter 3</option>
						<option value="codeigniter4">CodeIgniter 4</option>
						<option value="laravel">Laravel</option>
						<option value="laravel-octane">Laravel Octane (FrankenPHP)</option>
					</select>
				</div>
				{#if selection.template !== 'static'}
					<div>
						<label for="create-php-version" class="block text-sm text-gray-400 mb-1">PHP Version</label>
						<select
							id="create-php-version"
							bind:value={selection.php_version}
							class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
						>
							{#each installedPHP as runtime}
								<option value={runtime.version}>{runtime.version}{runtime.running ? '' : ' (FPM stopped)'}</option>
							{/each}
						</select>
						{#if installedPHP.length === 0}
							<p class="mt-1 text-xs text-yellow-300">No PHP version is installed. <a class="underline" href="/php">Install PHP</a></p>
						{:else if installedPHP.find((runtime) => runtime.version === selection.php_version)?.running === false}
							<p class="mt-1 text-xs text-yellow-300">PHP {selection.php_version} FPM is stopped; provisioning will try to restart it.</p>
						{/if}
					</div>
				{/if}
				{#if selection.template === 'laravel' || selection.template === 'laravel-octane'}
					<div>
						<label for="framework-version" class="block text-sm text-gray-400 mb-1">Laravel Version</label>
						<select id="framework-version" value={selection.framework_version} onchange={(event) => { selection.framework_version = event.currentTarget.value; normalizeSelection(); }} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
							{#if selection.template === 'laravel-octane'}
								{#each laravelOctaneVersions as version}<option value={version}>Laravel {version}</option>{/each}
							{:else}
								{#each laravelVersions as version}<option value={version}>Laravel {version}</option>{/each}
							{/if}
						</select>
					</div>
					{#if selection.template === 'laravel'}
					<div>
						<label for="frontend-stack" class="block text-sm text-gray-400 mb-1">Frontend</label>
						<select id="frontend-stack" value={selection.frontend_stack} onchange={(event) => { selection.frontend_stack = event.currentTarget.value; normalizeSelection(); }} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
							<option value="blade">Blade</option><option value="inertia">Inertia</option><option value="livewire">Livewire</option>
						</select>
					</div>
					{#if selection.frontend_stack === 'inertia'}
						<div>
							<label for="inertia-adapter" class="block text-sm text-gray-400 mb-1">Inertia Adapter</label>
							<select id="inertia-adapter" value={selection.inertia_adapter} onchange={(event) => { selection.inertia_adapter = event.currentTarget.value; normalizeSelection(); }} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
								{#each options?.inertia_adapters || [] as adapter}<option value={adapter}>{adapter[0].toUpperCase() + adapter.slice(1)}</option>{/each}
							</select>
						</div>
					{/if}
					{#if selection.frontend_stack !== 'blade'}
						<div>
							<label for="project-variant" class="block text-sm text-gray-400 mb-1">Project</label>
							<select id="project-variant" value={selection.project_variant} onchange={(event) => { selection.project_variant = event.currentTarget.value; normalizeSelection(); }} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
								<option value="empty">Empty project</option><option value="starter-kit">Starter kit</option>
							</select>
						</div>
					{/if}
					{/if}
				{/if}
					<div>
						<label for="setup-mode" class="block text-sm text-gray-400 mb-1">Setup</label>
					<select id="setup-mode" value={selection.setup_mode} onchange={(event) => { selection.setup_mode = event.currentTarget.value; normalizeSelection(); }} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
						<option value="config-only">Nginx config only</option><option value="auto-install">Install framework automatically</option>
					</select>
				</div>
				<div>
					<label for="website-node-version" class="block text-sm text-gray-400 mb-1">Node.js (NVM per website)</label>
					<select id="website-node-version" bind:value={selection.node_version} class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm">
						<option value="" disabled={selection.setup_mode === 'auto-install' && combination?.prerequisites?.includes('node')}>None</option>
						{#each options?.node_versions || [] as version}<option value={version}>Node.js {version}{version === '24' ? ' (recommended)' : ''}</option>{/each}
					</select>
					<p class="mt-1 text-xs text-gray-400">{selection.setup_mode === 'config-only' ? 'Selection is saved only. Install the runtime later on the Node.js page.' : 'Node.js is installed under this website user; global Node.js is not required.'}</p>
				</div>
			</div>
			{#if combination}
				<div class="mt-4 rounded border border-gray-700 bg-gray-900/60 p-3 text-sm text-gray-300">
					<p>Document root: <code>{combination.document_root || '-'}</code></p>
					{#if !combination.enabled}<p class="mt-2 text-yellow-300">{combination.reason}</p>{/if}
					{#if combination.missing_dependencies?.length}
						<div class="mt-2 flex flex-wrap gap-3">
							{#each combination.missing_dependencies as dependency}
								<a class="text-blue-400 underline" href={dependency.manage_url}>Install {dependency.name}</a>
							{/each}
						</div>
					{/if}
				</div>
			{/if}
			<div class="mt-4">
				<button
					onclick={createWebsite}
					disabled={creating || !createDomain.trim() || !options || !combination?.enabled || (selection.template !== 'static' && !selection.php_version)}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Creating...' : 'Create'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Websites Table -->
	{#if loading}
		<div class="text-gray-400">Loading websites...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if websites.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No websites configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Domain</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Template</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">PHP Version</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each websites as website}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3">
									<a href="/websites/{website.id}" class="text-sm text-blue-400 hover:text-blue-300 font-medium hover:underline">
										{website.domain}
									</a>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300 capitalize">
									{website.framework && website.framework !== 'none' ? website.framework : website.app_type}
									{#if website.framework_version}<span class="text-gray-500"> {website.framework_version}</span>{/if}
								</td>
								<td class="px-4 py-3 text-sm text-gray-300">{website.app_type === 'static' ? '-' : website.php_version}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(website.status)}">
										{website.status}
									</span>
									{#if pendingStatuses.includes(website.status) && website.provision_stage}
										<p class="mt-1 text-xs text-yellow-300">{website.provision_stage}</p>
									{/if}
									{#if website.status === 'failed' && website.error_message}
										<p class="mt-1 text-xs text-red-400">{website.error_message}</p>
									{/if}
									{#if website.status === 'failed' && website.provision_log}
										<details class="mt-2 max-w-md text-left">
											<summary class="cursor-pointer text-xs text-gray-400">Provisioning log</summary>
											<pre class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap rounded bg-gray-950 p-2 text-xs text-gray-300">{website.provision_log}</pre>
										</details>
									{/if}
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if website.status === 'active' && website.framework === 'laravel' && website.setup_mode === 'auto-install'}
											{#if repairConfirmId === website.id}
												<span class="max-w-xs text-xs text-yellow-400">Create missing SQLite files and run pending SQLite migrations? Existing data and app key are preserved.</span>
												<button class="px-2 py-1 text-xs bg-blue-600 text-white rounded" onclick={() => repairLaravel(website.id)} disabled={repairBusy}>Confirm repair</button>
												<button class="text-xs text-gray-400" onclick={() => repairConfirmId = null}>Cancel</button>
											{:else}
												<button class="px-2 py-1 text-xs bg-blue-600 text-white rounded disabled:opacity-50" disabled={repairBusy} onclick={() => repairConfirmId = website.id}>Repair Laravel</button>
											{/if}
										{/if}
										{#if website.status === 'failed'}
											<button
												onclick={() => retryWebsite(website.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Retry
											</button>
										{/if}
										{#if website.status === 'active'}
											<button
												onclick={() => suspendWebsite(website.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Suspend
											</button>
										{/if}
										{#if website.status === 'suspended' || website.status === 'disabled'}
											<button
												onclick={() => enableWebsite(website.id)}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Enable
											</button>
										{/if}
										{#if deleteConfirmId === website.id}
											<span class="text-xs text-red-400">Delete website, SSL, and all files?</span>
											<button
												onclick={() => deleteWebsite(website.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes, Delete
											</button>
											<button
												onclick={() => (deleteConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteConfirmId = website.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
											</button>
										{/if}
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
</div>
