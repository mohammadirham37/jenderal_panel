<script lang="ts">
	import { page } from '$app/state';
	import { api, getCSRFToken } from '$lib/api';

	// ─── Types ────────────────────────────────────────────────────────

	interface ManagedTable {
		name: string;
		engine: string;
		rows: number;
		bytes: number;
		comment: string;
	}

	interface ManagedColumn {
		name: string;
		type: string;
		nullable: boolean;
		key: string;
		default: string;
	}

	interface ManagedRows {
		columns: string[];
		rows: (string | null)[][];
		total: number;
		page: number;
		per_page: number;
		total_pages: number;
	}

	interface QueryResult {
		is_select: boolean;
		columns: string[];
		rows: (string | null)[][];
		affected: number;
		tag: string;
		capped: boolean;
	}

	// ─── State ────────────────────────────────────────────────────────

	const userId = page.params.userId;
	const tokenKey = `dbmanage-token-${userId}`;

	let token = $state(sessionStorage.getItem(tokenKey) || '');
	let unlockedUser = $state<{ username: string; engine: string } | null>(null);
	let sessionError = $state('');

	// Unlock form
	let unlockPassword = $state('');
	let unlockBusy = $state(false);
	let unlockError = $state('');

	// Data
	let databases = $state<string[]>([]);
	let tables = $state<ManagedTable[]>([]);
	let tableFilter = $state('');

	// Selection
	let selectedDb = $state('');
	let selectedTable = $state('');
	let viewMode = $state<'tables' | 'sql'>('tables');
	let tableTab = $state<'browse' | 'structure'>('browse');

	// Browse state
	let browse = $state<ManagedRows | null>(null);
	let browseLoading = $state(false);
	let browseError = $state('');
	let browsePage = $state(1);
	let browsePerPage = $state(25);
	let browseSort = $state('');
	let browseDesc = $state(false);
	let browseSearch = $state('');
	let browseSearchInput = $state('');

	// Structure
	let structure = $state<ManagedColumn[]>([]);
	let structureLoading = $state(false);

	// SQL console
	let sqlText = $state('');
	let sqlRunning = $state(false);
	let sqlResult = $state<QueryResult | null>(null);
	let sqlError = $state('');

	// Modals + feedback
	let confirmAction = $state<'empty' | 'drop' | null>(null);
	let confirmBusy = $state(false);
	let toastMsg = $state('');
	let toastError = $state('');

	let toastTimer: ReturnType<typeof setTimeout> | undefined;

	// ─── Helpers ──────────────────────────────────────────────────────

	function toast(msg: string, isError = false) {
		if (isError) {
			toastError = msg;
			toastMsg = '';
		} else {
			toastMsg = msg;
			toastError = '';
		}
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => { toastMsg = ''; toastError = ''; }, 4000);
	}

	async function mapi<T>(path: string, opts?: { method?: string; body?: unknown }): Promise<T> {
		const res = await fetch(`/api/v1/databases/manage/${token}${path}`, {
			method: opts?.method || 'GET',
			headers: {
				'Content-Type': 'application/json',
				'X-DB-Manage-Token': token,
				'X-CSRF-Token': getCSRFToken()
			},
			credentials: 'include',
			body: opts?.body !== undefined ? JSON.stringify(opts.body) : undefined
		});
		const json = await res.json().catch(() => null);
		if (!res.ok) {
			const code = json?.error?.code || '';
			if (code === 'MANAGE_SESSION_EXPIRED' || res.status === 401) {
				lockSession(true);
				const message = json?.error?.message || 'Management session expired.';
				throw new Error(message);
			}
			throw new Error(json?.error?.message || `Request failed (HTTP ${res.status})`);
		}
		return json?.data as T;
	}

	function lockSession(expired = false) {
		if (token && !expired) {
			fetch(`/api/v1/databases/manage/${token}/lock`, {
				method: 'POST',
				headers: { 'X-DB-Manage-Token': token, 'X-CSRF-Token': getCSRFToken() },
				credentials: 'include'
			}).catch(() => undefined);
		}
		sessionStorage.removeItem(tokenKey);
		token = '';
		unlockedUser = null;
		databases = [];
		tables = [];
		selectedDb = '';
		selectedTable = '';
		if (expired) sessionError = 'Management session expired. Unlock again to continue.';
	}

	async function unlock() {
		if (!unlockPassword || unlockBusy) return;
		unlockBusy = true;
		unlockError = '';
		sessionError = '';
		try {
			const res = await fetch(`/api/v1/databases/users/${userId}/manage/unlock`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
				credentials: 'include',
				body: JSON.stringify({ password: unlockPassword })
			});
			const json = await res.json().catch(() => null);
			if (!res.ok) throw new Error(json?.error?.message || 'Unlock failed');
			token = json.data.token;
			unlockedUser = { username: json.data.username, engine: json.data.engine };
			sessionStorage.setItem(tokenKey, token);
			unlockPassword = '';
			await loadDatabases();
		} catch (err) {
			unlockError = err instanceof Error ? err.message : 'Unlock failed';
		} finally {
			unlockBusy = false;
		}
	}

	async function loadDatabases() {
		databases = await mapi<string[]>('/databases');
		if (databases.length > 0) {
			selectedDb = databases[0];
			await loadTables();
		}
	}

	async function loadTables() {
		if (!selectedDb) return;
		tables = await mapi<ManagedTable[]>(`/tables?database=${encodeURIComponent(selectedDb)}`);
	}

	function pickDatabase(name: string) {
		selectedDb = name;
		selectedTable = '';
		viewMode = 'tables';
		tables = [];
		loadTables().catch((err) => toast(err.message, true));
	}

	function pickTable(name: string) {
		selectedTable = name;
		viewMode = 'tables';
		tableTab = 'browse';
		browsePage = 1;
		browseSort = '';
		browseDesc = false;
		browseSearch = '';
		browseSearchInput = '';
		loadStructure().catch((err) => toast(err.message, true));
		loadBrowse().catch((err) => toast(err.message, true));
	}

	async function loadStructure() {
		if (!selectedDb || !selectedTable) return;
		structureLoading = true;
		try {
			structure = await mapi<ManagedColumn[]>(
				`/structure?database=${encodeURIComponent(selectedDb)}&table=${encodeURIComponent(selectedTable)}`
			);
		} finally {
			structureLoading = false;
		}
	}

	async function loadBrowse() {
		if (!selectedDb || !selectedTable) return;
		browseLoading = true;
		browseError = '';
		try {
			browse = await mapi<ManagedRows>(
				`/rows?database=${encodeURIComponent(selectedDb)}&table=${encodeURIComponent(selectedTable)}` +
				`&page=${browsePage}&per_page=${browsePerPage}` +
				(browseSort ? `&sort=${encodeURIComponent(browseSort)}&dir=${browseDesc ? 'desc' : 'asc'}` : '') +
				(browseSearch ? `&search=${encodeURIComponent(browseSearch)}` : '')
			);
		} catch (err) {
			browseError = err instanceof Error ? err.message : 'Failed to load rows';
			browse = null;
		} finally {
			browseLoading = false;
		}
	}

	function submitBrowseSearch(e: Event) {
		e.preventDefault();
		browseSearch = browseSearchInput;
		browsePage = 1;
		loadBrowse().catch((err) => toast(err.message, true));
	}

	function sortBy(column: string) {
		if (browseSort === column) browseDesc = !browseDesc;
		else { browseSort = column; browseDesc = false; }
		browsePage = 1;
		loadBrowse().catch((err) => toast(err.message, true));
	}

	function goToPage(p: number) {
		browsePage = Math.min(Math.max(1, p), Number(browse?.total_pages || 1));
		loadBrowse().catch((err) => toast(err.message, true));
	}

	async function runSql() {
		if (!sqlText.trim() || sqlRunning) return;
		sqlRunning = true;
		sqlError = '';
		sqlResult = null;
		try {
			sqlResult = await mapi<QueryResult>('/query', {
				method: 'POST',
				body: { database: selectedDb, sql: sqlText }
			});
		} catch (err) {
			sqlError = err instanceof Error ? err.message : 'Query failed';
		} finally {
			sqlRunning = false;
		}
	}

	function handleSqlKeydown(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
			e.preventDefault();
			runSql();
		}
	}

	async function runTableAction(action: 'empty' | 'drop') {
		if (!confirmAction || confirmBusy) return;
		confirmBusy = true;
		try {
			if (action === 'empty') {
				await mapi('/empty-table', { method: 'POST', body: { database: selectedDb, table: selectedTable } });
				toast(`Table "${selectedTable}" emptied.`);
			} else {
				await mapi('/drop-table', { method: 'POST', body: { database: selectedDb, table: selectedTable } });
				toast(`Table "${selectedTable}" dropped.`);
				selectedTable = '';
				structure = [];
				browse = null;
			}
			confirmAction = null;
			await loadTables();
		} catch (err) {
			toast(err instanceof Error ? err.message : 'Action failed', true);
		} finally {
			confirmBusy = false;
		}
	}

	function formatSize(bytes: number): string {
		if (!bytes || bytes <= 0) return '—';
		const units = ['B', 'KB', 'MB', 'GB'];
		let i = 0;
		let size = bytes;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function exportCsv() {
		if (!browse) return;
		const escape = (v: string | null) => {
			const s = v === null ? '' : v;
			return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
		};
		const lines = [browse.columns.map(escape).join(',')];
		for (const row of browse.rows) lines.push(row.map((c) => escape(c === null ? null : c)).join(','));
		const blob = new Blob([lines.join('\n')], { type: 'text/csv' });
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = `${selectedTable}.csv`;
		a.click();
		URL.revokeObjectURL(a.href);
	}

	let filteredTables = $derived.by(() => {
		const q = tableFilter.trim().toLowerCase();
		return q ? tables.filter((t) => t.name.toLowerCase().includes(q)) : tables;
	});

	let tableSearchDebounce: ReturnType<typeof setTimeout> | undefined;
	function onSearchInput() {
		clearTimeout(tableSearchDebounce);
		tableSearchDebounce = setTimeout(() => {
			browseSearch = browseSearchInput;
			browsePage = 1;
			loadBrowse().catch((err) => toast(err.message, true));
		}, 400);
	}
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape' && confirmAction) confirmAction = null;
	}}
