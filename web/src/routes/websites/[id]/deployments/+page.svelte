<script lang="ts">
	import { onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { api } from '$lib/api';
	import WebsiteSectionNav from '$lib/components/WebsiteSectionNav.svelte';
	import { websiteOperationAPI } from '$lib/website-operations.js';
import { toast } from '$lib/stores/toast';

	interface Deployment {
		id: string;
		website_id: string;
		commit_hash: string;
		branch: string;
		status: string;
		duration_ms: number;
		log: string;
		created_at: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		status: string;
	}

	let websiteID = $derived(page.params.id ?? '');
	let operationAPI = $derived(websiteOperationAPI(websiteID));
	let website = $state<Website | null>(null);
	let loadedWebsiteID = $state('');
	let currentWebsite = $derived(loadedWebsiteID === websiteID ? website : null);
	let deployments = $state<Deployment[]>([]);
	let loading = $state(false);
	let loadingWebsite = $state(true);
	let websiteError = $state('');
	let error = $state('');

	// Deploy form
	let showDeployForm = $state(false);
	let deployRepoUrl = $state('');
	let deployBranch = $state('main');
	let deploying = $state(false);

	// Expanded logs
	let expandedId = $state<string | null>(null);

	// Polling
	let pollTimer: ReturnType<typeof setInterval> | null = null;
	let websiteLoadGeneration = 0;

	function statusBadgeClass(status: string): string {
		switch (status) {
			case 'success':
				return 'bg-green-900/50 text-green-400';
			case 'running':
				return 'bg-yellow-900/50 text-yellow-400 animate-pulse';
			case 'failed':
				return 'bg-red-900/50 text-red-400';
			case 'pending':
				return 'bg-gray-700 text-gray-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '-';
		const d = new Date(dateStr);
		return d.toLocaleString('en-US', {
			month: 'short',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit'
		});
	}

	function formatDuration(seconds: number): string {
		if (!seconds || seconds <= 0) return '-';
		if (seconds < 60) return `${seconds}s`;
		const mins = Math.floor(seconds / 60);
		const secs = seconds % 60;
		return `${mins}m ${secs}s`;
	}

	function shortHash(hash: string): string {
		if (!hash) return '-';
		return hash.substring(0, 7);
	}

	function hasRunning(deps: Deployment[]): boolean {
		return deps.some((d) => d.status === 'running' || d.status === 'pending');
	}

	function isCurrentRequest(requestedWebsiteID: string, generation: number): boolean {
		return websiteID === requestedWebsiteID && websiteLoadGeneration === generation;
	}

	function isCurrentRouteWebsite(requestedWebsiteID: string, generation: number): boolean {
		return isCurrentRequest(requestedWebsiteID, generation) && loadedWebsiteID === requestedWebsiteID && website !== null;
	}

	function startPolling(
		scopedAPI = operationAPI,
		requestedWebsiteID = websiteID,
		generation = websiteLoadGeneration
	) {
		stopPolling();
		pollTimer = setInterval(async () => {
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				stopPolling();
				return;
			}
			try {
				const nextDeployments = (await api.get<Deployment[]>(scopedAPI.deployments)) || [];
				if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
				deployments = nextDeployments;
				if (!hasRunning(deployments)) {
					stopPolling();
				}
			} catch {
				// Silently ignore polling errors
			}
		}, 5000);
	}

	function stopPolling() {
		if (pollTimer) {
			clearInterval(pollTimer);
			pollTimer = null;
		}
	}

	function resetForWebsiteChange() {
		stopPolling();
		website = null;
		loadedWebsiteID = '';
		deployments = [];
		loading = false;
		error = '';
		showDeployForm = false;
		deployRepoUrl = '';
		deployBranch = 'main';
		deploying = false;
		expandedId = null;
	}

	async function loadWebsite(requestedWebsiteID: string) {
		const generation = ++websiteLoadGeneration;
		loadingWebsite = true;
		websiteError = '';
		resetForWebsiteChange();
		if (!requestedWebsiteID) {
			websiteError = 'Website ID is required';
			loadingWebsite = false;
			return;
		}
		const scopedAPI = websiteOperationAPI(requestedWebsiteID);
		try {
			const loadedWebsite = await api.get<Website>(scopedAPI.website);
			if (!isCurrentRequest(requestedWebsiteID, generation)) return;
			website = loadedWebsite;
			loadedWebsiteID = requestedWebsiteID;
			await loadDeployments(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				websiteError = err instanceof Error ? err.message : 'Failed to load website';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				loadingWebsite = false;
			}
		}
	}

	async function loadDeployments(
		scopedAPI = operationAPI,
		requestedWebsiteID = websiteID,
		generation = websiteLoadGeneration
	) {
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) {
			deployments = [];
			return;
		}
		loading = true;
		error = '';
		try {
			const nextDeployments = (await api.get<Deployment[]>(scopedAPI.deployments)) || [];
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			deployments = nextDeployments;
			if (hasRunning(deployments)) {
				startPolling(scopedAPI, requestedWebsiteID, generation);
			}
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				error = err instanceof Error ? err.message : 'Failed to load deployments';
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				loading = false;
			}
		}
	}

	async function deploy() {
		const requestedWebsiteID = websiteID;
		const generation = websiteLoadGeneration;
		const scopedAPI = operationAPI;
		if (!isCurrentRouteWebsite(requestedWebsiteID, generation) || !deployRepoUrl.trim()) return;
		deploying = true;
		try {
			await api.post(scopedAPI.deploy, {
				repo: deployRepoUrl.trim(),
				branch: deployBranch.trim() || 'main'
			});
			if (!isCurrentRouteWebsite(requestedWebsiteID, generation)) return;
			toast.success('Deployment started.');
			showDeployForm = false;
			deployRepoUrl = '';
			deployBranch = 'main';
			await loadDeployments(scopedAPI, requestedWebsiteID, generation);
		} catch (err) {
			if (isCurrentRouteWebsite(requestedWebsiteID, generation)) {
				toast.error(err instanceof Error ? err.message : 'Failed to start deployment');
			}
		} finally {
			if (isCurrentRequest(requestedWebsiteID, generation)) {
				deploying = false;
			}
		}
	}

	function toggleLog(id: string) {
		expandedId = expandedId === id ? null : id;
	}

	$effect(() => {
		void loadWebsite(websiteID);
	});

	onDestroy(() => {
		websiteLoadGeneration++;
		stopPolling();
	});
