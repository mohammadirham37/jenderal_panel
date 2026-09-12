<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

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

	// Translation key maps for parameterised start/stop/restart actions
	const DAEMON_DONE: Record<'start' | 'stop' | 'restart', string> = {
		start: 'dk.daemonStarted',
		stop: 'dk.daemonStopped',
		restart: 'dk.daemonRestarted'
	};
	const DAEMON_FAILED: Record<'start' | 'stop' | 'restart', string> = {
		start: 'dk.daemonStartFailed',
		stop: 'dk.daemonStopFailed',
		restart: 'dk.daemonRestartFailed'
	};
	const CONTAINER_DONE: Record<'start' | 'stop' | 'restart', string> = {
		start: 'dk.containerStarted',
		stop: 'dk.containerStopped',
		restart: 'dk.containerRestarted'
	};
	const CONTAINER_FAILED: Record<'start' | 'stop' | 'restart', string> = {
		start: 'dk.containerStartFailed',
		stop: 'dk.containerStopFailed',
		restart: 'dk.containerRestartFailed'
	};

	// ── Loaders ───────────────────────────────────────────────────
	async function loadDockerStatus() {
		loadingStatus = true;
		try {
			dockerStatus = await api.get<DockerStatus>('/api/v1/docker/status');
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'dk.loadStatusFailed');
		} finally {
			loadingStatus = false;
		}
	}

	async function loadContainers() {
		loadingContainers = true;
		try {
			containers = (await api.get<Container[]>(`/api/v1/docker/containers?all=${showAll}`)) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'dk.loadContainersFailed');
		} finally {
			loadingContainers = false;
		}
	}

	async function loadImages() {
		loadingImages = true;
		try {
			images = (await api.get<DockerImage[]>('/api/v1/docker/images')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'dk.loadImagesFailed');
		} finally {
			loadingImages = false;
		}
	}

	async function loadVolumes() {
		loadingVolumes = true;
		try {
			volumes = (await api.get<Volume[]>('/api/v1/docker/volumes')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'dk.loadVolumesFailed');
		} finally {
			loadingVolumes = false;
		}
	}

	async function loadNetworks() {
		loadingNetworks = true;
		try {
			networks = (await api.get<Network[]>('/api/v1/docker/networks')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'dk.loadNetworksFailed');
		} finally {
			loadingNetworks = false;
		}
	}

	// ── Docker daemon actions ─────────────────────────────────────
	async function installDocker() {
		actionInProgress = 'install';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/docker/install');
			currentTaskId = result.task_id;
			toast.success(translate($language, 'dk.installStarted'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.installFailed'));
			actionInProgress = null;
		}
	}

	function onInstallComplete() {
		actionInProgress = null;
		currentTaskId = '';
		loadDockerStatus();
	}

	async function dockerDaemonAction(action: 'start' | 'stop' | 'restart') {
		actionInProgress = `daemon-${action}`;
		try {
			await api.post(`/api/v1/docker/${action}`);
			toast.success(translate($language, DAEMON_DONE[action]));
			await loadDockerStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, DAEMON_FAILED[action]));
		} finally {
			actionInProgress = null;
		}
	}

	// ── Container actions ─────────────────────────────────────────
	async function containerAction(id: string, action: 'start' | 'stop' | 'restart') {
		actionInProgress = `container-${action}-${id}`;
		try {
			await api.post(`/api/v1/docker/containers/${id}/${action}`);
			toast.success(translate($language, CONTAINER_DONE[action]).replace('{id}', shortId(id)));
			await loadContainers();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, CONTAINER_FAILED[action]));
		} finally {
			actionInProgress = null;
		}
	}

	async function removeContainer(id: string) {
		deleteContainerConfirmId = null;
		actionInProgress = `container-remove-${id}`;
		try {
			await api.del(`/api/v1/docker/containers/${id}`);
			toast.success(translate($language, 'dk.containerRemoved').replace('{id}', shortId(id)));
			await loadContainers();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.containerRemoveFailed'));
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
			containerLogs[id] = data?.logs || translate($language, 'dk.noLogs');
		} catch (err) {
			containerLogs[id] = err instanceof Error ? err.message : translate($language, 'dk.fetchLogsFailed');
		} finally {
			loadingLogs = null;
		}
	}

	// ── Image actions ─────────────────────────────────────────────
	async function pullImage() {
		if (!pullImageName.trim()) return;
		pullingImage = true;
		try {
			await api.post('/api/v1/docker/images/pull', { image: pullImageName.trim() });
			toast.success(translate($language, 'dk.imagePullStarted').replace('{name}', pullImageName.trim()));
			pullImageName = '';
			await loadImages();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.imagePullFailed'));
		} finally {
			pullingImage = false;
		}
	}

	async function deleteImage(id: string) {
		deleteImageConfirmId = null;
		try {
			await api.del(`/api/v1/docker/images/${id}`);
			toast.success(translate($language, 'dk.imageDeleted'));
			await loadImages();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.imageDeleteFailed'));
		}
	}

	// ── Volume actions ────────────────────────────────────────────
	async function createVolume() {
		if (!newVolumeName.trim()) return;
		creatingVolume = true;
		try {
			await api.post('/api/v1/docker/volumes', { name: newVolumeName.trim() });
			toast.success(translate($language, 'dk.volumeCreated').replace('{name}', newVolumeName.trim()));
			newVolumeName = '';
			await loadVolumes();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.volumeCreateFailed'));
		} finally {
			creatingVolume = false;
		}
	}

	async function deleteVolume(name: string) {
		deleteVolumeConfirmId = null;
		try {
			await api.del(`/api/v1/docker/volumes/${encodeURIComponent(name)}`);
			toast.success(translate($language, 'dk.volumeDeleted').replace('{name}', name));
			await loadVolumes();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.volumeDeleteFailed'));
		}
	}

	// ── Network actions ───────────────────────────────────────────
	async function createNetwork() {
		if (!newNetworkName.trim()) return;
		creatingNetwork = true;
		try {
			await api.post('/api/v1/docker/networks', { name: newNetworkName.trim() });
			toast.success(translate($language, 'dk.networkCreated').replace('{name}', newNetworkName.trim()));
			newNetworkName = '';
			await loadNetworks();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.networkCreateFailed'));
		} finally {
			creatingNetwork = false;
		}
	}

	async function deleteNetwork(id: string, name: string) {
		deleteNetworkConfirmId = null;
		try {
			await api.del(`/api/v1/docker/networks/${id}`);
			toast.success(translate($language, 'dk.networkDeleted').replace('{name}', name));
			await loadNetworks();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.networkDeleteFailed'));
		}
	}

	// ── Compose actions ───────────────────────────────────────────
	async function composeUp() {
		if (!composePath.trim()) return;
		composeActionInProgress = 'up';
		try {
			await api.post('/api/v1/docker/compose/up', { path: composePath.trim() });
			toast.success(translate($language, 'dk.composeUpDone'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.composeUpFailed'));
		} finally {
			composeActionInProgress = null;
		}
	}

	async function composeDown() {
		if (!composePath.trim()) return;
		composeActionInProgress = 'down';
		try {
			await api.post('/api/v1/docker/compose/down', { path: composePath.trim() });
			toast.success(translate($language, 'dk.composeDownDone'));
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'dk.composeDownFailed'));
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
	<h2 class="text-2xl font-bold text-white">{translate($language, 'dk.title')}</h2>

	<!-- Feedback messages -->


	<!-- ═══════════════════════════ STATUS CARD ═══════════════════════════ -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'dk.status')}</h3>
		{#if loadingStatus}
			<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingStatus')}</div>
		{:else if !dockerStatus}
			<div class="text-gray-400 text-sm">{translate($language, 'dk.statusUnavailable')}</div>
		{:else}
			<div class="flex flex-wrap items-center gap-6 mb-4">
				<!-- Badges -->
				<div class="flex items-center gap-3">
					{#if dockerStatus.installed}
						<span class="text-xs bg-green-900/50 text-green-400 px-2.5 py-1 rounded font-medium">{translate($language, 'dk.installed')}</span>
					{:else}
						<span class="text-xs bg-gray-700 text-gray-400 px-2.5 py-1 rounded font-medium">{translate($language, 'dk.notInstalled')}</span>
					{/if}
					{#if dockerStatus.installed}
						<span class="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded font-medium {dockerStatus.running ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
							<span class="w-1.5 h-1.5 rounded-full {dockerStatus.running ? 'bg-green-400' : 'bg-red-400'}"></span>
							{dockerStatus.running ? translate($language, 'dk.running') : translate($language, 'dk.stopped')}
						</span>
					{/if}
				</div>

				<!-- Info -->
				{#if dockerStatus.version}
					<div class="text-xs text-gray-400">{translate($language, 'dk.versionLabel')} <span class="text-gray-300">{dockerStatus.version}</span></div>
				{/if}
				{#if dockerStatus.installed}
					<div class="text-xs text-gray-400">{translate($language, 'dk.containersLabel')} <span class="text-gray-300">{dockerStatus.containers_count ?? 0}</span></div>
					<div class="text-xs text-gray-400">{translate($language, 'dk.imagesLabel')} <span class="text-gray-300">{dockerStatus.images_count ?? 0}</span></div>
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
						{actionInProgress === 'install' ? translate($language, 'dk.installing') : translate($language, 'dk.install')}
					</button>
				{:else}
					{#if !dockerStatus.running}
						<button
							onclick={() => dockerDaemonAction('start')}
							disabled={actionInProgress !== null}
							class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
						>
							{actionInProgress === 'daemon-start' ? '...' : translate($language, 'dk.start')}
						</button>
					{:else}
						<button
							onclick={() => dockerDaemonAction('stop')}
							disabled={actionInProgress !== null}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
						>
							{actionInProgress === 'daemon-stop' ? '...' : translate($language, 'dk.stop')}
						</button>
					{/if}
					<button
						onclick={() => dockerDaemonAction('restart')}
						disabled={actionInProgress !== null}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
					>
						{actionInProgress === 'daemon-restart' ? '...' : translate($language, 'dk.restart')}
					</button>
				{/if}
			</div>
		{/if}
	</div>

	<!-- ═══════════════════════════ TABS ═══════════════════════════ -->
	<div class="border-b border-gray-700">
		<nav class="flex gap-0 -mb-px">
			{#each [
				{ key: 'containers', label: translate($language, 'dk.containers') },
				{ key: 'images', label: translate($language, 'dk.images') },
				{ key: 'volumes', label: translate($language, 'dk.volumes') },
				{ key: 'networks', label: translate($language, 'dk.networks') },
				{ key: 'compose', label: translate($language, 'dk.compose') }
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
				<h3 class="text-lg font-semibold text-white">{translate($language, 'dk.containers')}</h3>
				<label class="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
					<input
						type="checkbox"
						bind:checked={showAll}
						class="rounded border-gray-600 bg-gray-900 text-blue-600 focus:ring-blue-500 cursor-pointer"
					/>
					{translate($language, 'dk.showAll')}
				</label>
			</div>

			{#if loadingContainers}
				<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingContainers')}</div>
			{:else if containers.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">{translate($language, 'dk.noContainers')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thId')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thName')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thImage')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thStatus')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thState')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thPorts')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thActions')}</th>
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
											<span class="text-xs text-red-400 mr-1">{translate($language, 'dk.removeConfirm')}</span>
											<button
												onclick={() => removeContainer(container.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'dk.yes')}
											</button>
											<button
												onclick={() => (deleteContainerConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												{translate($language, 'dk.cancel')}
											</button>
										{:else}
											<div class="flex items-center justify-end gap-1.5">
												{#if container.state?.toLowerCase() !== 'running'}
													<button
														onclick={() => containerAction(container.id, 'start')}
														disabled={actionInProgress !== null}
														class="px-2 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
													>
														{actionInProgress === `container-start-${container.id}` ? '...' : translate($language, 'dk.start')}
													</button>
												{:else}
													<button
														onclick={() => containerAction(container.id, 'stop')}
														disabled={actionInProgress !== null}
														class="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
													>
														{actionInProgress === `container-stop-${container.id}` ? '...' : translate($language, 'dk.stop')}
													</button>
												{/if}
												<button
													onclick={() => containerAction(container.id, 'restart')}
													disabled={actionInProgress !== null}
													class="px-2 py-1 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{actionInProgress === `container-restart-${container.id}` ? '...' : translate($language, 'dk.restart')}
												</button>
												<button
													onclick={() => fetchLogs(container.id)}
													class="px-2 py-1 bg-indigo-600 hover:bg-indigo-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'dk.logs')}
												</button>
												<button
													onclick={() => (deleteContainerConfirmId = container.id)}
													disabled={actionInProgress !== null}
													class="px-2 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'dk.remove')}
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
													<span class="text-xs text-gray-400 font-medium uppercase tracking-wider">{translate($language, 'dk.containerLogs').replace('{name}', container.name)}</span>
													<button
														onclick={() => (expandedLogs = null)}
														class="text-xs text-gray-500 hover:text-gray-300 cursor-pointer"
													>
														{translate($language, 'dk.close')}
													</button>
												</div>
												{#if loadingLogs === container.id}
													<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingLogs')}</div>
												{:else}
													<pre class="text-xs text-gray-300 font-mono whitespace-pre-wrap max-h-80 overflow-y-auto">{containerLogs[container.id] || translate($language, 'dk.noLogs')}</pre>
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
			<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'dk.images')}</h3>

			<!-- Pull image form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="pull-image" class="block text-sm text-gray-400 mb-1">{translate($language, 'dk.pullImage')}</label>
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
					{pullingImage ? translate($language, 'dk.pulling') : translate($language, 'dk.pull')}
				</button>
			</div>

			{#if loadingImages}
				<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingImages')}</div>
			{:else if images.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">{translate($language, 'dk.noImages')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thRepository')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thTag')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thId')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thSize')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thCreated')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thActions')}</th>
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
											<span class="text-xs text-red-400 mr-1">{translate($language, 'dk.deleteConfirm')}</span>
											<button
												onclick={() => deleteImage(img.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'dk.yes')}
											</button>
											<button
												onclick={() => (deleteImageConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												{translate($language, 'dk.cancel')}
											</button>
										{:else}
											<button
												onclick={() => (deleteImageConfirmId = img.id)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'dk.delete')}
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
			<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'dk.volumes')}</h3>

			<!-- Create volume form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="vol-name" class="block text-sm text-gray-400 mb-1">{translate($language, 'dk.volumeName')}</label>
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
					{creatingVolume ? translate($language, 'dk.creating') : translate($language, 'dk.create')}
				</button>
			</div>

			{#if loadingVolumes}
				<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingVolumes')}</div>
			{:else if volumes.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">{translate($language, 'dk.noVolumes')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thName')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thDriver')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thMountpoint')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thActions')}</th>
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
											<span class="text-xs text-red-400 mr-1">{translate($language, 'dk.deleteConfirm')}</span>
											<button
												onclick={() => deleteVolume(vol.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'dk.yes')}
											</button>
											<button
												onclick={() => (deleteVolumeConfirmId = null)}
												class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
											>
												{translate($language, 'dk.cancel')}
											</button>
										{:else}
											<button
												onclick={() => (deleteVolumeConfirmId = vol.name)}
												class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
											>
												{translate($language, 'dk.delete')}
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
			<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'dk.networks')}</h3>

			<!-- Create network form -->
			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div>
					<label for="net-name" class="block text-sm text-gray-400 mb-1">{translate($language, 'dk.networkName')}</label>
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
					{creatingNetwork ? translate($language, 'dk.creating') : translate($language, 'dk.create')}
				</button>
			</div>

			{#if loadingNetworks}
				<div class="text-gray-400 text-sm">{translate($language, 'dk.loadingNetworks')}</div>
			{:else if networks.length === 0}
				<div class="text-gray-500 text-sm py-4 text-center">{translate($language, 'dk.noNetworks')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thName')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thId')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thDriver')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thScope')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'dk.thActions')}</th>
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
												<span class="text-xs text-gray-500 italic">{translate($language, 'dk.defaultLabel')}</span>
											{:else if deleteNetworkConfirmId === net.id}
												<span class="text-xs text-red-400 mr-1">{translate($language, 'dk.deleteConfirm')}</span>
												<button
													onclick={() => deleteNetwork(net.id, net.name)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'dk.yes')}
												</button>
												<button
													onclick={() => (deleteNetworkConfirmId = null)}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
												>
													{translate($language, 'dk.cancel')}
												</button>
											{:else}
												<button
													onclick={() => (deleteNetworkConfirmId = net.id)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													{translate($language, 'dk.delete')}
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
			<h3 class="text-lg font-semibold text-white mb-4">{translate($language, 'dk.composeTitle')}</h3>

			<div class="flex flex-wrap items-end gap-3 mb-4">
				<div class="flex-1 min-w-64">
					<label for="compose-path" class="block text-sm text-gray-400 mb-1">{translate($language, 'dk.composePath')}</label>
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
						{composeActionInProgress === 'up' ? translate($language, 'dk.starting') : translate($language, 'dk.up')}
					</button>
					<button
						onclick={composeDown}
						disabled={composeActionInProgress !== null || !composePath.trim()}
						class="px-4 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						{composeActionInProgress === 'down' ? translate($language, 'dk.stopping') : translate($language, 'dk.down')}
					</button>
				</div>
			</div>

			{#if composeStatus}
				<div class="bg-gray-900 rounded border border-gray-700 p-4">
					<div class="text-xs text-gray-400 font-medium uppercase tracking-wider mb-2">{translate($language, 'dk.composeStatus')}</div>
					<div class="text-sm text-gray-300">{composeStatus.status}</div>
					{#if composeStatus.services && composeStatus.services.length > 0}
						<div class="mt-2 text-xs text-gray-400">
							{translate($language, 'dk.servicesLabel')} <span class="text-gray-300">{composeStatus.services.join(', ')}</span>
						</div>
					{/if}
				</div>
			{:else}
				<div class="text-gray-500 text-sm">{translate($language, 'dk.composeHint')}</div>
			{/if}
		</div>
	{/if}

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_docker_task" onComplete={onInstallComplete} onMissing={() => { actionInProgress = null; }} />
</div>