/>

{#if !token}
	<!-- ─── Unlock gate ────────────────────────────────────────────── -->
	<div class="mx-auto mt-10 max-w-md">
		<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-8">
			<div class="mb-6 text-center">
				<span class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-blue-500/10 text-blue-400">
					<svg class="h-7 w-7" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
					</svg>
				</span>
				<h2 class="mt-4 text-xl font-bold text-white">Database Management</h2>
				<p class="mt-2 text-sm text-gray-400">
					Enter this database user's password to open a management session
					(similar to signing in to phpMyAdmin). The session expires after 60 minutes of inactivity.
				</p>
			</div>

			{#if sessionError}
				<div class="mb-4 rounded-xl border border-yellow-700 bg-yellow-900/30 px-3.5 py-2.5 text-sm text-yellow-300">
					{sessionError}
				</div>
			{/if}

			<form
				onsubmit={(e) => { e.preventDefault(); unlock(); }}
				class="space-y-4"
			>
				<div>
					<label for="manage-pass" class="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
						Database user password
					</label>
					<input
						id="manage-pass"
						type="password"
						bind:value={unlockPassword}
						autocomplete="current-password"
						placeholder="••••••••••••"
						class="w-full rounded-xl border border-gray-600 bg-gray-900 px-4 py-3 text-sm text-gray-200 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				{#if unlockError}
					<p class="text-sm text-red-400">{unlockError}</p>
				{/if}
				<button
					type="submit"
					disabled={unlockBusy || !unlockPassword}
					class="w-full cursor-pointer rounded-xl bg-blue-600 px-4 py-3 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
				>
					{unlockBusy ? 'Unlocking…' : 'Unlock Management'}
				</button>
			</form>
			<a href="/databases" class="mt-4 block text-center text-xs text-gray-500 transition hover:text-gray-300">← Back to Databases</a>
		</div>
	</div>
{:else}
	<!-- ─── Management workspace ───────────────────────────────────── -->
	<div class="flex h-full min-h-[calc(100vh-8rem)] gap-4">
		<!-- Sidebar -->
		<aside class="flex w-72 shrink-0 flex-col overflow-hidden rounded-2xl border border-white/5 bg-gray-800/60">
			<div class="border-b border-white/5 px-4 py-3">
				<div class="flex items-center justify-between gap-2">
					<div class="min-w-0">
						<p class="truncate font-mono text-sm font-semibold text-white">{unlockedUser?.username || 'session'}</p>
						<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-blue-400">
							{unlockedUser?.engine || ''} management
						</p>
					</div>
					<button
						type="button"
						onclick={() => lockSession(false)}
						title="End management session"
						class="shrink-0 cursor-pointer rounded-lg border border-gray-600 bg-gray-700 p-1.5 text-gray-300 transition hover:border-red-400/40 hover:bg-red-600/20 hover:text-red-300"
					>
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9" />
						</svg>
					</button>
				</div>
			</div>

			<div class="border-b border-white/5 px-4 py-3">
				<label for="manage-db" class="mb-1 block text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">Database</label>
				<select
					id="manage-db"
					bind:value={selectedDb}
					onchange={() => pickDatabase(selectedDb)}
					class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-2 font-mono text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
				>
					{#each databases as name (name)}
						<option value={name}>{name}</option>
					{/each}
				</select>
				<button
					type="button"
					onclick={() => { viewMode = 'sql'; }}
					class="mt-2.5 flex w-full cursor-pointer items-center justify-center gap-2 rounded-lg border border-blue-400/30 bg-blue-500/10 px-3 py-2 text-xs font-semibold text-blue-300 transition hover:bg-blue-500/20
					{viewMode === 'sql' ? 'ring-2 ring-blue-500/40' : ''}"
				>
					<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6.75 7.5l3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0021 18V6a2.25 2.25 0 00-2.25-2.25H5.25A2.25 2.25 0 003 6v12a2.25 2.25 0 002.25 2.25z" />
					</svg>
					SQL Console
				</button>
			</div>

			<div class="px-3 py-2.5">
				<input
					type="text"
					bind:value={tableFilter}
					placeholder="Filter tables…"
					aria-label="Filter tables"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none"
				/>
			</div>

			<nav class="flex-1 overflow-y-auto px-2 pb-2" aria-label="Tables">
				{#each filteredTables as t (t.name)}
					<button
						type="button"
						onclick={() => pickTable(t.name)}
						class="mb-0.5 flex w-full cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left transition
						{selectedTable === t.name && viewMode === 'tables' ? 'bg-blue-500/15 text-blue-100' : 'text-gray-300 hover:bg-white/5'}"
					>
						<span class="min-w-0 truncate font-mono text-xs">{t.name}</span>
						<span class="shrink-0 text-[10px] tabular-nums text-gray-500">{t.rows > 0 ? t.rows : ''}</span>
					</button>
				{:else}
					<p class="px-2 py-4 text-xs text-gray-500">{tableFilter ? 'No tables match.' : 'No tables in this database.'}</p>
				{/each}
			</nav>
		</aside>

		<!-- Main panel -->
		<div class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-2xl border border-white/5 bg-gray-800/60">
			{#if toastMsg || toastError}
				<div class="flex items-center justify-between gap-2 border-b px-4 py-2 text-sm {toastError ? 'border-red-700/50 bg-red-900/30 text-red-300' : 'border-green-700/50 bg-green-900/30 text-green-300'}">
					<span>{toastError || toastMsg}</span>
					<button type="button" onclick={() => { toastMsg = ''; toastError = ''; }} class="cursor-pointer opacity-70 hover:opacity-100">✕</button>
				</div>
			{/if}

			{#if viewMode === 'sql'}
				<!-- SQL console -->
				<div class="flex items-center justify-between border-b border-white/5 px-5 py-3">
					<h3 class="text-sm font-semibold text-white">
						SQL Console — <span class="font-mono text-blue-300">{selectedDb || 'no database'}</span>
					</h3>
					<span class="text-[11px] text-gray-500">Ctrl+Enter to run</span>
				</div>
				<div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-5">
					<textarea
						bind:value={sqlText}
						onkeydown={handleSqlKeydown}
						spellcheck="false"
						placeholder="SELECT * FROM your_table LIMIT 10;"
						class="h-40 w-full shrink-0 resize-y rounded-xl border border-gray-600 bg-gray-950 p-4 font-mono text-[13px] text-gray-200 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					></textarea>
					<div class="flex items-center gap-2">
						<button
							type="button"
							onclick={runSql}
							disabled={sqlRunning || !sqlText.trim() || !selectedDb}
							class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
						>
							{sqlRunning ? 'Running…' : 'Run (Ctrl+Enter)'}
						</button>
						{#if sqlResult?.capped}
							<span class="text-[11px] text-yellow-400">Results capped at 1,000 rows.</span>
						{/if}
					</div>

					{#if sqlError}
						<div class="rounded-xl border border-red-700 bg-red-900/30 p-3.5 font-mono text-xs text-red-300">{sqlError}</div>
					{:else if sqlResult}
						{#if sqlResult.is_select}
							<div class="overflow-x-auto rounded-xl border border-gray-700">
								<table class="w-full text-xs">
									<thead>
										<tr class="border-b border-gray-700 bg-gray-900/80">
											{#each sqlResult.columns as col}
												<th class="whitespace-nowrap px-3 py-2 text-left font-semibold text-gray-300">{col}</th>
											{/each}
										</tr>
									</thead>
									<tbody class="divide-y divide-gray-700/40">
										{#each sqlResult.rows as row}
											<tr class="hover:bg-gray-750">
												{#each row as cell}
													<td class="max-w-xs truncate px-3 py-1.5 font-mono text-gray-300" title={cell ?? 'NULL'}>
														{#if cell === null}<span class="italic text-gray-600">NULL</span>{:else}{cell}{/if}
													</td>
												{/each}
											</tr>
										{:else}
											<tr><td class="px-3 py-3 text-center text-gray-500" colspan="{sqlResult.columns.length || 1}">No rows returned.</td></tr>
										{/each}
									</tbody>
								</table>
							</div>
							<p class="text-[11px] text-gray-500">{sqlResult.rows.length} rows</p>
						{:else}
							<div class="rounded-xl border border-green-700/50 bg-green-900/20 p-4 text-sm text-green-300">
								{sqlResult.tag || 'OK'}
								{#if sqlResult.affected > 0}— {sqlResult.affected} row(s) affected{/if}
							</div>
						{/if}
					{/if}
				</div>
			{:else if selectedTable}
				<!-- Table view -->
				<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-3">
					<div class="flex items-center gap-2.5">
						<h3 class="font-mono text-sm font-semibold text-white">{selectedTable}</h3>
						<span class="text-[11px] text-gray-500">in {selectedDb}</span>
					</div>
					<div class="flex items-center gap-2">
						<button
							type="button"
							onclick={() => (confirmAction = 'empty')}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-[11px] font-medium text-gray-200 transition hover:border-yellow-400/40 hover:bg-yellow-600/20 hover:text-yellow-200"
						>
							Empty
						</button>
						<button
							type="button"
							onclick={() => (confirmAction = 'drop')}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-[11px] font-medium text-gray-200 transition hover:border-red-400/40 hover:bg-red-600/20 hover:text-red-300"
						>
							Drop
						</button>
					</div>
				</div>

				<div class="flex gap-1 border-b border-white/5 px-5 pt-2">
					<button
						type="button"
						onclick={() => (tableTab = 'browse')}
						class="cursor-pointer rounded-t-lg px-4 py-2 text-xs font-semibold transition {tableTab === 'browse' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
					>Browse</button>
					<button
						type="button"
						onclick={() => { tableTab = 'structure'; if (structure.length === 0) loadStructure().catch((err) => toast(err.message, true)); }}
						class="cursor-pointer rounded-t-lg px-4 py-2 text-xs font-semibold transition {tableTab === 'structure' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
					>Structure</button>
				</div>

				{#if tableTab === 'browse'}
					<!-- Browse -->
					<div class="flex flex-wrap items-center gap-2 border-b border-white/5 px-5 py-2.5">
						<form onsubmit={submitBrowseSearch} class="relative min-w-52 flex-1">
							<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
							</svg>
							<input
								type="text"
								bind:value={browseSearchInput}
								oninput={onSearchInput}
								placeholder="Search all columns…"
								aria-label="Search rows"
								class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none"
							/>
						</form>
						<select
							bind:value={browsePerPage}
							onchange={() => { browsePage = 1; loadBrowse().catch((err) => toast(err.message, true)); }}
							aria-label="Rows per page"
							class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
						>
							{#each [25, 50, 100, 250] as n}<option value={n}>{n} / page</option>{/each}
						</select>
						<button
							type="button"
							onclick={exportCsv}
							disabled={!browse || browse.rows.length === 0}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2.5 py-1.5 text-[11px] font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40"
						>
							Export CSV
						</button>
					</div>

					<div class="min-h-0 flex-1 overflow-auto">
						{#if browseLoading}
							<div class="space-y-2 p-5">
								{#each Array(8) as _}
									<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
								{/each}
							</div>
						{:else if browseError}
							<div class="m-5 rounded-xl border border-red-700 bg-red-900/30 p-3.5 font-mono text-xs text-red-300">{browseError}</div>
						{:else if browse}
							<table class="w-full text-xs">
								<thead class="sticky top-0 z-10">
									<tr class="border-b border-gray-700 bg-gray-900/95 backdrop-blur">
										{#each browse.columns as col}
											<th class="whitespace-nowrap px-3 py-2 text-left">
												<button
													type="button"
													onclick={() => sortBy(col)}
													class="inline-flex cursor-pointer items-center gap-1 font-semibold text-gray-300 transition hover:text-white"
												>
													{col}
													{#if browseSort === col}<span class="text-blue-400">{browseDesc ? '↓' : '↑'}</span>{/if}
												</button>
											</th>
										{/each}
									</tr>
								</thead>
								<tbody class="divide-y divide-gray-700/40">
									{#each browse.rows as row}
										<tr class="hover:bg-gray-750">
											{#each row as cell}
												<td class="max-w-[16rem] truncate px-3 py-1.5 font-mono text-gray-300" title={cell ?? 'NULL'}>
													{#if cell === null}<span class="italic text-gray-600">NULL</span>{:else}{cell}{/if}
												</td>
											{/each}
										</tr>
									{:else}
										<tr><td class="px-4 py-8 text-center text-gray-500" colspan="{browse.columns.length || 1}">
											{browseSearch ? `No rows match "${browseSearch}".` : 'This table is empty.'}
										</td></tr>
									{/each}
								</tbody>
							</table>
						{/if}
					</div>

					{#if browse && browse.total_pages > 0}
						{@const bp = browse}
						<div class="flex items-center justify-between gap-3 border-t border-white/5 px-5 py-2.5 text-xs text-gray-400">
							<span>
								{bp.total} rows{browseSearch ? ` (filtered)` : ''}
								· page {bp.page} / {bp.total_pages}
							</span>
							<div class="flex items-center gap-1">
								<button type="button" onclick={() => goToPage(1)} disabled={bp.page <= 1} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2 py-1 transition hover:bg-gray-600 disabled:opacity-40">«</button>
								<button type="button" onclick={() => goToPage(bp.page - 1)} disabled={bp.page <= 1} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2 py-1 transition hover:bg-gray-600 disabled:opacity-40">‹</button>
								<button type="button" onclick={() => goToPage(bp.page + 1)} disabled={bp.page >= bp.total_pages} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2 py-1 transition hover:bg-gray-600 disabled:opacity-40">›</button>
								<button type="button" onclick={() => goToPage(bp.total_pages)} disabled={bp.page >= bp.total_pages} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2 py-1 transition hover:bg-gray-600 disabled:opacity-40">»</button>
							</div>
						</div>
					{/if}
				{:else}
					<!-- Structure -->
					<div class="min-h-0 flex-1 overflow-auto p-5">
						{#if structureLoading}
							<div class="space-y-2">
								{#each Array(6) as _}
									<div class="h-5 animate-pulse rounded bg-gray-700/50"></div>
								{/each}
							</div>
						{:else}
							<table class="w-full text-xs">
								<thead>
									<tr class="border-b border-gray-700">
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">Column</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">Type</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">Null</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">Key</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">Default</th>
									</tr>
								</thead>
								<tbody class="divide-y divide-gray-700/40">
									{#each structure as col (col.name)}
										<tr class="hover:bg-gray-750">
											<td class="px-3 py-2 font-mono font-semibold text-gray-100">{col.name}</td>
											<td class="px-3 py-2 font-mono text-gray-300">{col.type}</td>
											<td class="px-3 py-2 text-gray-400">{col.nullable ? 'YES' : 'NO'}</td>
											<td class="px-3 py-2">
												{#if col.key}
													<span class="rounded bg-yellow-900/50 px-1.5 py-0.5 font-mono text-[10px] font-bold text-yellow-300">{col.key}</span>
												{/if}
											</td>
											<td class="px-3 py-2 font-mono text-gray-400">{col.default || '—'}</td>
										</tr>
									{/each}
								</tbody>
							</table>
						{/if}
					</div>
				{/if}
			{:else}
				<!-- No table selected -->
				<div class="flex flex-1 flex-col items-center justify-center gap-3 p-10 text-center">
					<span class="flex h-14 w-14 items-center justify-center rounded-2xl bg-blue-500/10 text-blue-400">
						<svg class="h-7 w-7" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M3.375 19.5h17.25m-17.25 0a1.125 1.125 0 01-1.125-1.125M3.375 19.5h1.5C5.496 19.5 6 18.996 6 18.375m-3.75 0V5.625m0 12.75v-1.5c0-.621.504-1.125 1.125-1.125m18.375 2.625V5.625m0 12.75c0 .621-.504 1.125-1.125 1.125m1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125m0 3.75h-1.5A1.125 1.125 0 0118 18.375M20.625 4.5H3.375m17.25 0c.621 0 1.125.504 1.125 1.125M20.625 4.5h-1.5C18.504 4.5 18 5.004 18 5.625m3.75 0v1.5c0 .621-.504 1.125-1.125 1.125M3.375 4.5c-.621 0-1.125.504-1.125 1.125M3.375 4.5h1.5C5.496 4.5 6 5.004 6 5.625m-3.75 0v1.5c0 .621.504 1.125 1.125 1.125m0 0h1.5m-1.5 0c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125m1.5-3.75C5.496 7.25 6 6.746 6 6.125v1.5m0-1.5c0-.621-.504-1.125-1.125-1.125M6 7.25v-1.5M6 7.25c0 .621.504 1.125 1.125 1.125h1.5m11.25 0h1.5m-1.5 0c.621 0 1.125.504 1.125 1.125v1.5c0 .621-.504 1.125-1.125 1.125m2.25-3.75c0 .621-.504 1.125-1.125 1.125M18 5.625v1.5m0-1.5c0-.621-.504-1.125-1.125-1.125M6 7.25v1.5c0 .621-.504 1.125-1.125 1.125M6 7.25c0 .621.504 1.125 1.125 1.125" />
						</svg>
					</span>
					<p class="text-sm text-gray-400">Select a table from the sidebar to browse its data and structure.</p>
					<p class="text-xs text-gray-500">Or open the SQL Console to run any query as this user.</p>
				</div>
			{/if}
		</div>
	</div>

	<!-- Confirm modal for Empty / Drop -->
	{#if confirmAction}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-label="Confirm table action">
			<div class="w-full max-w-sm rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
				<h4 class="text-base font-semibold text-white">
					{confirmAction === 'drop' ? 'Drop table' : 'Empty table'} <span class="font-mono">{selectedTable}</span>?
				</h4>
				<p class="mt-2 text-sm text-gray-400">
					{#if confirmAction === 'drop'}
						The table and all of its data will be permanently deleted. This cannot be undone.
					{:else}
						All rows will be removed. The table structure is kept. This cannot be undone.
					{/if}
				</p>
				<div class="mt-4 flex justify-end gap-2">
					<button type="button" onclick={() => (confirmAction = null)} class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">Cancel</button>
					<button
						type="button"
						onclick={() => runTableAction(confirmAction!)}
						disabled={confirmBusy}
						class="cursor-pointer rounded-lg bg-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:opacity-50"
					>
						{confirmBusy ? 'Working…' : confirmAction === 'drop' ? 'Drop table' : 'Empty table'}
					</button>
				</div>
			</div>
		</div>
	{/if}
{/if}
