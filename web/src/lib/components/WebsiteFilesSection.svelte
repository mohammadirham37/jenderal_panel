<script lang="ts">
	import { onMount } from 'svelte';
	import { api, getCSRFToken } from '$lib/api';
	import { createFileManagerAPI } from '$lib/file-manager.js';
import { toast } from '$lib/stores/toast';

	interface FileEntry {
		name: string;
		path: string;
		is_dir: boolean;
		size: number;
		permissions: string;
		owner: string;
		mod_time: string;
	}

	interface WebsiteLite {
		id: string;
		domain: string;
		web_user: string;
	}

	let { website }: { website: WebsiteLite } = $props();

	// ─── State ────────────────────────────────────────────────────────

	let files = $state<FileEntry[]>([]);
	let currentPath = $state('/');
	let filesLoading = $state(true);
	let filesError = $state('');

	let search = $state('');
	let sortKey = $state<'name' | 'size' | 'time'>('name');
	let sortAsc = $state(true);

	// Editor
	let editingFile = $state<string | null>(null);
	let editFileContent = $state('');
	let editFileSaved = $state('');
	let editFileLoading = $state(false);
	let editSaving = $state(false);

	// Create / rename
	let creating = $state<'dir' | 'file' | null>(null);
	let newName = $state('');
	let newNameInput: HTMLInputElement | undefined = $state();
	let renamingFile = $state<string | null>(null);
	let renameValue = $state('');

	// Upload
	let uploadInput: HTMLInputElement | undefined = $state();
	let dragging = $state(false);
	let uploadQueue = $state<string[]>([]);
	let uploadTotal = $state(0);

	// Delete confirmation
	let pendingDelete = $state<FileEntry | null>(null);

	let fm = $derived(createFileManagerAPI(api, website.id));

	let editDirty = $derived(editFileSaved !== editFileContent);

	let visibleFiles = $derived.by(() => {
		const q = search.trim().toLowerCase();
		const list = q ? files.filter((f) => f.name.toLowerCase().includes(q)) : [...files];
		list.sort((a, b) => {
			if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
			let cmp = 0;
			if (sortKey === 'name') cmp = a.name.localeCompare(b.name);
			else if (sortKey === 'size') cmp = a.size - b.size;
			else cmp = new Date(a.mod_time).getTime() - new Date(b.mod_time).getTime();
			return sortAsc ? cmp : -cmp;
		});
		return list;
	});

	let fileCount = $derived.by(() => {
		const dirs = files.filter((f) => f.is_dir).length;
		return { dirs, files: files.length - dirs };
	});

	// ─── Helpers ──────────────────────────────────────────────────────

	const extensionColors: Record<string, string> = {
		php: 'text-purple-400',
		js: 'text-yellow-400',
		ts: 'text-blue-400',
		json: 'text-amber-400',
		html: 'text-orange-400',
		css: 'text-blue-400',
		md: 'text-gray-300',
		env: 'text-green-400',
		zip: 'text-red-400', gz: 'text-red-400', tar: 'text-red-400',
		png: 'text-green-400', jpg: 'text-green-400', svg: 'text-green-400', webp: 'text-green-400',
		sql: 'text-cyan-400', yml: 'text-cyan-400', yaml: 'text-cyan-400', conf: 'text-cyan-400'
	};

	function fileExt(name: string): string {
		const dot = name.lastIndexOf('.');
		return dot > 0 ? name.slice(dot + 1).toLowerCase() : '';
	}

	function fileIcon(entry: FileEntry): { cls: string; label: string } {
		if (entry.is_dir) return { cls: 'text-blue-400', label: 'DIR' };
		const ext = fileExt(entry.name);
		return { cls: extensionColors[ext] || 'text-gray-400', label: ext ? ext.slice(0, 4).toUpperCase() : 'FILE' };
	}

	function formatSize(bytes: number): string {
		if (bytes === 0) return '—';
		const units = ['B', 'KB', 'MB', 'GB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function formatTime(ts: string): string {
		if (!ts) return '—';
		const d = new Date(ts);
		if (isNaN(d.getTime())) return '—';
		return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) + ' ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	}

	function isTextFile(name: string): boolean {
		const ext = fileExt(name);
		return ['txt','html','css','js','ts','json','xml','yml','yaml','md','conf','cfg','ini','log','sh','bash','php','py','rb','env','htaccess','svg','lock','gitignore','example'].includes(ext) || name.startsWith('.env');
	}

	function joinPath(name: string): string {
		return currentPath === '/' ? `/${name}` : `${currentPath}/${name}`;
	}

	function breadcrumbParts(): { name: string; path: string }[] {
		const parts = currentPath.split('/').filter(Boolean);
		const result = [{ name: website.web_user, path: '/' }];
		let accumulated = '';
		for (const part of parts) {
			accumulated += '/' + part;
			result.push({ name: part, path: accumulated });
		}
		return result;
	}

	function flash(msg: string) {
		toast.success(msg);
	}

	function fail(err: unknown, fallback: string) {
		toast.error(err instanceof Error ? err.message : fallback);
	}

	function toggleSort(key: 'name' | 'size' | 'time') {
		if (sortKey === key) sortAsc = !sortAsc;
		else { sortKey = key; sortAsc = true; }
	}

	// ─── API ──────────────────────────────────────────────────────────

	async function loadFiles(path?: string) {
		// The tab always opens at the website root: the web user's home
		// (e.g. web_example_com), not the document root subfolder.
		const requested = path ?? '/';
		filesLoading = true;
		filesError = '';
		try {
			const data = await fm.browse(requested) as FileEntry[];
			files = data || [];
			currentPath = requested;
			search = '';
		} catch (err) {
			filesError = err instanceof Error ? err.message : 'Failed to load files';
		} finally {
			filesLoading = false;
		}
	}

	async function openFileEdit(entry: FileEntry) {
		editFileLoading = true;
		editingFile = joinPath(entry.name);
		editFileContent = '';
		editFileSaved = '';
		try {
			const data = await fm.read(editingFile) as { content: string };
			editFileContent = data.content || '';
			editFileSaved = editFileContent;
		} catch (err) {
			fail(err, 'Failed to read file');
			editingFile = null;
		} finally {
			editFileLoading = false;
		}
	}

	async function saveFileEdit() {
		if (!editingFile || editSaving) return;
		editSaving = true;
		try {
			await fm.write(editingFile, editFileContent);
			editFileSaved = editFileContent;
			flash('File saved.');
			await loadFiles(currentPath);
		} catch (err) {
			fail(err, 'Failed to save file');
		} finally {
			editSaving = false;
		}
	}

	function closeEditor(force = false) {
		if (editDirty && !force && !confirm('Discard unsaved changes?')) return;
		editingFile = null;
	}

	async function confirmDelete() {
		if (!pendingDelete) return;
		const entry = pendingDelete;
		pendingDelete = null;
		try {
			await fm.remove(joinPath(entry.name));
			flash(`"${entry.name}" deleted.`);
			await loadFiles(currentPath);
		} catch (err) {
			fail(err, 'Failed to delete');
		}
	}

	async function createEntry() {
		const name = newName.trim();
		if (!name || !creating) return;
		const target = joinPath(name);
		try {
			if (creating === 'dir') await fm.mkdir(target);
			else await fm.write(target, '');
			flash(`${creating === 'dir' ? 'Directory' : 'File'} "${name}" created.`);
			creating = null;
			newName = '';
			await loadFiles(currentPath);
		} catch (err) {
			fail(err, `Failed to create ${creating}`);
		}
	}

	async function renameFile(oldName: string) {
		const name = renameValue.trim();
		if (!name || name === oldName) { renamingFile = null; return; }
		try {
			await fm.rename(joinPath(oldName), joinPath(name));
			flash(`Renamed "${oldName}" to "${name}".`);
			renamingFile = null;
			await loadFiles(currentPath);
		} catch (err) {
			fail(err, 'Failed to rename');
		}
	}

	async function uploadFiles(list: FileList | File[]) {
		const arr = Array.from(list);
		if (arr.length === 0) return;
		uploadTotal = arr.length;
		uploadQueue = arr.map((f) => f.name);
		for (const file of arr) {
			try {
				const formData = new FormData();
				formData.append('file', file);
				formData.append('path', currentPath);
				const res = await fetch(`/api/v1/websites/${website.id}/files/upload`, {
					method: 'POST',
					headers: { 'X-CSRF-Token': getCSRFToken() },
					credentials: 'include',
					body: formData
				});
				if (!res.ok) throw new Error(`${file.name}: HTTP ${res.status}`);
				uploadQueue = uploadQueue.slice(1);
			} catch (err) {
				uploadQueue = [];
				uploadTotal = 0;
				fail(err, `Failed to upload "${file.name}"`);
				await loadFiles(currentPath);
				return;
			}
		}
		flash(uploadTotal === 1 ? `"${arr[0].name}" uploaded.` : `${arr.length} files uploaded.`);
		uploadQueue = [];
		uploadTotal = 0;
		await loadFiles(currentPath);
	}

	function downloadFile(entry: FileEntry) {
		window.open(`/api/v1/websites/${website.id}/files/download?path=${encodeURIComponent(joinPath(entry.name))}`, '_blank');
	}

	function handleEditorKeydown(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key === 's') {
			e.preventDefault();
			saveFileEdit();
		} else if (e.key === 'Escape') {
			closeEditor();
		}
	}

	function handleDrop(e: DragEvent) {
		e.preventDefault();
		dragging = false;
		if (e.dataTransfer?.files?.length) uploadFiles(e.dataTransfer.files);
	}

	async function startCreate(kind: 'dir' | 'file') {
		creating = kind;
		newName = '';
		await tickFocus();
	}

	async function tickFocus() {
		await new Promise((r) => setTimeout(r, 0));
		newNameInput?.focus();
	}

	onMount(() => loadFiles());