</script>

<div class="space-y-6">
	{#if loadingWebsite}
		<div class="text-gray-400">Loading website...</div>
	{:else if websiteError}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{websiteError}</div>
	{:else if currentWebsite}
	<div class="flex items-center justify-between">
		<div>
			<a href="/websites" class="text-sm text-blue-400 hover:text-blue-300">Websites</a>
			<div class="flex flex-wrap items-center gap-3">
				<h2 class="text-2xl font-bold text-white">Deployments · {currentWebsite.domain}</h2>
				<span aria-label="Website status" class="inline-block px-2.5 py-0.5 rounded bg-gray-700 text-xs font-medium text-gray-300">{currentWebsite.status}</span>
			</div>
		</div>
		<button
			onclick={() => (showDeployForm = !showDeployForm)}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
		>
			{showDeployForm ? 'Cancel' : 'Deploy'}
		</button>
	</div>

	<WebsiteSectionNav websiteId={currentWebsite.id} currentPath={page.url.pathname} />



	<!-- Deploy Form -->
	{#if showDeployForm}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-4">New Deployment</h3>
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="deploy-repo" class="block text-sm text-gray-400 mb-1">Repository URL</label>
					<input
						id="deploy-repo"
						type="text"
						bind:value={deployRepoUrl}
						placeholder="https://github.com/user/repo.git"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="deploy-branch" class="block text-sm text-gray-400 mb-1">Branch</label>
					<input
						id="deploy-branch"
						type="text"
						bind:value={deployBranch}
						placeholder="main"
						class="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			</div>
			<div class="mt-4">
				<button
					onclick={deploy}
					disabled={deploying || !deployRepoUrl.trim()}
					class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{deploying ? 'Deploying...' : 'Start Deploy'}
				</button>
			</div>
		</div>
	{/if}

	<!-- Deployment History -->
	{#if loading}
		<div class="text-gray-400">Loading deployments...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if deployments.length === 0}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center">
			<p class="text-gray-400">No deployments yet for this website.</p>
		</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Commit</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Branch</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Duration</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Created</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Log</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each deployments as dep}
							<tr
								class="hover:bg-gray-750 cursor-pointer"
								onclick={() => toggleLog(dep.id)}
							>
								<td class="px-4 py-3 text-sm">
									<code class="text-blue-400 bg-gray-900 px-1.5 py-0.5 rounded text-xs">{shortHash(dep.commit_hash)}</code>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300">{dep.branch || '-'}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {statusBadgeClass(dep.status)}">
										{dep.status}
									</span>
								</td>
								<td class="px-4 py-3 text-sm text-gray-400">{formatDuration(dep.duration_ms)}</td>
								<td class="px-4 py-3 text-sm text-gray-400">{formatDate(dep.created_at)}</td>
								<td class="px-4 py-3 text-right">
									<svg
										class="w-4 h-4 inline-block text-gray-400 transition-transform {expandedId === dep.id ? 'rotate-180' : ''}"
										fill="none"
										stroke="currentColor"
										viewBox="0 0 24 24"
										stroke-width="2"
									>
										<path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7" />
									</svg>
								</td>
							</tr>
							{#if expandedId === dep.id}
								<tr>
									<td colspan="6" class="px-4 py-3 bg-gray-900">
										<div class="max-h-80 overflow-auto">
											<pre class="text-xs text-gray-300 whitespace-pre-wrap font-mono leading-relaxed">{dep.log || 'No log output available.'}</pre>
										</div>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
	{/if}
</div>
