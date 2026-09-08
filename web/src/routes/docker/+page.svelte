<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

	// ── Types ──────────────────────────────────────────────────────
	interface DockerStatus {
		installed: boolean;
		running: boolean;
		version: string;
		containers_count: number;
		images_count: number;
	}

	interface Container {
		id: string;
		name: string;
		image: string;
		status: string;
		state: string;
		ports: string;
	}

	interface DockerImage {
		id: string;
		repository: string;
		tag: string;
		size: string;
		created: string;
	}

	interface Volume {
		name: string;
		driver: string;
		mountpoint: string;
	}

	interface Network {
		id: string;
		name: string;
		driver: string;
		scope: string;
	}

	interface ComposeStatus {
		status: string;
		services: string[];
	}

	// ── State ──────────────────────────────────────────────────────
	let activeTab = $state<'containers' | 'images' | 'volumes' | 'networks' | 'compose'>('containers');

	let dockerStatus = $state<DockerStatus | null>(null);
	let containers = $state<Container[]>([]);
	let images = $state<DockerImage[]>([]);
	let volumes = $state<Volume[]>([]);
	let networks = $state<Network[]>([]);

	let loadingStatus = $state(true);
	let loadingContainers = $state(true);
	let loadingImages = $state(true);
	let loadingVolumes = $state(true);
	let loadingNetworks = $state(true);

	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let actionInProgress = $state<string | null>(null);
	let currentTaskId = $state('');
	let installInProgress = $derived(actionInProgress !== null || !!currentTaskId);

	// Containers
	let showAll = $state(false);
	let expandedLogs = $state<string | null>(null);
	let containerLogs = $state<Record<string, string>>({});
	let loadingLogs = $state<string | null>(null);
	let deleteContainerConfirmId = $state<string | null>(null);

	// Images
	let pullImageName = $state('');
	let pullingImage = $state(false);
	let deleteImageConfirmId = $state<string | null>(null);

	// Volumes
	let newVolumeName = $state('');
	let creatingVolume = $state(false);
	let deleteVolumeConfirmId = $state<string | null>(null);

	// Networks
	let newNetworkName = $state('');
	let creatingNetwork = $state(false);
	let deleteNetworkConfirmId = $state<string | null>(null);

	// Compose
	let composePath = $state('');
	let composeStatus = $state<ComposeStatus | null>(null);
	let composeActionInProgress = $state<string | null>(null);

	// ── Helpers ────────────────────────────────────────────────────
	function shortId(id: string): string {
		return id.substring(0, 12);
	}

	function stateBadgeClass(state: string): string {
		switch (state?.toLowerCase()) {
			case 'running':
				return 'bg-green-900/50 text-green-400';
			case 'exited':
			case 'dead':
				return 'bg-gray-700 text-gray-400';
			case 'created':
			case 'paused':
				return 'bg-yellow-900/50 text-yellow-400';
			case 'restarting':
				return 'bg-blue-900/50 text-blue-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function stateDotClass(state: string): string {
		switch (state?.toLowerCase()) {
			case 'running':
				return 'bg-green-400';
			case 'exited':
			case 'dead':
				return 'bg-gray-400';
			case 'created':
			case 'paused':
				return 'bg-yellow-400';
			case 'restarting':
				return 'bg-blue-400';
			default:
				return 'bg-gray-400';
		}
	}

	function isDefaultNetwork(name: string): boolean {
		return ['bridge', 'host', 'none'].includes(name);
	}

	// ── Loaders ───────────────────────────────────────────────────
	async function loadDockerStatus() {
		loadingStatus = true;
		try {
			dockerStatus = await api.get<DockerStatus>('/api/v1/docker/status');
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load Docker status';
		} finally {
			loadingStatus = false;
		}
	}

	async function loadContainers() {
		loadingContainers = true;
		try {
			containers = (await api.get<Container[]>(`/api/v1/docker/containers?all=${showAll}`)) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load containers';
		} finally {
			loadingContainers = false;
		}
	}

	async function loadImages() {
		loadingImages = true;
		try {
			images = (await api.get<DockerImage[]>('/api/v1/docker/images')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load images';
		} finally {
			loadingImages = false;
		}
	}

	async function loadVolumes() {
		loadingVolumes = true;
		try {
			volumes = (await api.get<Volume[]>('/api/v1/docker/volumes')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load volumes';
		} finally {
			loadingVolumes = false;
		}
	}

	async function loadNetworks() {
		loadingNetworks = true;
		try {
			networks = (await api.get<Network[]>('/api/v1/docker/networks')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load networks';
		} finally {
			loadingNetworks = false;
		}
	}

	// ── Docker daemon actions ─────────────────────────────────────
	async function installDocker() {
		actionMsg = '';
		actionError = '';
		actionInProgress = 'install';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/docker/install');
			currentTaskId = result.task_id;
			actionMsg = 'Docker installation started.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to install Docker';
			actionInProgress = null;
		}
	}

	function onInstallComplete() {
		actionInProgress = null;
		currentTaskId = '';
		loadDockerStatus();
	}

	async function dockerDaemonAction(action: 'start' | 'stop' | 'restart') {
		actionMsg = '';
		actionError = '';
		actionInProgress = `daemon-${action}`;
		try {
			await api.post(`/api/v1/docker/${action}`);
			actionMsg = `Docker ${action}ed successfully.`;
			await loadDockerStatus();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to ${action} Docker`;
		} finally {
			actionInProgress = null;
		}
	}

	// ── Container actions ─────────────────────────────────────────
	async function containerAction(id: string, action: 'start' | 'stop' | 'restart') {
		actionMsg = '';
		actionError = '';
		actionInProgress = `container-${action}-${id}`;
		try {
			await api.post(`/api/v1/docker/containers/${id}/${action}`);
			actionMsg = `Container ${shortId(id)} ${action}ed successfully.`;
			await loadContainers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to ${action} container`;
		} finally {
			actionInProgress = null;
		}
	}

	async function removeContainer(id: string) {
		deleteContainerConfirmId = null;
		actionMsg = '';
		actionError = '';
		actionInProgress = `container-remove-${id}`;
		try {
			await api.del(`/api/v1/docker/containers/${id}`);
			actionMsg = `Container ${shortId(id)} removed.`;
			await loadContainers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to remove container';
		} finally {
			actionInProgress = null;
		}
	}

	async function fetchLogs(id: string) {
		if (expandedLogs === id) {
			expandedLogs = null;
			return;
		}
		expandedLogs = id;
		loadingLogs = id;
		try {
			const data = await api.get<{ logs: string }>(`/api/v1/docker/containers/${id}/logs?lines=100`);
			containerLogs[id] = data?.logs || 'No logs available.';
		} catch (err) {
			containerLogs[id] = err instanceof Error ? err.message : 'Failed to fetch logs';
		} finally {
			loadingLogs = null;
		}
	}

	// ── Image actions ─────────────────────────────────────────────
	async function pullImage() {
		if (!pullImageName.trim()) return;
		pullingImage = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/docker/images/pull', { image: pullImageName.trim() });
			actionMsg = `Image "${pullImageName.trim()}" pull started.`;
			pullImageName = '';
			await loadImages();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to pull image';
		} finally {
			pullingImage = false;
		}
	}

	async function deleteImage(id: string) {
		deleteImageConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/docker/images/${id}`);
			actionMsg = 'Image deleted.';
			await loadImages();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete image';
		}
	}

	// ── Volume actions ────────────────────────────────────────────
	async function createVolume() {
		if (!newVolumeName.trim()) return;
		creatingVolume = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/docker/volumes', { name: newVolumeName.trim() });
			actionMsg = `Volume "${newVolumeName.trim()}" created.`;
			newVolumeName = '';
			await loadVolumes();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create volume';
		} finally {
			creatingVolume = false;
		}
	}

	async function deleteVolume(name: string) {
		deleteVolumeConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/docker/volumes/${encodeURIComponent(name)}`);
			actionMsg = `Volume "${name}" deleted.`;
			await loadVolumes();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete volume';
		}
	}

	// ── Network actions ───────────────────────────────────────────
	async function createNetwork() {
		if (!newNetworkName.trim()) return;
		creatingNetwork = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/docker/networks', { name: newNetworkName.trim() });
			actionMsg = `Network "${newNetworkName.trim()}" created.`;
			newNetworkName = '';
			await loadNetworks();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create network';
		} finally {
			creatingNetwork = false;
		}
	}

	async function deleteNetwork(id: string, name: string) {
		deleteNetworkConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/docker/networks/${id}`);
			actionMsg = `Network "${name}" deleted.`;
			await loadNetworks();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete network';
		}
	}

	// ── Compose actions ───────────────────────────────────────────
	async function composeUp() {
		if (!composePath.trim()) return;
		composeActionInProgress = 'up';
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/docker/compose/up', { path: composePath.trim() });
			actionMsg = 'Docker Compose services started.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to run docker compose up';
		} finally {
			composeActionInProgress = null;
		}
	}

	async function composeDown() {
		if (!composePath.trim()) return;
		composeActionInProgress = 'down';
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/docker/compose/down', { path: composePath.trim() });
			actionMsg = 'Docker Compose services stopped.';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to run docker compose down';
		} finally {
			composeActionInProgress = null;
		}
	}

	// ── Reactive: reload containers when toggle changes ───────────
	$effect(() => {
		// Track showAll to reload containers
		showAll;
		loadContainers();
	});

	// ── Lifecycle ─────────────────────────────────────────────────
	onMount(() => {
		loadDockerStatus();
		loadImages();
		loadVolumes();
		loadNetworks();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Docker</h2>

	<!-- Feedback messages -->
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

	<!-- ═══════════════════════════ STATUS CARD ═══════════════════════════ -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">Docker Status</h3>
		{#if loadingStatus}
			<div class="text-gray-400 text-sm">Loading Docker status...</div>
		{:else if !dockerStatus}
			<div class="text-gray-400 text-sm">Unable to retrieve Docker status.</div>
		{:else}
			<div class="flex flex-wrap items-center gap-6 mb-4">
				<!-- Badges -->
				<div class="flex items-center gap-3">
					{#if dockerStatus.installed}
						<span class="text-xs bg-green-900/50 text-green-400 px-2.5 py-1 rounded font-medium">Installed</span>
					{:else}
						<span class="text-xs bg-gray-700 text-gray-400 px-2.5 py-1 rounded font-medium">Not Installed</span>
					{/if}
					{#if dockerStatus.installed}
						<span class="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded font-medium {dockerStatus.running ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
							<span class="w-1.5 h-1.5 rounded-full {dockerStatus.running ? 'bg-green-400' : 'bg-red-400'}"></span>
							{dockerStatus.running ? 'Running' : 'Stopped'}
						</span>
					{/if}
				</div>

				<!-- Info -->
				{#if dockerStatus.version}
					<div class="text-xs text-gray-400">Version: <span class="text-gray-300">{dockerStatus.version}</span></div>
				{/if}
				{#if dockerStatus.installed}
					<div class="text-xs text-gray-400">Containers: <span class="text-gray-300">{dockerStatus.containers_count ?? 0}</span></div>
					<div class="text-xs text-gray-400">Images: <span class="text-gray-300">{dockerStatus.images_count ?? 0}</span></div>
				{/if}
			</div>

			<!-- Actions -->
			<div class="flex items-center gap-2">
				{#if !dockerStatus.installed}
					<button
						onclick={installDocker}
						disabled={installInProgress}
						class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'install' ? 'Installing...' : 'Install Docker'}
					</button>
				{:else}
					{#if !dockerStatus.running}
						<button
							onclick={() => dockerDaemonAction('start')}
							disabled={actionInProgress !== null}
							class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
						>
							{actionInProgress === 'daemon-start' ? '...' : 'Start'}
						</button>
					{:else}
						<button
							onclick={() => dockerDaemonAction('stop')}
							disabled={actionInProgress !== null}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
						>
							{actionInProgress === 'daemon-stop' ? '...' : 'Stop'}
						</button>
					{/if}
					<button
						onclick={() => dockerDaemonAction('restart')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'daemon-restart' ? '...' : 'Restart'}
					</button>
				{/if}
			</div>
		{/if}
	</div>

	<!-- ═══════════════════════════ TABS ═══════════════════════════ -->
	<div class="border-b border-gray-700">
		<nav class="flex gap-0 -mb-px">
			{#each [
				{ key: 'containers', label: 'Containers' },
				{ key: 'images', label: 'Images' },
				{ key: 'volumes', label: 'Volumes' },
				{ key: 'networks', label: 'Networks' },
				{ key: 'compose', label: 'Compose' }
			] as tab}
				<button
					onclick={() => (activeTab = tab.key as typeof activeTab)}
					class="px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer
					{activeTab === tab.key
						? 'border-blue-500 text-blue-400'
						: 'border-transparent text-gray-400 hover:text-gray-200 hover:border-gray-600'}"
				>
					{tab.label}
				</button>
			{/each}
		</nav>
	</div>

	<!-- ═══════════════════════════ CONTAINERS TAB ═══════════════════════════ -->
	{#if activeTab === 'containers'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-4">
				<h3 class="text-lg font-semibold text-white">Containers</h3>
				<label class="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
					<input
						type="checkbox"
						bind:checked={showAll}
						class="rounded border-gray-600 bg-gray-900 text-blue-600 focus:ring-blue-500 cursor-pointer"
					/>
					Show all containers
				</label>
			</div>

			{#if loadingContainers}
				<div class="text-gray-400 text-sm">Loading containers...</div>
			{:else if containers.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">No containers found.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">ID</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Image</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">State</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Ports</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each containers as container}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-gray-300 font-mono">{shortId(container.id)}</td>
									<td class="px-4 py-3 text-sm text-white font-medium">{container.name}</td>
									<td class="px-4 py-3 text-sm text-gray-300 font-mono">{container.image}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{container.status}</td>
									<td class="px-4 py-3">
										<span class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium {stateBadgeClass(container.state)}">
											<span class="w-1.5 h-1.5 rounded-full {stateDotClass(container.state)}"></span>
											{container.state}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{container.ports || '-'}</td>
									<td class="px-4 py-3 text-right">
										{#if deleteContainerConfirmId === container.id}
											<span class="text-xs text-red-400 mr-1">Remove?</span>
											<button
												onclick={() => removeContainer(container.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteContainerConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												Cancel
											</button>
										{:else}
											<div class="flex items-center justify-end gap-1.5">
												{#if container.state?.toLowerCase() !== 'running'}
													<button
														onclick={() => containerAction(container.id, 'start')}
														disabled={actionInProgress !== null}
														class="px-2 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
													>
														{actionInProgress === `container-start-${container.id}` ? '...' : 'Start'}
													</button>
												{:else}
													<button
														onclick={() => containerAction(container.id, 'stop')}
														disabled={actionInProgress !== null}
														class="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
													>
														{actionInProgress === `container-stop-${container.id}` ? '...' : 'Stop'}
													</button>
												{/if}
												<button
													onclick={() => containerAction(container.id, 'restart')}
													disabled={actionInProgress !== null}
													class="px-2 py-1 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{actionInProgress === `container-restart-${container.id}` ? '...' : 'Restart'}
												</button>
												<button
													onclick={() => fetchLogs(container.id)}
													class="px-2 py-1 bg-indigo-600 hover:bg-indigo-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Logs
												</button>
												<button
													onclick={() => (deleteContainerConfirmId = container.id)}
													disabled={actionInProgress !== null}
													class="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Remove
												</button>
											</div>
										{/if}
									</td>
								</tr>
								<!-- Expandable logs -->
								{#if expandedLogs === container.id}
									<tr>
										<td colspan="7" class="px-4 py-3">
											<div class="bg-gray-900 rounded border border-gray-700 p-4">
												<div class="flex items-center justify-between mb-2">
													<span class="text-xs text-gray-400 font-medium uppercase tracking-wider">Container Logs - {container.name}</span>
													<button
														onclick={() => (expandedLogs = null)}
														class="text-xs text-gray-500 hover:text-gray-300 cursor-pointer"
													>
														Close
													</button>
												</div>
												{#if loadingLogs === container.id}
													<div class="text-gray-400 text-sm">Loading logs...</div>
												{:else}
													<pre class="text-xs text-gray-300 font-mono whitespace-pre-wrap max-h-80 overflow-y-auto">{containerLogs[container.id] || 'No logs available.'}</pre>
												{/if}
											</div>
										</td>
									</tr>
								{/if}
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- ═══════════════════════════ IMAGES TAB ═══════════════════════════ -->
	{#if activeTab === 'images'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Images</h3>

			<!-- Pull image form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="pull-image" class="block text-sm text-gray-400 mb-1">Pull Image</label>
					<input
						id="pull-image"
						type="text"
						bind:value={pullImageName}
						placeholder="nginx:latest"
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-64"
						onkeydown={(e) => { if (e.key === 'Enter') pullImage(); }}
					/>
				</div>
				<button
					onclick={pullImage}
					disabled={pullingImage || !pullImageName.trim()}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{pullingImage ? 'Pulling...' : 'Pull'}
				</button>
			</div>

			{#if loadingImages}
				<div class="text-gray-400 text-sm">Loading images...</div>
			{:else if images.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">No images found.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Repository</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Tag</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">ID</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Size</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Created</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each images as img}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-medium">{img.repository || '<none>'}</td>
									<td class="px-4 py-3 text-sm text-gray-300">{img.tag || '<none>'}</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{shortId(img.id)}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{img.size}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{img.created}</td>
									<td class="px-4 py-3 text-right">
										{#if deleteImageConfirmId === img.id}
											<span class="text-xs text-red-400 mr-1">Delete?</span>
											<button
												onclick={() => deleteImage(img.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteImageConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteImageConfirmId = img.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
											</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- ═══════════════════════════ VOLUMES TAB ═══════════════════════════ -->
	{#if activeTab === 'volumes'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Volumes</h3>

			<!-- Create volume form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="vol-name" class="block text-sm text-gray-400 mb-1">Volume Name</label>
					<input
						id="vol-name"
						type="text"
						bind:value={newVolumeName}
						placeholder="my_volume"
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-56"
						onkeydown={(e) => { if (e.key === 'Enter') createVolume(); }}
					/>
				</div>
				<button
					onclick={createVolume}
					disabled={creatingVolume || !newVolumeName.trim()}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creatingVolume ? 'Creating...' : 'Create'}
				</button>
			</div>

			{#if loadingVolumes}
				<div class="text-gray-400 text-sm">Loading volumes...</div>
			{:else if volumes.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">No volumes found.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Driver</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Mountpoint</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each volumes as vol}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-medium font-mono">{vol.name}</td>
									<td class="px-4 py-3 text-sm text-gray-300">{vol.driver}</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono truncate max-w-xs" title={vol.mountpoint}>{vol.mountpoint}</td>
									<td class="px-4 py-3 text-right">
										{#if deleteVolumeConfirmId === vol.name}
											<span class="text-xs text-red-400 mr-1">Delete?</span>
											<button
												onclick={() => deleteVolume(vol.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteVolumeConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteVolumeConfirmId = vol.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
											</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- ═══════════════════════════ NETWORKS TAB ═══════════════════════════ -->
	{#if activeTab === 'networks'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Networks</h3>

			<!-- Create network form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="net-name" class="block text-sm text-gray-400 mb-1">Network Name</label>
					<input
						id="net-name"
						type="text"
						bind:value={newNetworkName}
						placeholder="my_network"
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-56"
						onkeydown={(e) => { if (e.key === 'Enter') createNetwork(); }}
					/>
				</div>
				<button
					onclick={createNetwork}
					disabled={creatingNetwork || !newNetworkName.trim()}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{creatingNetwork ? 'Creating...' : 'Create'}
				</button>
			</div>

			{#if loadingNetworks}
				<div class="text-gray-400 text-sm">Loading networks...</div>
			{:else if networks.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">No networks found.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">ID</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Driver</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Scope</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each networks as net}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-medium">{net.name}</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{shortId(net.id)}</td>
									<td class="px-4 py-3 text-sm text-gray-300">{net.driver}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{net.scope}</td>
									<td class="px-4 py-3 text-right">
										{#if isDefaultNetwork(net.name)}
											<span class="text-xs text-gray-500 italic">default</span>
										{:else if deleteNetworkConfirmId === net.id}
											<span class="text-xs text-red-400 mr-1">Delete?</span>
											<button
												onclick={() => deleteNetwork(net.id, net.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Yes
											</button>
											<button
												onclick={() => (deleteNetworkConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												Cancel
											</button>
										{:else}
											<button
												onclick={() => (deleteNetworkConfirmId = net.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												Delete
											</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- ═══════════════════════════ COMPOSE TAB ═══════════════════════════ -->
	{#if activeTab === 'compose'}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">Docker Compose</h3>

			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div class="flex-1 min-w-64">
					<label for="compose-path" class="block text-sm text-gray-400 mb-1">Compose File Path</label>
					<input
						id="compose-path"
						type="text"
						bind:value={composePath}
						placeholder="/path/to/docker-compose.yml"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 font-mono"
					/>
				</div>
				<div class="flex items-center gap-2">
					<button
						onclick={composeUp}
						disabled={composeActionInProgress !== null || !composePath.trim()}
						class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{composeActionInProgress === 'up' ? 'Starting...' : 'Up'}
					</button>
					<button
						onclick={composeDown}
						disabled={composeActionInProgress !== null || !composePath.trim()}
						class="px-4 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{composeActionInProgress === 'down' ? 'Stopping...' : 'Down'}
					</button>
				</div>
			</div>

			{#if composeStatus}
				<div class="bg-gray-900 rounded border border-gray-700 p-4">
					<div class="text-xs text-gray-400 font-medium uppercase tracking-wider mb-2">Compose Status</div>
					<div class="text-sm text-gray-300">{composeStatus.status}</div>
					{#if composeStatus.services && composeStatus.services.length > 0}
						<div class="mt-2 text-xs text-gray-400">
							Services: <span class="text-gray-300">{composeStatus.services.join(', ')}</span>
						</div>
					{/if}
				</div>
			{:else}
				<div class="text-gray-500 text-sm">Enter a docker-compose.yml path and use Up/Down to manage services.</div>
			{/if}
		</div>
	{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_docker_task" onComplete={onInstallComplete} />
</div>