</script>

<svelte:window
	onkeydown={(e) => {
		if (editingFile && (e.metaKey || e.ctrlKey) && e.key === 's') {
			e.preventDefault();
			saveFileEdit();
		}
	}}
/>

<div class="space-y-4">

	{#if editingFile}
		<!-- ─── Editor ─────────────────────────────────────────────── -->
		<div class="overflow-hidden rounded-xl border border-gray-700 bg-gray-800">
			<div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-700 bg-gray-900/60 px-4 py-3">
				<div class="flex min-w-0 items-center gap-2.5">
					<span class="rounded bg-blue-900/50 px-1.5 py-0.5 text-[10px] font-bold text-blue-300">{fileExt(editingFile).toUpperCase() || 'FILE'}</span>
					<span class="truncate font-mono text-sm text-gray-200">{editingFile}</span>
					{#if editDirty}
						<span class="shrink-0 rounded-full bg-yellow-900/50 px-2 py-0.5 text-[10px] font-semibold text-yellow-300">● unsaved</span>
					{/if}
				</div>
				<div class="flex items-center gap-2">
					<span class="hidden text-[11px] text-gray-500 sm:inline">Ctrl+S to save</span>
					<button
						onclick={saveFileEdit}
						disabled={editSaving || editFileLoading || !editDirty}
						class="cursor-pointer rounded-lg bg-blue-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
					>
						{editSaving ? 'Saving…' : 'Save'}
					</button>
					<button
						onclick={() => closeEditor()}
						class="cursor-pointer rounded-lg bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
					>
						Close
					</button>
				</div>
			</div>
			{#if editFileLoading}
				<div class="p-8 text-center text-sm text-gray-400">Loading file…</div>
			{:else}
				<textarea
					bind:value={editFileContent}
					onkeydown={handleEditorKeydown}
					spellcheck="false"
					class="h-[60vh] w-full resize-none bg-gray-950 p-4 font-mono text-[13px] leading-relaxed text-gray-200 focus:outline-none"
				></textarea>
				<div class="flex justify-between border-t border-gray-700 bg-gray-900/60 px-4 py-1.5 text-[11px] text-gray-500">
					<span>{editFileContent.split('\n').length} lines · {editFileContent.length} chars</span>
					<span>{formatSize(new Blob([editFileContent]).size)}</span>
				</div>
			{/if}
		</div>
	{:else}
		<!-- ─── Toolbar ────────────────────────────────────────────── -->
		<div class="rounded-xl border border-gray-700 bg-gray-800">
			<div class="flex flex-wrap items-center gap-2 border-b border-gray-700 px-3 py-2.5">
				<!-- Breadcrumb -->
				<nav class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto text-sm" aria-label="File path">
					{#each breadcrumbParts() as part, i}
						{#if i > 0}<span class="text-gray-600">/</span>{/if}
						<button
							onclick={() => loadFiles(part.path)}
							class="shrink-0 cursor-pointer rounded px-1.5 py-0.5 font-mono transition
							{i === breadcrumbParts().length - 1 ? 'bg-gray-700 text-white' : 'text-blue-400 hover:bg-gray-700 hover:text-blue-300'}"
						>{part.name}</button>
					{/each}
				</nav>
				<button
					onclick={() => loadFiles(currentPath)}
					disabled={filesLoading}
					title="Refresh"
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 p-1.5 text-gray-300 transition hover:text-white disabled:opacity-50"
				>
					<svg class="h-4 w-4 {filesLoading ? 'animate-spin' : ''}" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
						<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992" />
					</svg>
				</button>
			</div>

			<div class="flex flex-wrap items-center gap-2 border-b border-gray-700 px-3 py-2.5">
				<!-- Search -->
				<div class="relative min-w-40 flex-1">
					<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
						<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
					</svg>
					<input
						bind:value={search}
						type="text"
						placeholder="Filter in this folder…"
						aria-label="Filter files"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				<input type="file" multiple bind:this={uploadInput} onchange={(e) => { const el = e.currentTarget; if (el.files) uploadFiles(el.files); el.value = ''; }} class="hidden" />
				<button
					onclick={() => uploadInput?.click()}
					disabled={uploadTotal > 0}
					class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
				>Upload</button>
				<button
					onclick={() => startCreate('file')}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
				>+ File</button>
				<button
					onclick={() => startCreate('dir')}
					class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600"
				>+ Folder</button>
			</div>

			{#if uploadTotal > 0}
				<div class="border-b border-gray-700 bg-blue-900/20 px-4 py-2 text-xs text-blue-300">
					Uploading {uploadTotal - uploadQueue.length + (uploadQueue.length ? 1 : 0)}/{uploadTotal}
					{#if uploadQueue.length > 0}— {uploadQueue[0]}{/if}
				</div>
			{/if}

			{#if creating}
				<form
					onsubmit={(e) => { e.preventDefault(); createEntry(); }}
					class="flex items-center gap-2 border-b border-gray-700 bg-gray-900/60 px-4 py-2.5"
				>
					<span class="text-xs text-gray-400">{creating === 'dir' ? 'Folder' : 'File'} name:</span>
					<input
						bind:this={newNameInput}
						bind:value={newName}
						type="text"
						placeholder={creating === 'dir' ? 'new-folder' : 'index.html'}
						class="flex-1 rounded-lg border border-gray-600 bg-gray-900 px-3 py-1.5 text-sm font-mono text-gray-200 focus:border-blue-500 focus:outline-none"
					/>
					<button type="submit" disabled={!newName.trim()} class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-40">Create</button>
					<button type="button" onclick={() => (creating = null)} class="cursor-pointer rounded-lg bg-gray-700 px-3 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600">Cancel</button>
				</form>
			{/if}

			<!-- ─── File list ─────────────────────────────────────────── -->
			{#if filesError}
				<div class="px-4 py-6 text-center text-sm text-red-400">{filesError}</div>
			{:else if filesLoading}
				<div class="divide-y divide-gray-700/50">
					{#each Array(6) as _}
						<div class="flex items-center gap-3 px-4 py-2.5">
							<div class="h-8 w-8 animate-pulse rounded-lg bg-gray-700/50"></div>
							<div class="h-4 w-48 animate-pulse rounded bg-gray-700/50"></div>
							<div class="ml-auto h-4 w-16 animate-pulse rounded bg-gray-700/50"></div>
						</div>
					{/each}
				</div>
			{:else}
				<!-- Sort header -->
				<div class="flex items-center gap-3 border-b border-gray-700 px-4 py-2 text-[11px] font-medium uppercase tracking-wider text-gray-500">
					<button onclick={() => toggleSort('name')} class="flex flex-1 cursor-pointer items-center gap-1 text-left hover:text-gray-300">
						Name
						{#if sortKey === 'name'}<span>{sortAsc ? '↑' : '↓'}</span>{/if}
					</button>
					<button onclick={() => toggleSort('size')} class="hidden w-20 cursor-pointer items-center gap-1 text-right hover:text-gray-300 sm:flex">
						Size {#if sortKey === 'size'}<span>{sortAsc ? '↑' : '↓'}</span>{/if}
					</button>
					<button onclick={() => toggleSort('time')} class="hidden w-32 cursor-pointer items-center gap-1 text-right hover:text-gray-300 md:flex">
						Modified {#if sortKey === 'time'}<span>{sortAsc ? '↑' : '↓'}</span>{/if}
					</button>
					<span class="w-32 text-right">Actions</span>
				</div>

				<div
					class="divide-y divide-gray-700/50"
					role="region"
					aria-label="Drop files to upload"
					ondragover={(e) => { e.preventDefault(); dragging = true; }}
					ondragleave={() => (dragging = false)}
					ondrop={handleDrop}
				>
					{#if currentPath !== '/' && !search}
						<button onclick={() => loadFiles(currentPath.split('/').slice(0, -1).join('/') || '/')} class="group flex w-full cursor-pointer items-center gap-3 px-4 py-2.5 text-left transition hover:bg-gray-750">
							<span class="flex h-8 w-8 items-center justify-center rounded-lg bg-gray-700/60 text-gray-400">↩</span>
							<span class="text-sm font-mono text-gray-400 group-hover:text-gray-200">..</span>
						</button>
					{/if}

					{#each visibleFiles as entry (entry.path)}
						<div class="group flex items-center gap-3 px-4 py-2.5 transition hover:bg-gray-750">
							<span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-gray-900/70 text-[9px] font-bold {fileIcon(entry).cls}">
								{fileIcon(entry).label}
							</span>

							{#if renamingFile === entry.name}
								<form
									onsubmit={(e) => { e.preventDefault(); renameFile(entry.name); }}
									class="flex flex-1 items-center gap-2"
								>
									<input
										bind:value={renameValue}
										type="text"
										class="w-full max-w-sm rounded-lg border border-blue-500 bg-gray-900 px-2.5 py-1 font-mono text-sm text-gray-200 focus:outline-none"
									/>
									<button type="submit" class="cursor-pointer rounded bg-blue-600 px-2.5 py-1 text-xs font-semibold text-white hover:bg-blue-700">Save</button>
									<button type="button" onclick={() => (renamingFile = null)} class="cursor-pointer rounded bg-gray-700 px-2.5 py-1 text-xs text-gray-200 hover:bg-gray-600">Cancel</button>
								</form>
							{:else}
								<button
									onclick={() => (entry.is_dir ? loadFiles(joinPath(entry.name)) : (isTextFile(entry.name) && entry.size < 2_000_000 ? openFileEdit(entry) : undefined))}
									class="min-w-0 flex-1 cursor-pointer text-left"
									title={entry.is_dir ? 'Open folder' : isTextFile(entry.name) ? 'Click to edit' : entry.name}
								>
									<span class="block truncate text-sm font-mono {entry.is_dir ? 'text-blue-400 hover:text-blue-300' : 'text-gray-200'}">{entry.name}</span>
									<span class="block text-[10px] text-gray-500">{entry.owner} · {entry.permissions}</span>
								</button>
							{/if}

							<span class="hidden w-20 shrink-0 text-right text-xs tabular-nums text-gray-400 sm:block">{entry.is_dir ? '—' : formatSize(entry.size)}</span>
							<span class="hidden w-32 shrink-0 text-right text-xs text-gray-500 md:block">{formatTime(entry.mod_time)}</span>

							<div class="flex w-32 shrink-0 items-center justify-end gap-1 opacity-0 transition group-hover:opacity-100 focus-within:opacity-100">
								{#if !entry.is_dir && isTextFile(entry.name)}
									<button onclick={() => openFileEdit(entry)} title="Edit" class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-600 hover:text-white">
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L6.832 19.82a4.5 4.5 0 01-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 011.13-1.897L16.863 4.487z" /></svg>
									</button>
								{/if}
								{#if !entry.is_dir}
									<button onclick={() => downloadFile(entry)} title="Download" class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-600 hover:text-white">
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3" /></svg>
									</button>
								{/if}
								<button onclick={() => { renamingFile = entry.name; renameValue = entry.name; }} title="Rename" class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-600 hover:text-white">
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" /></svg>
								</button>
								<button onclick={() => (pendingDelete = entry)} title="Delete" class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white">
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" /></svg>
								</button>
							</div>
						</div>
					{:else}
						{#if search}
							<div class="px-4 py-8 text-center text-sm text-gray-500">No files match "{search}".</div>
						{:else}
							<div class="px-4 py-10 text-center">
								<p class="text-sm text-gray-400">This folder is empty.</p>
								<p class="mt-1 text-xs text-gray-500">Drop files here or use the Upload button.</p>
							</div>
						{/if}
					{/each}
				</div>

				<div class="flex items-center justify-between border-t border-gray-700 bg-gray-900/40 px-4 py-2 text-[11px] text-gray-500">
					<span>{fileCount.dirs} folders · {fileCount.files} files</span>
					<span class="hidden sm:inline">Drag &amp; drop files anywhere in the list to upload</span>
				</div>
			{/if}
		</div>

		{#if dragging}
			<div class="pointer-events-none fixed inset-0 z-50 flex items-center justify-center bg-blue-950/60 backdrop-blur-sm">
				<div class="rounded-2xl border-2 border-dashed border-blue-400 bg-blue-900/40 px-10 py-8 text-center">
					<svg class="mx-auto h-10 w-10 text-blue-300" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M12 16.5V9.75m0 0l3 3m-3-3l-3 3M6.75 19.5a4.5 4.5 0 01-1.41-8.775 5.25 5.25 0 0110.233-2.33 3 3 0 013.758 3.848A3.752 3.752 0 0118 19.5H6.75z" /></svg>
					<p class="mt-3 text-sm font-semibold text-blue-100">Drop files to upload to <span class="font-mono">{currentPath}</span></p>
				</div>
			</div>
		{/if}
	{/if}

	<!-- Delete confirmation -->
	{#if pendingDelete}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-label="Confirm delete">
			<div class="w-full max-w-sm rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
				<h4 class="text-base font-semibold text-white">Delete "{pendingDelete.name}"?</h4>
				<p class="mt-2 text-sm text-gray-400">
					{pendingDelete.is_dir
						? 'This folder and everything inside it will be permanently deleted.'
						: 'This file will be permanently deleted.'}
					This cannot be undone.
				</p>
				<div class="mt-4 flex justify-end gap-2">
					<button onclick={() => (pendingDelete = null)} class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">Cancel</button>
					<button onclick={confirmDelete} class="cursor-pointer rounded-lg bg-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-red-700">Delete</button>
				</div>
			</div>
		</div>
	{/if}
</div>
