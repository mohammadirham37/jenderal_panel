<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	interface NodeVersion {
		version: string;
		installed: boolean;
		lts: boolean;
	}

	interface NodeApp {
		id: string;
		website_id: string;
		website_domain?: string;
		domain: string;
		node_version: string;
		package_manager: string;
		build_command: string;
		start_command: string;
		port: number;
		status: string;
		created_at: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let versions = $state<NodeVersion[]>([]);
	let apps = $state<NodeApp[]>([]);
	let websites = $state<Website[]>([]);
	let loadingVersions = $state(true);
	let loadingApps = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let installingVersion = $state<string | null>(null);
	let currentTaskId = $state('');

	// Create form
	let showCreateForm = $state(false);
	let createWebsiteId = $state('');
	let createNodeVersion = $state('');
	let createPackageMgr = $state('npm');
	let createBuildCmd = $state('');
	let createStartCmd = $state('');
	let createPort = $state(3000);
	let creating = $state(false);

	// Delete confirm
	let deleteConfirmId = $state<string | null>(null);

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'running':
				return 'bg-green-900/50 text-green-400';
			case 'stopped':
				return 'bg-gray-700 text-gray-400';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			case 'starting':
			case 'building':
			case 'installing':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	async function loadVersions() {
		loadingVersions = true;
		try {
			versions = (await api.get<NodeVersion[]>('/api/v1/nodejs/versions')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load Node.js versions';
		} finally {
			loadingVersions = false;
		}
	}

	async function loadApps() {
		loadingApps = true;
		try {
			apps = (await api.get<NodeApp[]>('/api/v1/nodejs/apps')) || [];
		} catch (err) {
			if (!error) {
				error = err instanceof Error ? err.message : 'Failed to load Node.js apps';
			}
		} finally {
			loadingApps = false;
		}
	}

	async function loadWebsites() {
		try {
			websites = (await api.get<Website[]>('/api/v1/websites')) || [];
		} catch {
			// Non-critical
		}
	}

	async function installVersion(version: string) {
		installingVersion = version;
		actionMsg = '';
		actionError = '';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/nodejs/versions/install', { version });
			currentTaskId = result.task_id;
			actionMsg = `Node.js ${version} installation started.`;
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to install Node.js version';
		} finally {
			installingVersion = null;
		}
	}

	async function createApp() {
		if (!createWebsiteId || !createNodeVersion || !createStartCmd.trim()) return;
		creating = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/nodejs/apps', {
				website_id: createWebsiteId,
				node_version: createNodeVersion,
				package_manager: createPackageMgr,
				build_command: createBuildCmd.trim(),
				start_command: createStartCmd.trim(),
				port: createPort
			});
			actionMsg = 'Node.js app created successfully.';
			showCreateForm = false;
			createWebsiteId = '';
			createNodeVersion = '';
			createPackageMgr = 'npm';
			createBuildCmd = '';
			createStartCmd = '';
			createPort = 3000;
			await loadApps();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create Node.js app';
		} finally {
			creating = false;
		}
	}

	async function startApp(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/nodejs/apps/${id}/start`);
			actionMsg = 'App started.';
			await loadApps();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to start app';
		}
	}

	async function stopApp(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/nodejs/apps/${id}/stop`);
			actionMsg = 'App stopped.';
			await loadApps();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to stop app';
		}
	}

	async function restartApp(id: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/nodejs/apps/${id}/restart`);
			actionMsg = 'App restarted.';
			await loadApps();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to restart app';
		}
	}

	async function deleteApp(id: string) {
		deleteConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/nodejs/apps/${id}`);
			actionMsg = 'Node.js app deleted.';
			await loadApps();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete app';
		}
	}

	function openCreateForm() {
		showCreateForm = true;
		loadWebsites();
	}

	onMount(() => {
		loadVersions();
		loadApps();
		loadWebsites();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Node.js</h2>

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

	<!-- Node Versions Section -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">Node.js Versions</h3>
		{#if loadingVersions}
			<div class="text-gray-400 text-sm">Loading versions...</div>
		{:else if versions.length === 0}
			<div class="text-gray-400 text-sm">No Node.js versions available. Install one to get started.</div>
		{:else}
			<div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
				{#each versions as ver}
					<div class="flex items-center justify-between bg-gray-900 rounded-lg px-4 py-3 border border-gray-700">
						<div>
							<span class="text-sm text-white font-medium">{ver.version}</span>
							{#if ver.lts}
								<span class="ml-1.5 text-xs bg-blue-900/50 text-blue-400 px-1.5 py-0.5 rounded">LTS</span>
							{/if}
						</div>
						{#if ver.installed}
							<span class="text-xs bg-green-900/50 text-green-400 px-2 py-0.5 rounded font-medium">Installed</span>
						{:else}
							<button
								onclick={() => installVersion(ver.version)}
								disabled={installingVersion !== null}
								class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
							>
								{installingVersion === ver.version ? 'Installing...' : 'Install'}
							</button>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<!-- Node.js Apps Section -->
	<div class="flex items-center justify-between">
		<h3 class="text-lg font-semibold text-white">Applications</h3>
		<button
			onclick={() => (showCreateForm ? (showCreateForm = false) : openCreateForm())}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showCreateForm ? 'Cancel' : 'Create App'}
		</button>
	</div>

	<!-- Create App Form -->
	{#if showCreateForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">New Node.js Application</h3>
			<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
				<div>
					<label for="node-website" class="block text-sm text-gray-400 mb-1">Website</label>
					<select
						id="node-website"
						bind:value={createWebsiteId}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="">Select a website...</option>
						{#each websites as website}
							<option value={website.id}>{website.domain}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="node-version" class="block text-sm text-gray-400 mb-1">Node Version</label>
					<select
						id="node-version"
						bind:value={createNodeVersion}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="">Select version...</option>
						{#each versions.filter((v) => v.installed) as ver}
							<option value={ver.version}>{ver.version}{ver.lts ? ' (LTS)' : ''}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="node-pkg" class="block text-sm text-gray-400 mb-1">Package Manager</label>
					<select
						id="node-pkg"
						bind:value={createPackageMgr}
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="npm">npm</option>
						<option value="yarn">yarn</option>
						<option value="pnpm">pnpm</option>
					</select>
				</div>
				<div>
					<label for="node-build" class="block text-sm text-gray-400 mb-1">Build Command</label>
					<input
						id="node-build"
						type="text"
						bind:value={createBuildCmd}
						placeholder="npm run build"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="node-start" class="block text-sm text-gray-400 mb-1">Start Command</label>
					<input
						id="node-start"
						type="text"
						bind:value={createStartCmd}
						placeholder="npm start"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="node-port" class="block text-sm text-gray-400 mb-1">Port</label>
					<input
						id="node-port"
						type="number"
						bind:value={createPort}
						min="1024"
						max="65535"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			</div>
			<div class="mt-4">
				<button
					onclick={createApp}
					disabled={creating || !createWebsiteId || !createNodeVersion || !createStartCmd.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creating ? 'Creating...' : 'Create'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Apps Table -->
	{#if loadingApps}
		<div class="text-gray-400">Loading Node.js apps...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if apps.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No Node.js applications configured yet.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Domain</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Node Version</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Port</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each apps as app}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white font-medium">{app.domain || app.website_domain || '-'}</td>
								<td class="px-4 py-3 text-sm text-gray-300">{app.node_version}</td>
								<td class="px-4 py-3 text-sm text-gray-300">{app.port}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(app.status)}">
										{app.status}
									</span>
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex items-center justify-end gap-2">
										{#if app.status === 'stopped' || app.status === 'failed'}
											<button
												onclick={() => startApp(app.id)}
												class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Start
											</button>
										{/if}
										{#if app.status === 'running'}
											<button
												onclick={() => stopApp(app.id)}
												class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Stop
											</button>
										{/if}
										{#if app.status === 'running' || app.status === 'failed'}
											<button
												onclick={() => restartApp(app.id)}
												class="px-2.5 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Restart
											</button>
										{/if}
										{#if deleteConfirmId === app.id}
											<span class="text-xs text-red-400">Delete?</span>
											<button
												onclick={() => deleteApp(app.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteConfirmId = app.id)}
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

	<TaskProgress taskId={currentTaskId} onComplete={() => { currentTaskId = ''; loadVersions(); }} />
</div>
