<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';

	interface WebsiteDomain {
		id: string;
		name: string;
		type: string;
	}

	interface Website {
		id: string;
		domain: string;
		app_type: string;
		php_version: string;
		status: string;
		error_message?: string;
		domains?: WebsiteDomain[];
		created_at: string;
	}

	let website = $state<Website | null>(null);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Domains
	let addDomainName = $state('');
	let addDomainType = $state('alias');
	let addingDomain = $state(false);

	// Config editor
	let configContent = $state('');
	let configLoading = $state(false);
	let configError = $state('');
	let configSaveMsg = $state('');
	let showConfig = $state(false);

	// Logs
	let logTab = $state<'access' | 'error'>('access');
	let accessLogs = $state('');
	let errorLogs = $state('');
	let logsLoading = $state(false);
	let logsError = $state('');
	let showLogs = $state(false);

	// Delete confirm
	let deleteConfirm = $state(false);

	// File Manager
	interface FileEntry {
		name: string;
		type: 'file' | 'directory';
		size: number;
		permissions: string;
	}

	let files = $state<FileEntry[]>([]);
	let currentPath = $state('/');
	let filesLoading = $state(false);
	let filesError = $state('');
	let showFiles = $state(false);
	let fileActionMsg = $state('');
	let fileActionError = $state('');

	// File edit
	let editingFile = $state<string | null>(null);
	let editFileContent = $state('');
	let editFileLoading = $state(false);

	// Create dir/file
	let showCreateDir = $state(false);
	let newDirName = $state('');
	let showCreateFile = $state(false);
	let newFileName = $state('');

	// Rename
	let renamingFile = $state<string | null>(null);
	let renameValue = $state('');

	// Upload
	let uploadInput: HTMLInputElement;

	const pendingStatuses = ['pending', 'installing', 'configuring', 'validating'];

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

	function domainTypeBadgeClass(type: string): string {
		switch (type) {
			case 'primary':
				return 'bg-blue-900 text-blue-300';
			case 'alias':
				return 'bg-purple-900 text-purple-300';
			case 'subdomain':
				return 'bg-cyan-900 text-cyan-300';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	async function loadWebsite() {
		loading = true;
		error = '';
		try {
			website = await api.get<Website>(`/api/v1/websites/${page.params.id}`);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load website';
		} finally {
			loading = false;
		}
	}

	async function addDomain() {
		if (!website || !addDomainName.trim()) return;
		addingDomain = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/domains`, {
				name: addDomainName,
				type: addDomainType
			});
			actionMsg = `Domain "${addDomainName}" added.`;
			addDomainName = '';
			addDomainType = 'alias';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to add domain';
		} finally {
			addingDomain = false;
		}
	}

	async function removeDomain(domainId: string) {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}/domains/${domainId}`);
			actionMsg = 'Domain removed.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to remove domain';
		}
	}

	async function loadConfig() {
		if (!website) return;
		configLoading = true;
		configError = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/websites/${website.id}/config`);
			configContent = data.content || '';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to load config';
		} finally {
			configLoading = false;
		}
	}

	async function saveConfig() {
		if (!website) return;
		configSaveMsg = '';
		configError = '';
		try {
			await api.put(`/api/v1/websites/${website.id}/config`, { content: configContent });
			configSaveMsg = 'Configuration saved successfully.';
		} catch (err) {
			configError = err instanceof Error ? err.message : 'Failed to save config';
		}
	}

	async function loadLogs(type: 'access' | 'error') {
		if (!website) return;
		logsLoading = true;
		logsError = '';
		try {
			const data = await api.get<{ content: string }>(
				`/api/v1/websites/${website.id}/logs/${type}?lines=100`
			);
			if (type === 'access') {
				accessLogs = data.content || '';
			} else {
				errorLogs = data.content || '';
			}
		} catch (err) {
			logsError = err instanceof Error ? err.message : 'Failed to load logs';
		} finally {
			logsLoading = false;
		}
	}

	async function suspendWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/suspend`);
			actionMsg = 'Website suspended.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to suspend website';
		}
	}

	async function enableWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/enable`);
			actionMsg = 'Website enabled.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to enable website';
		}
	}

	async function retryWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/retry`);
			actionMsg = 'Retry initiated.';
			await loadWebsite();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to retry';
		}
	}

	async function deleteWebsite() {
		if (!website) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}`);
			goto('/websites');
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete website';
			deleteConfirm = false;
		}
	}

	// File Manager functions
	async function loadFiles(path: string = '/') {
		if (!website) return;
		filesLoading = true;
		filesError = '';
		try {
			const data = await api.get<FileEntry[]>(`/api/v1/websites/${website.id}/files?path=${encodeURIComponent(path)}`);
			files = data || [];
			currentPath = path;
		} catch (err) {
			filesError = err instanceof Error ? err.message : 'Failed to load files';
		} finally {
			filesLoading = false;
		}
	}

	function navigateTo(name: string) {
		const newPath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		loadFiles(newPath);
	}

	function navigateUp() {
		const parts = currentPath.split('/').filter(Boolean);
		parts.pop();
		loadFiles(parts.length === 0 ? '/' : '/' + parts.join('/'));
	}

	function breadcrumbParts(): { name: string; path: string }[] {
		const parts = currentPath.split('/').filter(Boolean);
		const result = [{ name: '/', path: '/' }];
		let accumulated = '';
		for (const part of parts) {
			accumulated += '/' + part;
			result.push({ name: part, path: accumulated });
		}
		return result;
	}

	function formatSize(bytes: number): string {
		if (bytes === 0) return '-';
		const units = ['B', 'KB', 'MB', 'GB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) {
			size /= 1024;
			i++;
		}
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function isTextFile(name: string): boolean {
		const ext = name.split('.').pop()?.toLowerCase() || '';
		return ['txt', 'html', 'css', 'js', 'ts', 'json', 'xml', 'yml', 'yaml', 'md', 'conf', 'cfg', 'ini', 'log', 'sh', 'bash', 'php', 'py', 'rb', 'env', 'htaccess', 'svg'].includes(ext);
	}

	async function openFileEdit(name: string) {
		if (!website) return;
		const filePath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		editFileLoading = true;
		editingFile = filePath;
		editFileContent = '';
		try {
			const data = await api.get<{ content: string }>(`/api/v1/websites/${website.id}/files/content?path=${encodeURIComponent(filePath)}`);
			editFileContent = data.content || '';
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to read file';
			editingFile = null;
		} finally {
			editFileLoading = false;
		}
	}

	async function saveFileEdit() {
		if (!website || !editingFile) return;
		fileActionMsg = '';
		fileActionError = '';
		try {
			await api.put(`/api/v1/websites/${website.id}/files/content`, { path: editingFile, content: editFileContent });
			fileActionMsg = 'File saved.';
			editingFile = null;
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to save file';
		}
	}

	async function deleteFile(name: string) {
		if (!website) return;
		const filePath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		fileActionMsg = '';
		fileActionError = '';
		try {
			await api.del(`/api/v1/websites/${website.id}/files?path=${encodeURIComponent(filePath)}`);
			fileActionMsg = `"${name}" deleted.`;
			await loadFiles(currentPath);
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to delete file';
		}
	}

	async function createDir() {
		if (!website || !newDirName.trim()) return;
		const dirPath = currentPath === '/' ? `/${newDirName}` : `${currentPath}/${newDirName}`;
		fileActionMsg = '';
		fileActionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/files/directory`, { path: dirPath });
			fileActionMsg = `Directory "${newDirName}" created.`;
			newDirName = '';
			showCreateDir = false;
			await loadFiles(currentPath);
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to create directory';
		}
	}

	async function createFile() {
		if (!website || !newFileName.trim()) return;
		const filePath = currentPath === '/' ? `/${newFileName}` : `${currentPath}/${newFileName}`;
		fileActionMsg = '';
		fileActionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/files`, { path: filePath, content: '' });
			fileActionMsg = `File "${newFileName}" created.`;
			newFileName = '';
			showCreateFile = false;
			await loadFiles(currentPath);
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to create file';
		}
	}

	async function renameFile(oldName: string) {
		if (!website || !renameValue.trim()) return;
		const oldPath = currentPath === '/' ? `/${oldName}` : `${currentPath}/${oldName}`;
		const newPath = currentPath === '/' ? `/${renameValue}` : `${currentPath}/${renameValue}`;
		fileActionMsg = '';
		fileActionError = '';
		try {
			await api.post(`/api/v1/websites/${website.id}/files/rename`, { old_path: oldPath, new_path: newPath });
			fileActionMsg = `Renamed "${oldName}" to "${renameValue}".`;
			renamingFile = null;
			renameValue = '';
			await loadFiles(currentPath);
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to rename';
		}
	}

	async function uploadFile(event: Event) {
		if (!website) return;
		const input = event.target as HTMLInputElement;
		const file = input.files?.[0];
		if (!file) return;
		fileActionMsg = '';
		fileActionError = '';
		try {
			const formData = new FormData();
			formData.append('file', file);
			formData.append('path', currentPath);
			const res = await fetch(`/api/v1/websites/${website.id}/files/upload`, {
				method: 'POST',
				credentials: 'include',
				body: formData
			});
			if (!res.ok) throw new Error(`Upload failed: ${res.statusText}`);
			fileActionMsg = `"${file.name}" uploaded.`;
			input.value = '';
			await loadFiles(currentPath);
		} catch (err) {
			fileActionError = err instanceof Error ? err.message : 'Failed to upload file';
		}
	}

	function downloadFile(name: string) {
		if (!website) return;
		const filePath = currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
		window.open(`/api/v1/websites/${website.id}/files/download?path=${encodeURIComponent(filePath)}`, '_blank');
	}

	onMount(loadWebsite);
</script>

<div class="space-y-6">
	<!-- Back link -->
	<a href="/websites" class="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-white transition-colors">
		<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
			<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
		</svg>
		Back to Websites
	</a>

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
		<div class="text-gray-400">Loading website details...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if website}
		<!-- Header -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<h2 class="text-2xl font-bold text-white">{website.domain}</h2>
				<span class="inline-block px-2.5 py-0.5 rounded text-xs font-medium {statusBadgeClass(website.status)}">
					{website.status}
				</span>
			</div>
			<div class="flex flex-wrap gap-4 text-sm text-gray-400">
				<span>App Type: <span class="text-gray-200 capitalize">{website.app_type}</span></span>
				{#if website.app_type !== 'static'}
					<span>PHP Version: <span class="text-gray-200">{website.php_version}</span></span>
				{/if}
			</div>

			{#if website.status === 'failed' && website.error_message}
				<div class="mt-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
					{website.error_message}
				</div>
			{/if}

			{#if pendingStatuses.includes(website.status)}
				<div class="mt-3 p-3 bg-yellow-900/30 border border-yellow-700 rounded-lg text-yellow-300 text-sm">
					Provisioning in progress: <span class="font-medium">{website.status}</span>
				</div>
			{/if}

			<!-- Actions -->
			<div class="mt-4 flex flex-wrap gap-2">
				{#if website.status === 'active'}
					<button
						onclick={suspendWebsite}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Suspend
					</button>
				{/if}
				{#if website.status === 'suspended' || website.status === 'disabled'}
					<button
						onclick={enableWebsite}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Enable
					</button>
				{/if}
				{#if website.status === 'failed'}
					<button
						onclick={retryWebsite}
						class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Retry
					</button>
				{/if}

				{#if deleteConfirm}
					<div class="flex items-center gap-2 p-2 bg-red-900/30 border border-red-700 rounded-lg">
						<span class="text-sm text-red-300">Are you sure? This cannot be undone.</span>
						<button
							onclick={deleteWebsite}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Yes, Delete
						</button>
						<button
							onclick={() => (deleteConfirm = false)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Cancel
						</button>
					</div>
				{:else}
					<button
						onclick={() => (deleteConfirm = true)}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Delete
					</button>
				{/if}
			</div>
		</div>

		<!-- Domains -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Domains</h3>

			{#if website.domains && website.domains.length > 0}
				<div class="space-y-2 mb-4">
					{#each website.domains as domain}
						<div class="flex items-center justify-between p-3 bg-gray-900 rounded-lg">
							<div class="flex items-center gap-3">
								<span class="text-sm text-gray-200">{domain.name}</span>
								<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {domainTypeBadgeClass(domain.type)}">
									{domain.type}
								</span>
							</div>
							{#if domain.type !== 'primary'}
								<button
									onclick={() => removeDomain(domain.id)}
									class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
								>
									Remove
								</button>
							{/if}
						</div>
					{/each}
				</div>
			{:else}
				<p class="text-sm text-gray-400 mb-4">No additional domains configured.</p>
			{/if}

			<!-- Add Domain Form -->
			<div class="flex flex-wrap items-end gap-3">
				<div>
					<label for="add-domain-name" class="block text-sm text-gray-400 mb-1">Domain Name</label>
					<input
						id="add-domain-name"
						type="text"
						bind:value={addDomainName}
						placeholder="sub.example.com"
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="add-domain-type" class="block text-sm text-gray-400 mb-1">Type</label>
					<select
						id="add-domain-type"
						bind:value={addDomainType}
						class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="alias">Alias</option>
						<option value="subdomain">Subdomain</option>
					</select>
				</div>
				<button
					onclick={addDomain}
					disabled={addingDomain || !addDomainName.trim()}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
				>
					{addingDomain ? 'Adding...' : 'Add Domain'}
				</button>
			</div>
		</div>

		<!-- Nginx Config -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-3">
				<h3 class="text-lg font-semibold text-white">Nginx Configuration</h3>
				{#if !showConfig}
					<button
						onclick={() => { showConfig = true; loadConfig(); }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Edit Config
					</button>
				{:else}
					<button
						onclick={() => (showConfig = false)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Close
					</button>
				{/if}
			</div>

			{#if showConfig}
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
					<textarea
						bind:value={configContent}
						rows={20}
						class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
					></textarea>
					<div class="mt-2">
						<button
							onclick={saveConfig}
							class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer"
						>
							Save Configuration
						</button>
					</div>
				{/if}
			{/if}
		</div>

		<!-- Logs -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-3">
				<h3 class="text-lg font-semibold text-white">Logs</h3>
				{#if !showLogs}
					<button
						onclick={() => { showLogs = true; loadLogs(logTab); }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						View Logs
					</button>
				{:else}
					<button
						onclick={() => (showLogs = false)}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Close
					</button>
				{/if}
			</div>

			{#if showLogs}
				<div class="flex gap-2 mb-4">
					<button
						onclick={() => { logTab = 'access'; loadLogs('access'); }}
						class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'access'
							? 'bg-blue-600 text-white'
							: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
					>
						Access Log
					</button>
					<button
						onclick={() => { logTab = 'error'; loadLogs('error'); }}
						class="px-3 py-1.5 text-sm rounded transition-colors cursor-pointer {logTab === 'error'
							? 'bg-blue-600 text-white'
							: 'bg-gray-700 text-gray-300 hover:bg-gray-600'}"
					>
						Error Log
					</button>
				</div>

				{#if logsError}
					<div class="mb-2 text-red-400 text-sm">{logsError}</div>
				{/if}

				<div class="flex items-center justify-end mb-2">
					<button
						onclick={() => loadLogs(logTab)}
						disabled={logsLoading}
						class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer"
					>
						{logsLoading ? 'Loading...' : 'Refresh'}
					</button>
				</div>

				<textarea
					readonly
					value={logTab === 'access' ? accessLogs : errorLogs}
					class="w-full h-64 bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-xs font-mono resize-y focus:outline-none"
				></textarea>
			{/if}
		</div>

		<!-- File Manager -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex items-center justify-between mb-3">
				<h3 class="text-lg font-semibold text-white">File Manager</h3>
				{#if !showFiles}
					<button
						onclick={() => { showFiles = true; loadFiles('/'); }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Browse Files
					</button>
				{:else}
					<button
						onclick={() => { showFiles = false; editingFile = null; }}
						class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded transition-colors cursor-pointer"
					>
						Close
					</button>
				{/if}
			</div>

			{#if showFiles}
				{#if fileActionMsg}
					<div class="mb-3 p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
						{fileActionMsg}
						<button onclick={() => (fileActionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">Dismiss</button>
					</div>
				{/if}

				{#if fileActionError}
					<div class="mb-3 p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
						{fileActionError}
						<button onclick={() => (fileActionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">Dismiss</button>
					</div>
				{/if}

				{#if editingFile}
					<!-- File Editor -->
					<div class="space-y-3">
						<div class="flex items-center justify-between">
							<span class="text-sm text-gray-300 font-mono">{editingFile}</span>
							<button onclick={() => (editingFile = null)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Back</button>
						</div>
						{#if editFileLoading}
							<div class="text-gray-400 text-sm">Loading file...</div>
						{:else}
							<textarea
								bind:value={editFileContent}
								rows={20}
								class="w-full bg-gray-950 border border-gray-700 rounded p-3 text-gray-300 text-sm font-mono resize-y focus:outline-none focus:ring-2 focus:ring-blue-500"
							></textarea>
							<button onclick={saveFileEdit} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
								Save File
							</button>
						{/if}
					</div>
				{:else}
					<!-- Breadcrumb -->
					<div class="flex items-center gap-1 mb-3 text-sm">
						{#each breadcrumbParts() as part, i}
							{#if i > 0}
								<span class="text-gray-500">/</span>
							{/if}
							<button
								onclick={() => loadFiles(part.path)}
								class="text-blue-400 hover:text-blue-300 cursor-pointer font-mono"
							>
								{part.name}
							</button>
						{/each}
					</div>

					<!-- Toolbar -->
					<div class="flex flex-wrap gap-2 mb-3">
						<input type="file" bind:this={uploadInput} onchange={uploadFile} class="hidden" />
						<button onclick={() => uploadInput.click()} class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer">Upload</button>
						<button onclick={() => { showCreateDir = true; showCreateFile = false; }} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Create Dir</button>
						<button onclick={() => { showCreateFile = true; showCreateDir = false; }} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Create File</button>
						<button onclick={() => loadFiles(currentPath)} disabled={filesLoading} class="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 disabled:opacity-50 text-gray-300 text-xs rounded transition-colors cursor-pointer">
							Refresh
						</button>
					</div>

					<!-- Create Dir Form -->
					{#if showCreateDir}
						<div class="flex items-end gap-2 mb-3 p-3 bg-gray-900 rounded-lg">
							<div>
								<label for="new-dir" class="block text-xs text-gray-400 mb-1">Directory Name</label>
								<input id="new-dir" type="text" bind:value={newDirName} placeholder="new-folder" class="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
							</div>
							<button onclick={createDir} disabled={!newDirName.trim()} class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer">Create</button>
							<button onclick={() => { showCreateDir = false; newDirName = ''; }} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
						</div>
					{/if}

					<!-- Create File Form -->
					{#if showCreateFile}
						<div class="flex items-end gap-2 mb-3 p-3 bg-gray-900 rounded-lg">
							<div>
								<label for="new-file" class="block text-xs text-gray-400 mb-1">File Name</label>
								<input id="new-file" type="text" bind:value={newFileName} placeholder="index.html" class="px-3 py-1.5 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
							</div>
							<button onclick={createFile} disabled={!newFileName.trim()} class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer">Create</button>
							<button onclick={() => { showCreateFile = false; newFileName = ''; }} class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
						</div>
					{/if}

					{#if filesError}
						<div class="mb-2 text-red-400 text-sm">{filesError}</div>
					{/if}

					{#if filesLoading}
						<div class="text-gray-400 text-sm">Loading files...</div>
					{:else if files.length === 0}
						<div class="text-gray-400 text-sm">Empty directory.</div>
					{:else}
						<div class="overflow-x-auto">
							<table class="w-full">
								<thead>
									<tr class="border-b border-gray-700">
										<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
										<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">Type</th>
										<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">Size</th>
										<th class="text-left px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">Permissions</th>
										<th class="text-right px-4 py-2 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-gray-700">
									{#if currentPath !== '/'}
										<tr class="hover:bg-gray-750">
											<td class="px-4 py-2" colspan="5">
												<button onclick={navigateUp} class="text-sm text-blue-400 hover:text-blue-300 cursor-pointer font-mono">..</button>
											</td>
										</tr>
									{/if}
									{#each files as entry}
										<tr class="hover:bg-gray-750">
											<td class="px-4 py-2">
												{#if renamingFile === entry.name}
													<div class="flex items-center gap-2">
														<input type="text" bind:value={renameValue} class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-48" />
														<button onclick={() => renameFile(entry.name)} class="px-2 py-0.5 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded cursor-pointer">OK</button>
														<button onclick={() => { renamingFile = null; renameValue = ''; }} class="px-2 py-0.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded cursor-pointer">X</button>
													</div>
												{:else if entry.type === 'directory'}
													<button onclick={() => navigateTo(entry.name)} class="text-sm text-blue-400 hover:text-blue-300 cursor-pointer font-mono">{entry.name}</button>
												{:else}
													<span class="text-sm text-gray-200 font-mono">{entry.name}</span>
												{/if}
											</td>
											<td class="px-4 py-2">
												{#if entry.type === 'directory'}
													<svg class="w-4 h-4 text-yellow-400 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
														<path stroke-linecap="round" stroke-linejoin="round" d="M2.25 12.75V12A2.25 2.25 0 014.5 9.75h15A2.25 2.25 0 0121.75 12v.75m-8.69-6.44l-2.12-2.12a1.5 1.5 0 00-1.061-.44H4.5A2.25 2.25 0 002.25 6v12a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18V9a2.25 2.25 0 00-2.25-2.25h-5.379a1.5 1.5 0 01-1.06-.44z" />
													</svg>
												{:else}
													<svg class="w-4 h-4 text-gray-400 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
														<path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z" />
													</svg>
												{/if}
											</td>
											<td class="px-4 py-2 text-sm text-gray-400 font-mono">{entry.type === 'file' ? formatSize(entry.size) : '-'}</td>
											<td class="px-4 py-2 text-sm text-gray-400 font-mono">{entry.permissions}</td>
											<td class="px-4 py-2 text-right">
												<div class="flex justify-end gap-1.5">
													{#if entry.type === 'file' && isTextFile(entry.name)}
														<button onclick={() => openFileEdit(entry.name)} class="px-2 py-0.5 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors cursor-pointer">Edit</button>
													{/if}
													<button onclick={() => { renamingFile = entry.name; renameValue = entry.name; }} class="px-2 py-0.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Rename</button>
													{#if entry.type === 'file'}
														<button onclick={() => downloadFile(entry.name)} class="px-2 py-0.5 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Download</button>
													{/if}
													<button onclick={() => deleteFile(entry.name)} class="px-2 py-0.5 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
												</div>
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				{/if}
			{/if}
		</div>
	{/if}
</div>
