<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, getCSRFToken } from '$lib/api';
	import { language, translate } from '$lib/stores/language';

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
	const lastDbKey = `dbmanage-db-${userId}`;

	let token = $state(sessionStorage.getItem(tokenKey) || '');
	let unlockedUser = $state<{ username: string; engine: string } | null>(null);
	function restoreStoredUser(): { username: string; engine: string } | null {
		try {
			const raw = sessionStorage.getItem(tokenKey + '-user');
			return raw && token ? JSON.parse(raw) : null;
		} catch {
			return null;
		}
	}
	unlockedUser = restoreStoredUser();
	// A stored token survives refresh — reload the workspace instead of
	// forcing the operator back to the unlock gate. Deliberately onMount, not
	// $effect: loadDatabases/loadObjects read selectedDb, so a reactive effect
	// would re-run on every database switch and reset the selection back to
	// the first database in the list.
	onMount(() => {
		if (token) {
			loadDatabases().catch((err) => toast(err.message, true));
			loadObjects().catch(() => undefined);
		}
	});
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
	let selectedDb = $state(sessionStorage.getItem(lastDbKey) || '');
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

	// Non-table objects (views, routines, triggers)
	interface DbObjects {
		views: string[];
		procedures: string[];
		functions: string[];
		triggers: string[];
	}
	let objects = $state<DbObjects | null>(null);
	let objectDefinition = $state<{ kind: string; name: string; text: string } | null>(null);
	let objectBusy = $state('');

	// Row editing
	let editingRow = $state<{ mode: 'insert' | 'edit'; columns: string[]; values: (string | null)[] } | null>(null);
	let rowSaving = $state(false);
	let deletingRowIndex = $state(-1);

	// Column management
	let showColumnForm = $state(false);
	let columnForm = $state<{ mode: 'add' | 'modify'; original: string; name: string; type: string; nullable: boolean; hasDefault: boolean; default: string } | null>(null);
	let columnBusy = $state('');
	let dropColumnTarget = $state('');

	// Modals + feedback
	let confirmAction = $state<'empty' | 'drop' | null>(null);
	let confirmBusy = $state(false);
	let toastMsg = $state('');
	let toastError = $state('');

	// Bulk table selection (checkboxes in the sidebar table list)
	let selectedTables = $state<string[]>([]);
	let bulkConfirm = $state<'empty' | 'drop' | null>(null);

	// Restore
	let restoreFileInput: HTMLInputElement | undefined = $state();
	let restoreFile = $state<File | null>(null);
	let restoreBusy = $state(false);
	let restoreConfirmOpen = $state(false);

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
				const message = json?.error?.message || translate($language, 'dbm.session_expired');
				throw new Error(message);
			}
			throw new Error(json?.error?.message || translate($language, 'dbm.request_failed_http').replace('{code}', String(res.status)));
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
		sessionStorage.removeItem(tokenKey + '-user');
		token = '';
		unlockedUser = null;
		databases = [];
		tables = [];
		selectedDb = '';
		selectedTable = '';
		if (expired) sessionError = translate($language, 'dbm.session_expired_full');
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
			if (!res.ok) throw new Error(json?.error?.message || translate($language, 'dbm.unlock_failed'));
			token = json.data.token;
			unlockedUser = { username: json.data.username, engine: json.data.engine };
			sessionStorage.setItem(tokenKey, token);
			sessionStorage.setItem(tokenKey + '-user', JSON.stringify({ username: json.data.username, engine: json.data.engine }));
			unlockPassword = '';
			await loadDatabases();
			await loadObjects().catch(() => undefined);
		} catch (err) {
			unlockError = err instanceof Error ? err.message : translate($language, 'dbm.unlock_failed');
		} finally {
			unlockBusy = false;
		}
	}

	async function loadDatabases() {
		databases = await mapi<string[]>('/databases');
		if (databases.length > 0) {
			// Keep the current selection when it still exists; otherwise prefer
			// the last database picked in this browser (survives refresh), then
			// fall back to the first entry.
			if (!databases.includes(selectedDb)) {
				const stored = sessionStorage.getItem(lastDbKey) || '';
				selectedDb = databases.includes(stored) ? stored : databases[0];
			}
			await loadTables();
		}
	}

	async function loadTables() {
		if (!selectedDb) return;
		tables = await mapi<ManagedTable[]>(`/tables?database=${encodeURIComponent(selectedDb)}`);
	}

	async function loadObjects() {
		if (!selectedDb) return;
		try {
			objects = await mapi<DbObjects>(`/objects?database=${encodeURIComponent(selectedDb)}`);
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		}
	}

	function pickDatabase(name: string) {
		selectedDb = name;
		sessionStorage.setItem(lastDbKey, name);
		selectedTable = '';
		selectedTables = [];
		viewMode = 'tables';
		tables = [];
		objects = null;
		loadTables().catch((err) => toast(err.message, true));
		loadObjects().catch((err) => toast(err.message, true));
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
			browseError = err instanceof Error ? err.message : translate($language, 'dbm.load_rows_failed');
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
			sqlError = err instanceof Error ? err.message : translate($language, 'dbm.query_failed');
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
					toast(translate($language, 'dbm.emptied').replace('{name}', selectedTable));
				} else {
					await mapi('/drop-table', { method: 'POST', body: { database: selectedDb, table: selectedTable } });
					toast(translate($language, 'dbm.dropped').replace('{name}', selectedTable));
				selectedTable = '';
				structure = [];
				browse = null;
			}
			await loadTables();
		} catch (err) {
				toast(err instanceof Error ? err.message : translate($language, 'dbm.action_failed'), true);
		} finally {
			// Close the modal whatever the outcome so the toast is visible.
			confirmAction = null;
			confirmBusy = false;
		}
	}

	// ─── Bulk table actions (checkboxes) ─────────────────────────────

	function toggleTableSelect(name: string, checked: boolean) {
		selectedTables = checked
			? [...selectedTables, name]
			: selectedTables.filter((n) => n !== name);
	}

	function toggleSelectAllTables(checked: boolean) {
		selectedTables = checked ? filteredTables.map((t) => t.name) : [];
	}

	// Runs the single-table endpoint for each checked table, keeps going on
	// failures, and reports the totals at the end.
	async function runBulkTableAction(action: 'empty' | 'drop') {
		if (!bulkConfirm || confirmBusy) return;
		confirmBusy = true;
		const targets = [...selectedTables];
		let ok = 0;
		const failed: string[] = [];
		for (const table of targets) {
			try {
				await mapi(action === 'empty' ? '/empty-table' : '/drop-table', {
					method: 'POST',
					body: { database: selectedDb, table }
				});
				ok++;
			} catch (err) {
				failed.push(`${table}: ${err instanceof Error ? err.message : String(err)}`);
			}
		}
		confirmBusy = false;
		bulkConfirm = null;
		selectedTables = [];
		if (targets.includes(selectedTable)) {
			selectedTable = '';
			structure = [];
			browse = null;
		}
		if (failed.length > 0) {
			toast(
				translate($language, 'dbm.bulk_partial')
					.replace('{ok}', String(ok))
					.replace('{failed}', String(failed.length)) +
					' — ' +
					failed.join('; '),
				true
			);
		} else if (ok > 0) {
			toast(translate($language, 'dbm.bulk_done').replace('{ok}', String(ok)));
		}
		await loadTables().catch((err) => toast(err.message, true));
		loadObjects().catch(() => undefined);
	}

	function pickRestoreFile(e: Event) {
		const el = e.currentTarget as HTMLInputElement;
		restoreFile = el.files?.[0] ?? null;
		el.value = '';
	}

	async function doRestore() {
		if (!restoreFile || !selectedDb || restoreBusy) return;
		restoreBusy = true;
		try {
			const formData = new FormData();
			formData.append('file', restoreFile);
			formData.append('database', selectedDb);
			const res = await fetch(`/api/v1/databases/manage/${token}/restore`, {
				method: 'POST',
				headers: { 'X-DB-Manage-Token': token, 'X-CSRF-Token': getCSRFToken() },
				credentials: 'include',
				body: formData
			});
			const json = await res.json().catch(() => null);
			if (!res.ok) {
				const code = json?.error?.code || '';
				if (code === 'MANAGE_SESSION_EXPIRED' || res.status === 401) {
					lockSession(true);
					throw new Error(json?.error?.message || translate($language, 'dbm.session_expired'));
				}
				throw new Error(json?.error?.message || translate($language, 'dbm.request_failed_http').replace('{code}', String(res.status)));
			}
			toast(translate($language, 'dbm.restored').replace('{db}', selectedDb));
			restoreFile = null;
			selectedTable = '';
			structure = [];
			browse = null;
			await loadTables();
		} catch (err) {
			toast(err instanceof Error ? err.message : translate($language, 'dbm.restore_failed'), true);
		} finally {
			// Close the modal whatever the outcome so the success/error toast
			// underneath is visible.
			restoreConfirmOpen = false;
			restoreBusy = false;
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

	async function viewObjectDefinition(kind: string, name: string) {
		objectBusy = kind + ':' + name;
		try {
			const res = await mapi<{ definition: string }>(
				`/definition?database=${encodeURIComponent(selectedDb)}&kind=${encodeURIComponent(kind)}&name=${encodeURIComponent(name)}`
			);
			objectDefinition = { kind, name, text: res.definition || '(empty)' };
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		} finally {
			objectBusy = '';
		}
	}

	function primaryKeyColumns(): string[] {
		return structure.filter((c) => c.key === 'PRI').map((c) => c.name);
	}

	function rowKeyValue(row: (string | null)[] | null, col: string): string | null {
		const idx = browse?.columns.indexOf(col) ?? -1;
		return idx === -1 || row === null ? null : (row[idx] ?? null);
	}

	function openInsertRow() {
		if (!browse) return;
		editingRow = { mode: 'insert', columns: browse.columns, values: browse.columns.map(() => null) };
	}

	function openEditRow(row: (string | null)[]) {
		if (!browse) return;
		editingRow = { mode: 'edit', columns: browse.columns, values: [...row] };
	}

	function setEditingValue(idx: number, value: string) {
		const row = editingRow;
		if (!row) return;
		row.values[idx] = value === '' ? null : value;
		editingRow = { ...row };
	}

	function setEditingNull(idx: number, isNull: boolean) {
		const row = editingRow;
		if (!row) return;
		row.values[idx] = isNull ? null : row.values[idx] ?? '';
		editingRow = { ...row };
	}

	async function saveEditingRow() {
		const editing = editingRow;
		if (!editing || !browse || rowSaving) return;
		rowSaving = true;
		try {
			if (editing.mode === 'insert') {
				await mapi('/insert-row', {
					method: 'POST',
					body: { database: selectedDb, table: selectedTable, columns: editing.columns, values: editing.values }
				});
				toast(translate($language, 'dbm.rowInserted'));
			} else {
				const pks = primaryKeyColumns();
				const keys = pks.map((col) => ({ col, val: rowKeyValue(editing.values, col) }));
				const set = editing.columns
					.map((col, i) => ({ col, val: editing.values[i] }))
					.filter((entry) => !pks.includes(entry.col));
				await mapi('/update-row', {
					method: 'POST',
					body: {
						database: selectedDb,
						table: selectedTable,
						keys: { columns: keys.map((k) => k.col), values: keys.map((k) => k.val) },
						set: { columns: set.map((s) => s.col), values: set.map((s) => s.val) }
					}
				});
				toast(translate($language, 'dbm.rowSaved'));
			}
			editingRow = null;
			await loadBrowse().catch((err) => toast(err.message, true));
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		} finally {
			rowSaving = false;
		}
	}

	let pendingDeleteRow = $state<(string | null)[] | null>(null);

	function askDeleteRow(row: (string | null)[]) {
		pendingDeleteRow = row;
	}

	async function confirmDeleteRow() {
		if (!pendingDeleteRow || !browse) return;
		rowSaving = true;
		try {
			const pks = primaryKeyColumns();
			const keys = pks.map((col) => ({ col, val: rowKeyValue(pendingDeleteRow, col) }));
			await mapi('/delete-row', {
				method: 'POST',
				body: { database: selectedDb, table: selectedTable, keys: { columns: keys.map((k) => k.col), values: keys.map((k) => k.val) } }
			});
			toast(translate($language, 'dbm.rowDeleted'));
			pendingDeleteRow = null;
			await loadBrowse().catch((err) => toast(err.message, true));
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		} finally {
			rowSaving = false;
		}
	}

	// ── Column management ───────────────────────────────────────────
	function openAddColumn() {
		columnForm = { mode: 'add', original: '', name: '', type: 'varchar(255)', nullable: true, hasDefault: false, default: '' };
	}

	function openModifyColumn(name: string, col: ManagedColumn) {
		columnForm = { mode: 'modify', original: name, name, type: col.type, nullable: col.nullable, hasDefault: !!col.default, default: col.default || '' };
	}

	async function saveColumn() {
		if (!columnForm || columnBusy) return;
		columnBusy = 'save';
		try {
			if (columnForm.mode === 'add') {
				await mapi('/add-column', {
					method: 'POST',
					body: {
						database: selectedDb,
						table: selectedTable,
						spec: {
							name: columnForm.name,
							type: columnForm.type,
							nullable: columnForm.nullable,
							has_default: columnForm.hasDefault,
							default: columnForm.hasDefault ? columnForm.default : null
						}
					}
				});
				toast(translate($language, 'dbm.columnAdded'));
			} else {
				await mapi('/modify-column', {
					method: 'POST',
					body: {
						database: selectedDb,
						table: selectedTable,
						original: columnForm.original,
						spec: {
							name: columnForm.name,
							type: columnForm.type,
							nullable: columnForm.nullable,
							has_default: columnForm.hasDefault,
							default: columnForm.hasDefault ? columnForm.default : null
						}
					}
				});
				toast(translate($language, 'dbm.columnSaved'));
			}
			columnForm = null;
			await loadStructure().catch((err) => toast(err.message, true));
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		} finally {
			columnBusy = '';
		}
	}

	async function dropColumn(name: string) {
		if (columnBusy) return;
		columnBusy = 'drop';
		try {
			await mapi('/drop-column', { method: 'POST', body: { database: selectedDb, table: selectedTable, column: name } });
			toast(translate($language, 'dbm.columnDropped').replace('{name}', name));
			dropColumnTarget = '';
			await loadStructure().catch((err) => toast(err.message, true));
		} catch (err) {
			toast(err instanceof Error ? err.message : String(err), true);
		} finally {
			columnBusy = '';
		}
	}

	let filteredTables = $derived.by(() => {
		const q = tableFilter.trim().toLowerCase();
		return q ? tables.filter((t) => t.name.toLowerCase().includes(q)) : tables;
	});

	// For PostgreSQL sessions, group tables under their schema headers —
	// non-public tables arrive as "schema.table" names. Null keeps the flat
	// list for engines without schemas (MySQL).
	let tableGroups = $derived.by(() => {
		if (unlockedUser?.engine !== 'postgresql') return null;
		const order: string[] = [];
		const map: Record<string, ManagedTable[]> = {};
		for (const t of filteredTables) {
			const idx = t.name.indexOf('.');
			const schema = idx > 0 ? t.name.slice(0, idx) : 'public';
			if (!map[schema]) {
				map[schema] = [];
				order.push(schema);
			}
			map[schema].push(t);
		}
		order.sort((a, b) => (a === 'public' ? -1 : b === 'public' ? 1 : a.localeCompare(b)));
		return order.map((schema) => ({ schema, tables: map[schema] }));
	});

	let allTablesSelected = $derived(
		filteredTables.length > 0 && filteredTables.every((t) => selectedTables.includes(t.name))
	);

	let pkColumns = $derived(structure.filter((c) => c.key === 'PRI').map((c) => c.name));
	let rowActionsVisible = $derived(pkColumns.length > 0 && !objects?.views.includes(selectedTable));

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
		if (e.key === 'Escape') { if (confirmAction) confirmAction = null; if (bulkConfirm) bulkConfirm = null; if (editingRow) editingRow = null; if (columnForm) columnForm = null; if (objectDefinition) objectDefinition = null; if (pendingDeleteRow) pendingDeleteRow = null; }
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
				<h2 class="mt-4 text-xl font-bold text-white">{translate($language, 'dbm.title')}</h2>
				<p class="mt-2 text-sm text-gray-400">
					{translate($language, 'dbm.unlock_help')}
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
						{translate($language, 'dbm.password_label')}
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
					{unlockBusy ? translate($language, 'dbm.unlocking') : translate($language, 'dbm.unlock')}
				</button>
			</form>
			<a href="/databases" class="mt-4 block text-center text-xs text-gray-500 transition hover:text-gray-300">{translate($language, 'dbm.back')}</a>
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
						<p class="truncate font-mono text-sm font-semibold text-white">{unlockedUser?.username || translate($language, 'dbm.session_fallback')}</p>
						<p class="text-[10px] font-semibold uppercase tracking-[0.16em] text-blue-400">
							{unlockedUser?.engine || ''} {translate($language, 'dbm.management')}
						</p>
					</div>
					<button
						type="button"
						onclick={() => lockSession(false)}
						title={translate($language, 'dbm.end_session')}
						class="shrink-0 cursor-pointer rounded-lg border border-gray-600 bg-gray-700 p-1.5 text-gray-300 transition hover:border-red-400/40 hover:bg-red-600/20 hover:text-red-300"
					>
						<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9" />
						</svg>
					</button>
				</div>
			</div>

			<div class="border-b border-white/5 px-4 py-3">
				<label for="manage-db" class="mb-1 block text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">{translate($language, 'dbm.database')}</label>
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
				{#if databases.length === 0}
					<p class="mt-1.5 text-[11px] leading-snug text-yellow-300">{translate($language, 'dbm.no_granted_databases')}</p>
				{/if}
				<button
					type="button"
					onclick={() => { viewMode = 'sql'; }}
					class="mt-2.5 flex w-full cursor-pointer items-center justify-center gap-2 rounded-lg border border-blue-400/30 bg-blue-500/10 px-3 py-2 text-xs font-semibold text-blue-300 transition hover:bg-blue-500/20
					{viewMode === 'sql' ? 'ring-2 ring-blue-500/40' : ''}"
				>
					<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6.75 7.5l3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0021 18V6a2.25 2.25 0 00-2.25-2.25H5.25A2.25 2.25 0 003 6v12a2.25 2.25 0 002.25 2.25z" />
					</svg>
					{translate($language, 'dbm.sql_console')}
				</button>

				<div class="mt-2.5 border-t border-white/5 pt-2.5">
					<span class="mb-1 block text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">{translate($language, 'dbm.restore')}</span>
					<input type="file" bind:this={restoreFileInput} onchange={pickRestoreFile} accept=".sql,.gz,.gzip" class="hidden" />
					<button
						type="button"
						onclick={() => restoreFileInput?.click()}
						title={restoreFile?.name}
						class="w-full cursor-pointer truncate rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-left text-xs text-gray-300 transition hover:border-blue-500 focus:outline-none"
					>
						{restoreFile ? restoreFile.name : translate($language, 'dbm.choose_dump')}
					</button>
					{#if restoreFile}
						<button
							type="button"
							onclick={() => (restoreConfirmOpen = true)}
							disabled={!selectedDb}
							title={!selectedDb ? translate($language, 'dbm.no_database') : ''}
							class="mt-2 flex w-full cursor-pointer items-center justify-center gap-2 rounded-lg bg-green-600 px-3 py-2 text-xs font-semibold text-white transition hover:bg-green-700 disabled:cursor-not-allowed disabled:opacity-40"
						>
							<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
								<path stroke-linecap="round" stroke-linejoin="round" d="M8.25 9L4.5 12m0 0l3.75 3M4.5 12h9m4.5 4.5V9.75a4.5 4.5 0 00-4.5-4.5H9" />
							</svg>
							{translate($language, 'dbm.restore_into').replace('{db}', selectedDb)}
						</button>
					{/if}
				</div>
			</div>

			<div class="px-3 py-2.5">
				<div class="flex items-center gap-2">
					<input
						type="checkbox"
						checked={allTablesSelected}
						onchange={(e) => toggleSelectAllTables(e.currentTarget.checked)}
						disabled={filteredTables.length === 0}
						aria-label={translate($language, 'dbm.select_all_tables')}
						title={translate($language, 'dbm.select_all_tables')}
						class="shrink-0 accent-blue-500"
					/>
					<input
						type="text"
						bind:value={tableFilter}
						placeholder={translate($language, 'dbm.filter_tables')}
						aria-label={translate($language, 'dbm.filter_tables')}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none"
					/>
				</div>
				{#if selectedTables.length > 0}
					<div class="mt-2 rounded-lg border border-yellow-700/40 bg-yellow-900/10 p-2">
						<p class="text-[11px] font-semibold text-yellow-300">
							{translate($language, 'dbm.tables_selected').replace('{count}', String(selectedTables.length))}
						</p>
						<div class="mt-1.5 flex items-center gap-1.5">
							<button
								type="button"
								onclick={() => (bulkConfirm = 'empty')}
								class="flex-1 cursor-pointer rounded-md bg-yellow-600 px-2 py-1.5 text-[10px] font-semibold text-white transition hover:bg-yellow-500"
							>
								{translate($language, 'dbm.bulk_empty')}
							</button>
							<button
								type="button"
								onclick={() => (bulkConfirm = 'drop')}
								class="flex-1 cursor-pointer rounded-md bg-red-600 px-2 py-1.5 text-[10px] font-semibold text-white transition hover:bg-red-500"
							>
								{translate($language, 'dbm.bulk_drop')}
							</button>
							<button
								type="button"
								onclick={() => (selectedTables = [])}
								title={translate($language, 'dbm.clear_selection')}
								aria-label={translate($language, 'dbm.clear_selection')}
								class="cursor-pointer rounded-md px-1.5 py-1.5 text-[10px] text-gray-400 transition hover:bg-white/5 hover:text-gray-200"
							>
								✕
							</button>
						</div>
					</div>
				{/if}
			</div>

			<nav class="flex-1 overflow-y-auto px-2 pb-2" aria-label={translate($language, 'dbm.tables_aria')}>
				{#snippet tableRow(t: ManagedTable)}
					<div class="mb-0.5 flex w-full items-center gap-1 rounded-lg pr-2 transition {selectedTable === t.name && viewMode === 'tables' ? 'bg-blue-500/15' : 'hover:bg-white/5'}">
						<input
							type="checkbox"
							checked={selectedTables.includes(t.name)}
							onchange={(e) => toggleTableSelect(t.name, e.currentTarget.checked)}
							aria-label="{translate($language, 'dbm.select_table')} {t.name}"
							class="ml-2.5 shrink-0 accent-blue-500"
						/>
						<button
							type="button"
							onclick={() => pickTable(t.name)}
							class="flex min-w-0 flex-1 cursor-pointer items-center justify-between gap-2 rounded-lg px-1.5 py-2 text-left {selectedTable === t.name && viewMode === 'tables' ? 'text-blue-100' : 'text-gray-300'}"
						>
							<span class="min-w-0 truncate font-mono text-xs">{t.name}</span>
							<span class="shrink-0 text-[10px] tabular-nums text-gray-500">{t.rows > 0 ? t.rows : ''}</span>
						</button>
					</div>
				{/snippet}

				{#if tableGroups}
					{#if tableGroups.length === 0}
						<p class="px-2 py-4 text-xs text-gray-500">{tableFilter ? translate($language, 'dbm.no_tables_match') : translate($language, 'dbm.no_tables')}</p>
					{:else}
						{#each tableGroups as g (g.schema)}
							<div class="mb-1.5">
								<span class="mb-0.5 flex items-center gap-1 px-2.5 pt-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-500">
									<svg class="h-2.5 w-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
										<path d="M4 6a8 3 0 1016 0 8 3 0 00-16 0" />
										<path d="M4 6v12a8 3 0 0016 0V6" />
									</svg>
									{g.schema}
								</span>
								{#each g.tables as t (t.name)}
									{@render tableRow(t)}
								{/each}
							</div>
						{/each}
					{/if}
				{:else}
					{#each filteredTables as t (t.name)}
						{@render tableRow(t)}
					{:else}
						<p class="px-2 py-4 text-xs text-gray-500">{tableFilter ? translate($language, 'dbm.no_tables_match') : translate($language, 'dbm.no_tables')}</p>
					{/each}
				{/if}

				{#if objects}
					{#if objects.views.length}
						<div class="mt-2.5 border-t border-white/5 pt-2.5">
							<span class="mb-1 block text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">{translate($language, 'dbm.views')}</span>
							{#each objects.views as v (v)}
								<button type="button" onclick={() => pickTable(v)} class="mb-0.5 flex w-full cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left transition {selectedTable === v && viewMode === 'tables' ? 'bg-blue-500/15 text-blue-100' : 'text-gray-300 hover:bg-white/5'}">
									<span class="min-w-0 truncate font-mono text-xs">{v}</span>
								</button>
							{/each}
						</div>
					{/if}
					{#if objects.procedures.length || objects.functions.length || objects.triggers.length}
						<div class="mt-2.5 border-t border-white/5 pt-2.5">
							<span class="mb-1 block text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">{translate($language, 'dbm.routines')}</span>
							{#each objects.procedures as p (p)}
								<button type="button" onclick={() => viewObjectDefinition('procedure', p)} class="mb-0.5 flex w-full cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left transition text-gray-300 hover:bg-white/5">
									<span class="min-w-0 truncate font-mono text-xs">{p}</span>
									<span class="shrink-0 text-[9px] uppercase text-gray-500">proc</span>
								</button>
							{/each}
							{#each objects.functions as fn (fn)}
								<button type="button" onclick={() => viewObjectDefinition('function', fn)} class="mb-0.5 flex w-full cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left transition text-gray-300 hover:bg-white/5">
									<span class="min-w-0 truncate font-mono text-xs">{fn}</span>
									<span class="shrink-0 text-[9px] uppercase text-gray-500">fn</span>
								</button>
							{/each}
							{#each objects.triggers as tg (tg)}
								<button type="button" onclick={() => viewObjectDefinition('trigger', tg)} class="mb-0.5 flex w-full cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-left transition text-gray-300 hover:bg-white/5">
									<span class="min-w-0 truncate font-mono text-xs">{tg}</span>
									<span class="shrink-0 text-[9px] uppercase text-gray-500">trg</span>
								</button>
							{/each}
						</div>
					{/if}
				{/if}
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
						{translate($language, 'dbm.sql_console')} — <span class="font-mono text-blue-300">{selectedDb || translate($language, 'dbm.no_database')}</span>
					</h3>
					<span class="text-[11px] text-gray-500">{translate($language, 'dbm.ctrl_enter')}</span>
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
							{sqlRunning ? translate($language, 'dbm.running') : translate($language, 'dbm.run')}
						</button>
						{#if sqlResult?.capped}
							<span class="text-[11px] text-yellow-400">{translate($language, 'dbm.capped')}</span>
						{/if}
					</div>

					{#if sqlError}
						<div class="rounded-xl border border-red-700 bg-red-900/30 p-3.5 font-mono text-xs text-red-300">{sqlError}</div>
					{:else if sqlResult}
						{#if sqlResult.is_select}
							<div class="overflow-x-auto rounded-xl border border-gray-700">
								<div class="mb-3 flex items-center justify-between gap-2">
									<p class="text-xs text-gray-500">{translate($language, 'dbm.column_hint')}</p>
									<button type="button" onclick={openAddColumn} class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-blue-700">{translate($language, 'dbm.add_column')}</button>
								</div>

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
												<tr><td class="px-3 py-3 text-center text-gray-500" colspan="{sqlResult.columns.length || 1}">{translate($language, 'dbm.no_rows_returned')}</td></tr>
											{/each}
										</tbody>
									</table>
								</div>
								<p class="text-[11px] text-gray-500">{translate($language, 'dbm.rows_count').replace('{count}', String(sqlResult.rows.length))}</p>
						{:else}
							<div class="rounded-xl border border-green-700/50 bg-green-900/20 p-4 text-sm text-green-300">
								{sqlResult.tag || 'OK'}
								{#if sqlResult.affected > 0}{translate($language, 'dbm.affected').replace('{count}', String(sqlResult.affected))}{/if}
							</div>
						{/if}
					{/if}
				</div>
			{:else if selectedTable}
				<!-- Table view -->
				<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-3">
					<div class="flex items-center gap-2.5">
						<h3 class="font-mono text-sm font-semibold text-white">{selectedTable}</h3>
						<span class="text-[11px] text-gray-500">{translate($language, 'dbm.in_db').replace('{name}', selectedDb)}</span>
					</div>
					<div class="flex items-center gap-2">
						<button
							type="button"
							onclick={() => (confirmAction = 'empty')}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-[11px] font-medium text-gray-200 transition hover:border-yellow-400/40 hover:bg-yellow-600/20 hover:text-yellow-200"
						>
							{translate($language, 'dbm.empty')}
						</button>
						<button
							type="button"
							onclick={() => (confirmAction = 'drop')}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-1.5 text-[11px] font-medium text-gray-200 transition hover:border-red-400/40 hover:bg-red-600/20 hover:text-red-300"
						>
							{translate($language, 'dbm.drop')}
						</button>
					</div>
				</div>

				<div class="flex gap-1 border-b border-white/5 px-5 pt-2">
					<button
						type="button"
						onclick={() => (tableTab = 'browse')}
						class="cursor-pointer rounded-t-lg px-4 py-2 text-xs font-semibold transition {tableTab === 'browse' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
						>{translate($language, 'dbm.browse')}</button>
					<button
						type="button"
						onclick={() => { tableTab = 'structure'; if (structure.length === 0) loadStructure().catch((err) => toast(err.message, true)); }}
						class="cursor-pointer rounded-t-lg px-4 py-2 text-xs font-semibold transition {tableTab === 'structure' ? 'bg-blue-500/15 text-blue-200' : 'text-gray-400 hover:bg-white/5'}"
					>{translate($language, 'dbm.structure')}</button>
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
								placeholder={translate($language, 'dbm.search_columns')}
								aria-label={translate($language, 'dbm.search_rows')}
								class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none"
							/>
						</form>
						<select
							bind:value={browsePerPage}
							onchange={() => { browsePage = 1; loadBrowse().catch((err) => toast(err.message, true)); }}
							aria-label={translate($language, 'dbm.rows_per_page')}
							class="rounded-lg border border-gray-600 bg-gray-900 px-2 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none"
						>
							{#each [25, 50, 100, 250] as n}<option value={n}>{translate($language, 'dbm.per_page').replace('{n}', String(n))}</option>{/each}
						</select>
						<button
							type="button"
							onclick={openInsertRow}
							disabled={!browse}
							class="cursor-pointer rounded-lg bg-blue-600 px-2.5 py-1.5 text-[11px] font-medium text-white transition hover:bg-blue-700 disabled:opacity-40"
						>
							{translate($language, 'dbm.insert_row')}
						</button>
						<button
							type="button"
							onclick={exportCsv}
							disabled={!browse || browse.rows.length === 0}
							class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2.5 py-1.5 text-[11px] font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40"
						>
							{translate($language, 'dbm.export_csv')}
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
												{#if rowActionsVisible}<th class="px-3 py-2 text-right font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.actions')}</th>{/if}
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
											{#if rowActionsVisible}
												<td class="whitespace-nowrap px-3 py-1.5 text-right">
													<button type="button" onclick={() => openEditRow(row)} class="cursor-pointer rounded border border-blue-600/50 bg-blue-600/10 px-2 py-0.5 text-[11px] text-blue-300 transition hover:bg-blue-600/20">{translate($language, 'dbm.edit')}</button>
													<button type="button" onclick={() => askDeleteRow(row)} class="ml-1 cursor-pointer rounded border border-red-600/50 bg-red-600/10 px-2 py-0.5 text-[11px] text-red-300 transition hover:bg-red-600/20">{translate($language, 'dbm.delete')}</button>
												</td>
											{/if}
										</tr>
									{:else}
										<tr><td class="px-4 py-8 text-center text-gray-500" colspan="{browse.columns.length + (rowActionsVisible ? 1 : 0) || 1}">
											{browseSearch ? translate($language, 'dbm.no_rows_match').replace('{query}', browseSearch) : translate($language, 'dbm.table_empty')}
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
								{translate($language, 'dbm.rows_count').replace('{count}', String(bp.total))}{browseSearch ? translate($language, 'dbm.filtered') : ''}
								· {translate($language, 'dbm.page_of').replace('{page}', String(bp.page)).replace('{total}', String(bp.total_pages))}
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
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_column')}</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_type')}</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_null')}</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_key')}</th>
										<th class="px-3 py-2 text-left font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_default')}</th>
									<th class="px-3 py-2 text-right font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'dbm.actions')}</th>
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
									<td class="px-3 py-2 text-right whitespace-nowrap">
										<button type="button" onclick={() => openModifyColumn(col.name, col)} class="cursor-pointer rounded border border-gray-600 bg-gray-700 px-2 py-0.5 text-[10px] text-gray-200 transition hover:bg-gray-600">{translate($language, 'dbm.modify')}</button>
										{#if dropColumnTarget === col.name}
											<button type="button" onclick={() => dropColumn(col.name)} disabled={columnBusy !== ''} class="ml-1 cursor-pointer rounded bg-red-600 px-2 py-0.5 text-[10px] font-medium text-white transition hover:bg-red-700 disabled:opacity-50">{translate($language, 'dbm.confirm_drop')}</button>
											<button type="button" onclick={() => (dropColumnTarget = '')} class="ml-1 cursor-pointer text-[10px] text-gray-400 hover:text-gray-200">{translate($language, 'wl.cancel')}</button>
										{:else}
											<button type="button" onclick={() => (dropColumnTarget = col.name)} class="ml-1 cursor-pointer rounded border border-red-600/50 bg-red-600/10 px-2 py-0.5 text-[10px] text-red-300 transition hover:bg-red-600/20">{translate($language, 'dbm.drop')}</button>
										{/if}
									</td>
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
					<p class="text-sm text-gray-400">{translate($language, 'dbm.select_table_hint')}</p>
					<p class="text-xs text-gray-500">{translate($language, 'dbm.select_table_alt')}</p>
				</div>
			{/if}
		</div>
	</div>

	<!-- Confirm modal for Empty / Drop -->
	{#if confirmAction}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-label={translate($language, 'dbm.confirm_aria')}>
			<div class="w-full max-w-sm rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
				<h4 class="text-base font-semibold text-white">
					{confirmAction === 'drop' ? translate($language, 'dbm.drop_table') : translate($language, 'dbm.empty_table')} <span class="font-mono">{selectedTable}</span>?
				</h4>
				<p class="mt-2 text-sm text-gray-400">
					{#if confirmAction === 'drop'}
						{translate($language, 'dbm.drop_warning')}
					{:else}
						{translate($language, 'dbm.empty_warning')}
					{/if}
				</p>
				<div class="mt-4 flex justify-end gap-2">
					<button type="button" onclick={() => (confirmAction = null)} class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">{translate($language, 'db.cancel')}</button>
					<button
						type="button"
						onclick={() => runTableAction(confirmAction!)}
						disabled={confirmBusy}
						class="cursor-pointer rounded-lg bg-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:opacity-50"
					>
						{confirmBusy ? translate($language, 'dbm.working') : confirmAction === 'drop' ? translate($language, 'dbm.drop_table') : translate($language, 'dbm.empty_table')}
					</button>
				</div>
			</div>
		</div>
	{/if}

	<!-- Bulk confirm modal for Empty / Drop of selected tables -->
	{#if bulkConfirm}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-label={translate($language, 'dbm.confirm_aria')}>
			<div class="w-full max-w-md rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
				<h4 class="text-base font-semibold text-white">
					{bulkConfirm === 'drop'
						? translate($language, 'dbm.bulk_drop_confirm').replace('{count}', String(selectedTables.length))
						: translate($language, 'dbm.bulk_empty_confirm').replace('{count}', String(selectedTables.length))}
				</h4>
				<p class="mt-2 text-sm text-gray-400">
					{bulkConfirm === 'drop' ? translate($language, 'dbm.drop_warning') : translate($language, 'dbm.empty_warning')}
				</p>
				<p class="mt-2 max-h-24 overflow-y-auto rounded-lg bg-gray-900 px-2.5 py-1.5 font-mono text-[11px] leading-relaxed text-gray-300">
					{selectedTables.join(', ')}
				</p>
				<div class="mt-4 flex justify-end gap-2">
					<button type="button" onclick={() => (bulkConfirm = null)} class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">{translate($language, 'db.cancel')}</button>
					<button
						type="button"
						onclick={() => runBulkTableAction(bulkConfirm!)}
						disabled={confirmBusy}
						class="cursor-pointer rounded-lg px-3.5 py-2 text-sm font-semibold text-white transition disabled:opacity-50 {bulkConfirm === 'drop' ? 'bg-red-600 hover:bg-red-700' : 'bg-yellow-600 hover:bg-yellow-500'}"
					>
						{confirmBusy
							? translate($language, 'dbm.working')
							: bulkConfirm === 'drop'
								? translate($language, 'dbm.bulk_drop')
								: translate($language, 'dbm.bulk_empty')}
					</button>
				</div>
			</div>
		</div>
	{/if}

	<!-- Restore confirmation -->
	{#if restoreConfirmOpen}
		<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm" role="dialog" aria-modal="true" aria-label={translate($language, 'dbm.restore')}>
			<div class="w-full max-w-sm rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
				<h4 class="text-base font-semibold text-white">
					{translate($language, 'dbm.restore_into').replace('{db}', selectedDb)}?
				</h4>
				<p class="mt-2 text-sm text-gray-400">
					{translate($language, 'dbm.restore_warning')}
					{translate($language, 'dbm.restore_cannot_undo')}
				</p>
				<p class="mt-2 truncate rounded bg-gray-900 px-2.5 py-1.5 font-mono text-xs text-gray-300">{restoreFile?.name}</p>
				<div class="mt-4 flex justify-end gap-2">
					<button type="button" onclick={() => (restoreConfirmOpen = false)} class="cursor-pointer rounded-lg bg-gray-700 px-3.5 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">{translate($language, 'db.cancel')}</button>
					<button
						type="button"
						onclick={doRestore}
						disabled={restoreBusy}
						class="cursor-pointer rounded-lg bg-green-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-green-700 disabled:opacity-50"
					>
						{restoreBusy ? translate($language, 'dbm.working') : translate($language, 'dbm.restore')}
					</button>
				</div>
			</div>
		</div>
	{/if}
{/if}

{#if editingRow}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" tabindex="-1" onkeydown={(e) => { if (e.key === 'Escape') editingRow = null; }}>
		<div class="max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-gray-700 bg-gray-800 p-6">
			<div class="mb-4 flex items-center justify-between gap-3">
				<h3 class="text-lg font-semibold text-white">{editingRow.mode === 'insert' ? translate($language, 'dbm.insert_row') : translate($language, 'dbm.edit_row')} — <span class="font-mono">{selectedTable}</span></h3>
				<button type="button" onclick={() => (editingRow = null)} class="cursor-pointer rounded-lg p-1.5 text-gray-400 hover:bg-white/5 hover:text-white">✕</button>
			</div>
			<div class="space-y-3">
				{#each editingRow.columns as col, i}
					<div class="flex flex-wrap items-center gap-3">
						<label for="rowcol-{i}" class="w-40 shrink-0 truncate font-mono text-xs text-gray-400">{col}</label>
						<input
							id="rowcol-{i}"
							type="text"
							value={editingRow.values[i] ?? ''}
							oninput={(e) => setEditingValue(i, e.currentTarget.value)}
							class="min-w-0 flex-1 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 focus:border-blue-500 focus:outline-none"
						/>
						<label class="flex shrink-0 items-center gap-1.5 text-xs text-gray-400">
							<input type="checkbox" checked={editingRow.values[i] === null} onchange={(e) => setEditingNull(i, e.currentTarget.checked)} /> NULL
						</label>
					</div>
				{/each}
			</div>
			<div class="mt-5 flex justify-end gap-2">
				<button type="button" onclick={() => (editingRow = null)} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 transition hover:bg-gray-700">{translate($language, 'wl.cancel')}</button>
				<button type="button" onclick={saveEditingRow} disabled={rowSaving} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-50">
					{rowSaving ? translate($language, 'dbm.saving') : translate($language, 'dbm.save_row')}
				</button>
			</div>
		</div>
	</div>
{/if}

{#if columnForm}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" tabindex="-1" onkeydown={(e) => { if (e.key === 'Escape') columnForm = null; }}>
		<div class="w-full max-w-lg space-y-4 rounded-xl border border-gray-700 bg-gray-800 p-6">
			<div class="flex items-center justify-between gap-3">
				<h3 class="text-lg font-semibold text-white">{columnForm.mode === 'add' ? translate($language, 'dbm.add_column') : translate($language, 'dbm.modify_column')}</h3>
				<button type="button" onclick={() => (columnForm = null)} class="cursor-pointer rounded-lg p-1.5 text-gray-400 hover:bg-white/5 hover:text-white">✕</button>
			</div>
			<div class="space-y-3">
				<div>
					<label for="colform-name" class="mb-1 block text-xs uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_column')}</label>
					<input id="colform-name" type="text" bind:value={columnForm.name} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200" />
				</div>
				<div>
					<label for="colform-type" class="mb-1 block text-xs uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_type')}</label>
					<input id="colform-type" type="text" bind:value={columnForm.type} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200" />
				</div>
				<div class="flex flex-wrap items-center gap-4 text-sm text-gray-300">
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={columnForm.nullable} /> {translate($language, 'dbm.col_null')}</label>
					<label class="flex items-center gap-2"><input type="checkbox" bind:checked={columnForm.hasDefault} /> {translate($language, 'dbm.has_default')}</label>
				</div>
				{#if columnForm.hasDefault}
					<div>
						<label for="colform-default" class="mb-1 block text-xs uppercase tracking-wider text-gray-400">{translate($language, 'dbm.col_default')}</label>
						<input id="colform-default" type="text" bind:value={columnForm.default} class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200" />
					</div>
				{/if}
			</div>
			<div class="flex justify-end gap-2">
				<button type="button" onclick={() => (columnForm = null)} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 transition hover:bg-gray-700">{translate($language, 'wl.cancel')}</button>
				<button type="button" onclick={saveColumn} disabled={columnBusy !== ''} class="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-blue-700 disabled:opacity-50">
					{columnBusy ? translate($language, 'dbm.working') : translate($language, 'dbm.save')}
				</button>
			</div>
		</div>
	</div>
{/if}

{#if objectDefinition}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" tabindex="-1" onkeydown={(e) => { if (e.key === 'Escape') objectDefinition = null; }}>
		<div class="max-h-[85vh] w-full max-w-3xl overflow-y-auto rounded-xl border border-gray-700 bg-gray-800 p-6">
			<div class="mb-4 flex items-center justify-between gap-3">
				<h3 class="text-lg font-semibold text-white">{translate($language, 'dbm.definition')}: <span class="font-mono">{objectDefinition.name}</span></h3>
				<button type="button" onclick={() => (objectDefinition = null)} class="cursor-pointer rounded-lg p-1.5 text-gray-400 hover:bg-white/5 hover:text-white">✕</button>
			</div>
			<pre class="overflow-x-auto rounded-lg bg-gray-950 p-4 font-mono text-xs leading-relaxed text-gray-200">{objectDefinition.text}</pre>
			<div class="mt-4 flex justify-end">
				<button type="button" onclick={() => (objectDefinition = null)} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 transition hover:bg-gray-700">{translate($language, 'wl.cancel')}</button>
			</div>
		</div>
	</div>
{/if}

{#if pendingDeleteRow}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" role="dialog" aria-modal="true" tabindex="-1" onkeydown={(e) => { if (e.key === 'Escape') pendingDeleteRow = null; }}>
		<div class="w-full max-w-md rounded-xl border border-red-700 bg-gray-800 p-6">
			<h3 class="text-lg font-semibold text-white">{translate($language, 'dbm.delete_row')}</h3>
			<p class="mt-2 text-sm text-gray-300">{translate($language, 'dbm.delete_row_confirm')}</p>
			<div class="mt-5 flex justify-end gap-2">
				<button type="button" onclick={() => (pendingDeleteRow = null)} class="rounded-lg border border-gray-600 px-4 py-2 text-sm text-gray-300 transition hover:bg-gray-700">{translate($language, 'wl.cancel')}</button>
				<button type="button" onclick={confirmDeleteRow} disabled={rowSaving} class="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-red-700 disabled:opacity-50">{translate($language, 'dbm.delete')}</button>
			</div>
		</div>
	</div>
{/if}
