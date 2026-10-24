<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, getCSRFToken } from '#lib/api.js';
	import TaskProgress from '#lib/components/TaskProgress.svelte';
	import Modal from '#lib/components/Modal.svelte';
import { toast } from '#lib/stores/toast.js';
	import { language, translate } from '#lib/stores/language.js';
	import { hasPermission, permissions } from '#lib/stores/auth.js';

	// ── Types ──────────────────────────────────────────────────────
	interface EngineStatus {
		name: string;
		installed: boolean;
		running: boolean;
		version: string;
	}

	interface Database {
		id: string;
		name: string;
		engine: string;
		charset: string;
		created_at: string;
		size_bytes: number;
	}

	interface DbUser {
		id: string;
		username: string;
		engine: string;
	}

	interface Grant {
		user_id: string;
		database_id: string;
		username: string;
		database_name: string;
		engine: string;
		created_at: string;
	}

	// ── State ──────────────────────────────────────────────────────
	let engines = $state<EngineStatus[]>([]);
	let databases = $state<Database[]>([]);
	let dbUsers = $state<DbUser[]>([]);
	let grants = $state<Grant[]>([]);

	let loadingEngines = $state(true);
	let loadingDatabases = $state(true);
	let loadingUsers = $state(true);
	let loadingGrants = $state(true);

	let error = $state('');
	let actionInProgress = $state<string | null>(null);
	let currentTaskId = $state('');
	let pgvector = $state<{ available_version: string; databases: Record<string, boolean> } | null>(null);
	let enablingVector = $state('');
	let engineOperationInProgress = $derived(actionInProgress !== null || !!currentTaskId);

	// Search + engine filter per column
	let dbSearch = $state('');
	let userSearch = $state('');
	let dbEngineFilter = $state('all');
	let userEngineFilter = $state('all');

	// Modals
	let showCreateDb = $state(false);
	let showCreateUser = $state(false);
	type ConfirmAction =
		| { kind: 'delete-db'; id: string; name: string }
		| { kind: 'delete-user'; id: string; name: string }
		| { kind: 'revoke'; userId: string; databaseId: string; username: string; name: string };
	let confirmAction = $state<ConfirmAction | null>(null);
	let confirmBusy = $state(false);

	// Create database form
	let newDbName = $state('');
	let newDbEngine = $state('mysql');
	let newDbCharset = $state('utf8mb4');
	let creatingDb = $state(false);

	// Create user form
	let newUsername = $state('');
	let newPassword = $state('');
	let newUserEngine = $state('mysql');
	let creatingUser = $state(false);
	let showNewPassword = $state(false);

	// Password generator (shared by create + reset forms)
	let genLength = $state(20);
	let genSymbols = $state(true);

	// Reset password state
	let resetPasswordUserId = $state<string | null>(null);
	let resetPasswordValue = $state('');
	let showResetPassword = $state(false);

	// Grant modal: one user, their current access, and grantable databases
	let grantUserId = $state<string | null>(null);
	let grantSearch = $state('');
	let grantBusy = $state('');
	let grantUser = $derived(dbUsers.find((u) => u.id === grantUserId) || null);
	let grantCandidates = $derived.by(() => {
		if (!grantUser) return [];
		const q = grantSearch.trim().toLowerCase();
		return databases.filter((d) => d.engine === grantUser.engine && !isGranted(grantUser.id, d.id) && (!q || d.name.toLowerCase().includes(q)));
	});
	let resetUser = $derived(dbUsers.find((u) => u.id === resetPasswordUserId) || null);

	function openGrant(userId: string) {
		grantUserId = userId;
		grantSearch = '';
	}

	// Copy feedback
	let copiedField = $state('');

	// Export
	let exportMenuId = $state<string | null>(null);
	let exportStatus = $state<{ id: string; message: string; error: boolean } | null>(null);

	async function exportDatabase(id: string, name: string, format: 'sql' | 'sql.gz') {
		exportMenuId = null;
		exportStatus = { id, message: translate($language, 'db.preparing_export'), error: false };
		try {
			const res = await fetch(`/api/v1/databases/${id}/export?format=${encodeURIComponent(format)}`, {
				credentials: 'include'
			});
			if (!res.ok) {
				const json = await res.json().catch(() => null);
				throw new Error(json?.error?.message || translate($language, 'db.export_failed_http').replace('{code}', String(res.status)));
			}
			const blob = await res.blob();
			const a = document.createElement('a');
			a.href = URL.createObjectURL(blob);
			a.download = `${name}-${new Date().toISOString().slice(0, 10)}.${format}`;
			a.click();
			URL.revokeObjectURL(a.href);
			exportStatus = { id, message: translate($language, 'db.exported').replace('{format}', format), error: false };
			setTimeout(() => { if (exportStatus?.id === id) exportStatus = null; }, 3000);
		} catch (err) {
			exportStatus = { id, message: err instanceof Error ? err.message : translate($language, 'db.export_failed'), error: true };
			setTimeout(() => { if (exportStatus?.id === id) exportStatus = null; }, 4000);
		}
	}

	// Restore
	let restoreInput = $state<HTMLInputElement | undefined>();
	let restoreTargetDb = $state<{ id: string; name: string } | null>(null);
	let restorePending = $state<{ id: string; name: string; fileName: string; file: File } | null>(null);
	let restoreBusy = $state(false);
	let restoreStatus = $state<{ id: string; message: string; error: boolean } | null>(null);
	// Client-side mirror of the panel's restore upload cap; null until loaded.
	let restoreLimit = $state<{ max_mb: number; label: string } | null>(null);

	function restoreLimitBytes(): number | null {
		return restoreLimit ? restoreLimit.max_mb * 1024 * 1024 : null;
	}

	function rejectIfTooLarge(id: string, file: File): boolean {
		const limitBytes = restoreLimitBytes();
		if (limitBytes !== null && file.size > limitBytes) {
			restoreStatus = {
				id,
				message: translate($language, 'db.restore_too_large').replace('{limit}', restoreLimit?.label || String(restoreLimit?.max_mb) + ' MB'),
				error: true
			};
			setTimeout(() => { if (restoreStatus?.id === id) restoreStatus = null; }, 6000);
			return true;
		}
		return false;
	}

	async function loadRestoreLimit() {
		try {
			restoreLimit = await api.get<{ max_mb: number; label: string }>('/api/v1/databases/restore-limit');
		} catch {
			// The server still enforces the cap; the pre-check is a convenience.
		}
	}

	function pickRestoreFile(id: string, name: string) {
		restoreTargetDb = { id, name };
		restoreInput?.click();
	}

	async function onRestoreFileSelected(e: Event) {
		const input = e.target as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file || !restoreTargetDb) return;
		if (rejectIfTooLarge(restoreTargetDb.id, file)) return;
		restorePending = {
			id: restoreTargetDb.id,
			name: restoreTargetDb.name,
			fileName: file.name,
			file
		};
	}

	async function confirmRestore() {
		const pending = restorePending;
		if (!pending || restoreBusy) return;
		restoreBusy = true;
		restoreStatus = { id: pending.id, message: translate($language, 'db.restoring_note'), error: false };
		try {
			const form = new FormData();
			form.append('file', pending.file);
			const res = await fetch(`/api/v1/databases/${pending.id}/restore`, {
				method: 'POST',
				headers: { 'X-CSRF-Token': getCSRFToken() },
				credentials: 'include',
				body: form
			});
			if (!res.ok) {
				const json = await res.json().catch(() => null);
				throw new Error(json?.error?.message || translate($language, 'db.restore_failed_http').replace('{code}', String(res.status)));
			}
			restoreStatus = { id: pending.id, message: translate($language, 'db.restored_from').replace('{name}', pending.fileName), error: false };
			setTimeout(() => { if (restoreStatus?.id === pending.id) restoreStatus = null; }, 4000);
		} catch (err) {
			restoreStatus = { id: pending.id, message: err instanceof Error ? err.message : translate($language, 'db.restore_failed'), error: true };
			setTimeout(() => { if (restoreStatus?.id === pending.id) restoreStatus = null; }, 6000);
		} finally {
			restorePending = null;
			restoreBusy = false;
		}
	}

	// ── Helpers ────────────────────────────────────────────────────
	const engineMeta: Record<string, { label: string; badge: string; iconBg: string; initial: string }> = {
		mysql: { label: 'MySQL', badge: 'bg-blue-900/50 text-blue-300', iconBg: 'bg-blue-500/10 text-blue-400', initial: 'M' },
		postgresql: { label: 'PostgreSQL', badge: 'bg-indigo-900/50 text-indigo-300', iconBg: 'bg-indigo-500/10 text-indigo-400', initial: 'P' },
		redis: { label: 'Redis', badge: 'bg-red-900/50 text-red-300', iconBg: 'bg-red-500/10 text-red-400', initial: 'R' }
	};

	function engineBadgeClass(engine: string): string {
		return engineMeta[engine]?.badge || 'bg-gray-700 text-gray-300';
	}

	function engineIconBg(engine: string): string {
		return engineMeta[engine]?.iconBg || 'bg-gray-500/10 text-gray-300';
	}

	function engineLabel(engine: string): string {
		return engineMeta[engine]?.label || engine;
	}

	function engineInitial(engine: string): string {
		return engineMeta[engine]?.initial || engine.charAt(0).toUpperCase();
	}

	function formatSize(bytes: number): string {
		if (!bytes || bytes <= 0) return '';
		const units = ['B', 'KB', 'MB', 'GB', 'TB'];
		let size = bytes;
		let i = 0;
		while (size >= 1024 && i < units.length - 1) { size /= 1024; i++; }
		return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '—';
		try {
			return new Date(dateStr).toLocaleDateString('en-US', {
				year: 'numeric',
				month: 'short',
				day: 'numeric'
			});
		} catch {
			return dateStr;
		}
	}

	function randomInt(max: number): number {
		const buf = new Uint32Array(1);
		const limit = Math.floor(0x100000000 / max) * max;
		let value = 0;
		do {
			crypto.getRandomValues(buf);
			value = buf[0];
		} while (value >= limit);
		return value % max;
	}

	// Ambiguity-free alphabet: no 0/O, 1/l/I look-alikes.
	function generatePassword(length: number, useSymbols: boolean): string {
		const sets = ['abcdefghijkmnopqrstuvwxyz', 'ABCDEFGHJKLMNPQRSTUVWXYZ', '23456789'];
		if (useSymbols) sets.push('!@#$%^&*_+-=');
		const all = sets.join('');
		const chars: string[] = sets.map((set) => set[randomInt(set.length)]);
		while (chars.length < length) chars.push(all[randomInt(all.length)]);
		for (let i = chars.length - 1; i > 0; i--) {
			const j = randomInt(i + 1);
			[chars[i], chars[j]] = [chars[j], chars[i]];
		}
		return chars.slice(0, length).join('');
	}

	let generatedPreview = $derived(generatePassword(genLength, genSymbols));

	function fillGenerated(target: 'new' | 'reset') {
		const pw = generatePassword(genLength, genSymbols);
		if (target === 'new') {
			newPassword = pw;
			showNewPassword = true;
		} else {
			resetPasswordValue = pw;
			showResetPassword = true;
		}
	}

	async function copyText(text: string, field: string) {
		try {
			await navigator.clipboard.writeText(text);
			copiedField = field;
			setTimeout(() => (copiedField = ''), 2000);
		} catch {
			/* clipboard unavailable */
		}
	}

	let filteredDatabases = $derived.by(() => {
		const q = dbSearch.trim().toLowerCase();
		return databases.filter((d) => (dbEngineFilter === 'all' || d.engine === dbEngineFilter) && (!q || d.name.toLowerCase().includes(q)));
	});

	let filteredUsers = $derived.by(() => {
		const q = userSearch.trim().toLowerCase();
		return dbUsers.filter(
			(u) =>
				(userEngineFilter === 'all' || u.engine === userEngineFilter) &&
				(!q || u.username.toLowerCase().includes(q) || grantsForUser(u.id).some((g) => g.database_name.toLowerCase().includes(q)))
		);
	});

	let totalSize = $derived(databases.reduce((sum, d) => sum + (d.size_bytes || 0), 0));
	let ungrantedUsers = $derived(dbUsers.filter((u) => !grants.some((g) => g.user_id === u.id)).length);
	let sqlEngines = $derived(engines.filter((e) => e.name === 'mysql' || e.name === 'postgresql'));

	function openCreateDb(engine = 'mysql') {
		newDbEngine = engine;
		newDbCharset = 'utf8mb4';
		showCreateDb = true;
	}

	function openCreateUser(engine = 'mysql') {
		newUserEngine = engine;
		if (!newPassword) fillGenerated('new');
		showCreateUser = true;
	}

	function openResetPassword(userId: string) {
		resetPasswordUserId = userId;
		resetPasswordValue = '';
		showResetPassword = false;
	}

	function closeResetPassword() {
		resetPasswordUserId = null;
		resetPasswordValue = '';
		showResetPassword = false;
	}

	function grantsForUser(userId: string): Grant[] {
		return grants.filter((g) => g.user_id === userId);
	}

	function grantCountForDb(databaseId: string): number {
		return grants.filter((g) => g.database_id === databaseId).length;
	}

	function grantNamesForDb(databaseId: string): string {
		return grants
			.filter((g) => g.database_id === databaseId)
			.map((g) => g.username)
			.join(', ');
	}

	function isGranted(userId: string, databaseId: string): boolean {
		return grants.some((g) => g.user_id === userId && g.database_id === databaseId);
	}

	function flash(msg: string) {
		toast.success(msg);
	}

	function fail(err: unknown, fallback: string) {
		toast.error(err instanceof Error ? err.message : fallback);
	}

	// ── Loaders ───────────────────────────────────────────────────
	async function loadEngines() {
		loadingEngines = true;
		try {
			engines = (await api.get<EngineStatus[]>('/api/v1/databases/engines')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'db.load_engines_failed');
		} finally {
			loadingEngines = false;
		}
	}

	async function loadDatabases() {
		loadingDatabases = true;
		try {
			databases = (await api.get<Database[]>('/api/v1/databases')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'db.load_databases_failed');
		} finally {
			loadingDatabases = false;
		}
	}

	async function loadUsers() {
		loadingUsers = true;
		try {
			dbUsers = (await api.get<DbUser[]>('/api/v1/databases/users')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'db.load_users_failed');
		} finally {
			loadingUsers = false;
		}
	}

	async function loadGrants() {
		loadingGrants = true;
		try {
			grants = (await api.get<Grant[]>('/api/v1/databases/grants')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : translate($language, 'db.load_grants_failed');
		} finally {
			loadingGrants = false;
		}
	}

	async function refreshAll() {
		error = '';
		await Promise.all([loadEngines(), loadDatabases(), loadUsers(), loadGrants(), loadPgvector()]);
	}

	// ── Engine actions ────────────────────────────────────────────
	async function installEngine(engineName: string) {
		actionInProgress = `install-${engineName}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/databases/engines/${engineName}/install`);
			currentTaskId = result.task_id;
			flash(translate($language, 'db.install_started').replace('{name}', engineLabel(engineName)));
		} catch (err) {
			fail(err, translate($language, 'db.install_failed').replace('{name}', engineLabel(engineName)));
		} finally {
			actionInProgress = null;
		}
	}

	// ── pgvector (RAG embeddings) ─────────────────────────────────
	async function loadPgvector() {
		try {
			pgvector = await api.get<{ available_version: string; databases: Record<string, boolean> }>(
				'/api/v1/databases/engines/postgresql/pgvector'
			);
		} catch {
			// PostgreSQL not installed (or not permitted): keep the row hidden.
			pgvector = null;
		}
	}

	async function installPgvector() {
		actionInProgress = 'install-pgvector';
		try {
			const result = await api.post<{ task_id: string }>('/api/v1/databases/engines/postgresql/install-pgvector');
			currentTaskId = result.task_id;
			flash(translate($language, 'db.pgvectorInstallStarted'));
		} catch (err) {
			fail(err, translate($language, 'db.pgvectorInstallFailed'));
		} finally {
			actionInProgress = null;
		}
	}

	async function enableVector(dbId: string, dbName: string) {
		enablingVector = dbId;
		try {
			await api.post(`/api/v1/databases/${dbId}/enable-pgvector`);
			flash(translate($language, 'db.pgvectorEnabled').replace('{name}', dbName));
			await loadPgvector();
		} catch (err) {
			fail(err, translate($language, 'db.pgvectorEnableFailed').replace('{name}', dbName));
		} finally {
			enablingVector = '';
		}
	}

	async function engineAction(engineName: string, action: 'start' | 'stop' | 'restart') {
		actionInProgress = `${action}-${engineName}`;
		try {
			await api.post(`/api/v1/databases/engines/${engineName}/${action}`);
			flash(translate($language, action === 'start' ? 'db.started' : action === 'stop' ? 'db.stopped' : 'db.restarted').replace('{name}', engineLabel(engineName)));
			await loadEngines();
		} catch (err) {
			fail(err, translate($language, action === 'start' ? 'db.start_failed' : action === 'stop' ? 'db.stop_failed' : 'db.restart_failed').replace('{name}', engineLabel(engineName)));
		} finally {
			actionInProgress = null;
		}
	}

	// ── Database actions ──────────────────────────────────────────
	async function createDatabase() {
		if (!newDbName.trim() || !newDbEngine) return;
		creatingDb = true;
		try {
			await api.post('/api/v1/databases', {
				name: newDbName.trim(),
				engine: newDbEngine,
				charset: newDbCharset.trim() || 'utf8mb4'
			});
			flash(translate($language, 'db.created').replace('{name}', newDbName.trim()));
			showCreateDb = false;
			newDbName = '';
			newDbEngine = 'mysql';
			newDbCharset = 'utf8mb4';
			await loadDatabases();
		} catch (err) {
			fail(err, translate($language, 'db.create_failed'));
		} finally {
			creatingDb = false;
		}
	}

	async function deleteDatabase(id: string, name: string) {
		try {
			await api.del(`/api/v1/databases/${id}`);
			flash(translate($language, 'db.deleted').replace('{name}', name));
			await Promise.all([loadDatabases(), loadGrants()]);
		} catch (err) {
			fail(err, translate($language, 'db.delete_failed'));
		}
	}

	// ── User actions ──────────────────────────────────────────────
	async function createUser() {
		if (!newUsername.trim() || !newPassword.trim() || !newUserEngine) return;
		creatingUser = true;
		try {
			await api.post('/api/v1/databases/users', {
				username: newUsername.trim(),
				password: newPassword.trim(),
				engine: newUserEngine
			});
			flash(translate($language, 'db.user_created').replace('{name}', newUsername.trim()));
			showCreateUser = false;
			newUsername = '';
			newPassword = '';
			showNewPassword = false;
			newUserEngine = 'mysql';
			await loadUsers();
		} catch (err) {
			fail(err, translate($language, 'db.user_create_failed'));
		} finally {
			creatingUser = false;
		}
	}

	async function resetPassword(userId: string) {
		if (!resetPasswordValue.trim()) return;
		try {
			// Backend registers this route as POST only.
			await api.post(`/api/v1/databases/users/${userId}/password`, {
				password: resetPasswordValue.trim()
			});
			flash(translate($language, 'db.password_reset'));
			closeResetPassword();
		} catch (err) {
			fail(err, translate($language, 'db.password_reset_failed'));
		}
	}

	async function grantPrivileges(userId: string, databaseId: string) {
		if (grantBusy) return;
		grantBusy = databaseId;
		try {
			// Handler reads user_id and database_id from the body; the URL param
			// is ignored. database_id is the managed_databases row ID.
			await api.post(`/api/v1/databases/users/${userId}/grant`, {
				user_id: userId,
				database_id: databaseId
			});
			const dbName = databases.find((d) => d.id === databaseId)?.name || databaseId;
			flash(translate($language, 'db.granted').replace('{name}', dbName));
			await loadGrants();
		} catch (err) {
			fail(err, translate($language, 'db.grant_failed'));
		} finally {
			grantBusy = '';
		}
	}

	async function revokePrivileges(userId: string, databaseId: string) {
		try {
			await api.post(`/api/v1/databases/users/${userId}/revoke`, {
				database_id: databaseId
			});
			flash(translate($language, 'db.revoked').replace('{name}', databases.find((d) => d.id === databaseId)?.name || databaseId));
			await loadGrants();
		} catch (err) {
			fail(err, translate($language, 'db.revoke_failed'));
		}
	}

	async function runConfirmed() {
		const action = confirmAction;
		if (!action || confirmBusy) return;
		confirmBusy = true;
		try {
			if (action.kind === 'delete-db') await deleteDatabase(action.id, action.name);
			else if (action.kind === 'delete-user') await deleteUser(action.id, action.name);
			else await revokePrivileges(action.userId, action.databaseId);
		} finally {
			confirmBusy = false;
			confirmAction = null;
		}
	}

	async function deleteUser(id: string, username: string) {
		try {
			await api.del(`/api/v1/databases/users/${id}`);
			flash(translate($language, 'db.user_deleted').replace('{name}', username));
			await Promise.all([loadUsers(), loadGrants()]);
		} catch (err) {
			fail(err, translate($language, 'db.user_delete_failed'));
		}
	}

	// ── Lifecycle ─────────────────────────────────────────────────
	function manageUser(userId: string) {
		goto(`/databases/manage/${userId}`);
	}

	onMount(() => {
		loadEngines();
		loadDatabases();
		loadUsers();
		loadGrants();
		loadRestoreLimit();
		loadPgvector();
	});
</script>

<svelte:window onclick={() => (exportMenuId = null)} />

<input
	bind:this={restoreInput}
	type="file"
	accept=".sql,.sql.gz,.gz"
	class="hidden"
	onchange={onRestoreFileSelected}
/>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<h2 class="text-2xl font-bold text-white">{translate($language, 'db.title')}</h2>
			<div class="mt-1 flex flex-wrap items-center gap-2 text-sm text-gray-400">
				<span>{translate($language, 'db.count_databases').replace('{count}', String(databases.length))}</span>
				<span class="text-gray-600">·</span>
				<span>{translate($language, 'db.count_users').replace('{count}', String(dbUsers.length))}</span>
				<span class="text-gray-600">·</span>
				<span>{translate($language, 'db.count_engines').replace('{count}', String(engines.filter((e) => e.running).length))}</span>
			</div>
		</div>
		<button
			type="button"
			onclick={refreshAll}
			disabled={loadingEngines || loadingDatabases || loadingUsers || loadingGrants}
			class="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-white/8 bg-white/[0.035]
			px-3.5 py-2 text-xs font-medium text-gray-300 transition
			hover:border-blue-400/20 hover:bg-blue-500/8 hover:text-white disabled:opacity-50"
		>
			<svg
				class="h-3.5 w-3.5 {(loadingEngines || loadingDatabases || loadingUsers || loadingGrants) ? 'animate-spin' : ''}"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
				stroke-width="2"
				aria-hidden="true"
			>
				<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992" />
			</svg>
			{translate($language, 'db.refresh')}
		</button>
	</div>

	{#if error}
		<div class="flex items-center justify-between gap-2 rounded-xl border border-red-700 bg-red-900/40 px-3.5 py-2.5 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={() => (error = '')} class="cursor-pointer text-red-400 hover:text-red-200">✕</button>
		</div>
	{/if}



	<!-- ═══════════════════════════ ENGINES ═══════════════════════════ -->
	<section aria-label={translate($language, 'db.engines_aria')}>
		{#if loadingEngines}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each Array(3) as _}
					<div class="h-32 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
				{/each}
			</div>
		{:else if engines.length > 0}
			<div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
				{#each engines as eng (eng.name)}
					<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-5 transition-colors hover:border-blue-400/20">
						<div class="flex items-start justify-between gap-3">
							<div class="flex items-center gap-3">
								<span class="flex h-11 w-11 items-center justify-center rounded-xl text-lg font-bold {engineIconBg(eng.name)}">
									{engineInitial(eng.name)}
								</span>
								<div>
									<p class="font-semibold text-white">{engineLabel(eng.name)}</p>
									<p class="text-xs text-gray-500">{eng.version || (eng.installed ? translate($language, 'db.installed') : translate($language, 'db.not_installed'))}</p>
								</div>
							</div>
							<div class="flex flex-col items-end gap-1.5">
								{#if eng.installed}
									<span class="inline-flex items-center gap-1.5 rounded-full bg-green-900/40 px-2 py-0.5 text-[11px] font-semibold text-green-400">
										<span class="relative flex h-1.5 w-1.5">
											{#if eng.running}
												<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-60"></span>
											{/if}
											<span class="relative inline-flex h-1.5 w-1.5 rounded-full {eng.running ? 'bg-green-400' : 'bg-red-400'}"></span>
										</span>
										{eng.running ? translate($language, 'db.status_running') : translate($language, 'db.status_stopped')}
									</span>
								{:else}
									<span class="rounded-full bg-gray-700/60 px-2 py-0.5 text-[11px] font-semibold text-gray-400">{translate($language, 'db.not_installed')}</span>
								{/if}
							</div>
						</div>
						<div class="mt-4 flex items-center gap-2">
							{#if !eng.installed}
								<button
									type="button"
									onclick={() => installEngine(eng.name)}
									disabled={engineOperationInProgress}
									class="cursor-pointer rounded-lg bg-blue-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
								>
									{actionInProgress === `install-${eng.name}` ? translate($language, 'db.installing') : translate($language, 'db.install')}
								</button>
							{:else}
								{#if !eng.running}
									<button
										type="button"
										onclick={() => engineAction(eng.name, 'start')}
										disabled={engineOperationInProgress}
										class="cursor-pointer rounded-lg bg-green-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-green-700 disabled:opacity-50"
									>
										{actionInProgress === `start-${eng.name}` ? translate($language, 'db.starting') : translate($language, 'db.start')}
									</button>
								{:else}
									<button
										type="button"
										onclick={() => engineAction(eng.name, 'stop')}
										disabled={engineOperationInProgress}
										class="cursor-pointer rounded-lg bg-red-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-red-700 disabled:opacity-50"
									>
										{actionInProgress === `stop-${eng.name}` ? translate($language, 'db.stopping') : translate($language, 'db.stop')}
									</button>
								{/if}
								<button
									type="button"
									onclick={() => engineAction(eng.name, 'restart')}
									disabled={engineOperationInProgress}
									class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3.5 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
								>
									{actionInProgress === `restart-${eng.name}` ? translate($language, 'db.restarting') : translate($language, 'db.restart')}
								</button>
							{/if}
						</div>
							{#if eng.name === 'postgresql' && eng.installed}
								<div class="mt-3 flex items-center justify-between gap-2 rounded-lg border border-white/5 bg-gray-900/40 px-3 py-2">
									<span class="text-[11px] text-gray-400">
										pgvector
										{#if pgvector?.available_version}
											<span class="ml-1 font-mono text-green-400">v{pgvector.available_version}</span>
										{:else}
											<span class="ml-1 text-gray-500">{translate($language, 'db.pgvectorMissing')}</span>
										{/if}
									</span>
									{#if pgvector?.available_version}
										<span class="rounded-full bg-green-900/40 px-2 py-0.5 text-[10px] font-semibold text-green-400">{translate($language, 'db.installed')}</span>
									{:else if hasPermission($permissions, 'databases.config')}
										<button
											type="button"
											onclick={installPgvector}
											disabled={engineOperationInProgress}
											class="cursor-pointer rounded-lg bg-blue-600 px-3 py-1 text-[11px] font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
										>
											{actionInProgress === 'install-pgvector' ? translate($language, 'db.installing') : translate($language, 'db.pgvectorInstall')}
										</button>
									{/if}
								</div>
							{/if}
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<!-- ═══════════════════ DATABASES (left) + USERS (right) ═══════════════════ -->
	<div class="grid items-start gap-6 lg:grid-cols-2">
		<!-- DATABASES -->
		<section class="flex min-w-0 flex-col rounded-2xl border border-white/5 bg-gray-800/60" aria-label={translate($language, 'db.title')}>
			<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-4">
				<div class="flex items-center gap-3">
					<span class="flex h-9 w-9 items-center justify-center rounded-xl bg-blue-500/10 text-blue-400" aria-hidden="true">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6">
							<ellipse cx="12" cy="5.5" rx="8" ry="3" />
							<path d="M4 5.5v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6M4 11.5v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
						</svg>
					</span>
					<div>
						<h3 class="text-sm font-semibold text-white">{translate($language, 'db.title')}</h3>
						<p class="text-[11px] text-gray-500">
							{translate($language, 'db.count_databases').replace('{count}', String(databases.length))}{totalSize > 0 ? ` · ${formatSize(totalSize)}` : ''}
						</p>
					</div>
				</div>
				<button
					type="button"
					onclick={() => openCreateDb()}
					class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-blue-700"
				>
					<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" d="M12 5v14m7-7H5" /></svg>
					{translate($language, 'db.create_database')}
				</button>
			</div>

			<div class="flex gap-2 border-b border-white/5 px-5 py-3">
				<div class="relative min-w-0 flex-1">
					<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
					</svg>
					<input
						type="search"
						bind:value={dbSearch}
						placeholder={translate($language, 'db.search_databases')}
						aria-label={translate($language, 'db.search_databases')}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				<select
					bind:value={dbEngineFilter}
					aria-label={translate($language, 'db.filter_engine')}
					class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				>
					<option value="all">{translate($language, 'db.all_engines')}</option>
					<option value="mysql">MySQL</option>
					<option value="postgresql">PostgreSQL</option>
				</select>
			</div>

			{#if loadingDatabases}
				<div class="divide-y divide-gray-700/40">
					{#each Array(4) as _}
						<div class="flex items-center gap-3 px-5 py-3">
							<div class="h-9 w-9 animate-pulse rounded-lg bg-gray-700/50"></div>
							<div class="h-4 w-44 animate-pulse rounded bg-gray-700/50"></div>
							<div class="ml-auto h-4 w-20 animate-pulse rounded bg-gray-700/50"></div>
						</div>
					{/each}
				</div>
			{:else if filteredDatabases.length === 0}
				<div class="flex flex-col items-center gap-2 px-5 py-12 text-center">
					<p class="text-sm text-gray-400">{dbSearch ? translate($language, 'db.no_match').replace('{query}', dbSearch) : translate($language, 'db.none_yet')}</p>
					{#if !dbSearch && dbEngineFilter === 'all'}
						<button type="button" onclick={() => openCreateDb()} class="cursor-pointer text-sm font-medium text-blue-400 hover:underline">
							+ {translate($language, 'db.create_database')}
						</button>
					{/if}
				</div>
			{:else}
				<ul class="divide-y divide-gray-700/40">
					{#each filteredDatabases as db (db.id)}
						{@const users = grants.filter((g) => g.database_id === db.id)}
						<li class="group px-5 py-3 transition hover:bg-white/[0.02]">
							<div class="flex items-center gap-3">
								<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-xs font-bold {engineIconBg(db.engine)}" title={engineLabel(db.engine)}>
									{engineInitial(db.engine)}
								</span>
								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-1.5">
										<button
											type="button"
											onclick={() => copyText(db.name, `db-${db.id}`)}
											title={translate($language, 'db.click_copy_name')}
											class="cursor-pointer truncate text-left font-mono text-sm font-medium text-gray-100 hover:text-blue-300"
										>
											{db.name}
										</button>
										{#if copiedField === `db-${db.id}`}
											<span class="text-[10px] font-medium text-green-400">{translate($language, 'db.copied')}</span>
										{/if}
										{#if db.engine === 'postgresql' && pgvector?.available_version && pgvector.databases[db.name]}
											<span class="rounded border border-green-800/60 bg-green-900/30 px-1.5 text-[10px] font-semibold text-green-400">vector</span>
										{/if}
									</div>
									<p class="truncate text-[11px] text-gray-500">
										{engineLabel(db.engine)}{db.engine === 'mysql' && db.charset ? ` · ${db.charset}` : ''}{formatSize(db.size_bytes) ? ` · ${formatSize(db.size_bytes)}` : ''} · {formatDate(db.created_at)}
									</p>
								</div>
								<div class="relative flex shrink-0 items-center gap-0.5">
									{#if db.engine === 'postgresql' && pgvector?.available_version && !pgvector.databases[db.name] && hasPermission($permissions, 'databases.create')}
										<button
											type="button"
											onclick={() => enableVector(db.id, db.name)}
											disabled={enablingVector !== ''}
											title={translate($language, 'db.pgvectorEnableTitle')}
											class="cursor-pointer rounded-md border border-gray-700 px-1.5 py-0.5 text-[10px] font-medium text-gray-300 transition hover:border-green-700 hover:text-green-400 disabled:opacity-50"
										>
											{enablingVector === db.id ? '…' : '+ vector'}
										</button>
									{/if}
									{#if db.engine !== 'redis'}
										<button
											type="button"
											onclick={(e) => { e.stopPropagation(); exportMenuId = exportMenuId === db.id ? null : db.id; }}
											title={translate($language, 'db.export_title')}
											aria-haspopup="menu"
											aria-expanded={exportMenuId === db.id}
											class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white {exportMenuId === db.id ? 'bg-gray-700 text-white' : ''}"
										>
											<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6" aria-hidden="true">
												<path stroke-linecap="round" stroke-linejoin="round" d="M7.5 7.5h-.75A2.25 2.25 0 0 0 4.5 9.75v7.5a2.25 2.25 0 0 0 2.25 2.25h7.5a2.25 2.25 0 0 0 2.25-2.25v-7.5a2.25 2.25 0 0 0-2.25-2.25h-.75m-6 3.75 3 3m0 0 3-3m-3 3V1.5m6 9h.75a2.25 2.25 0 0 1 2.25 2.25v7.5a2.25 2.25 0 0 1-2.25 2.25h-7.5a2.25 2.25 0 0 1-2.25-2.25v-.75" />
											</svg>
										</button>
										{#if exportMenuId === db.id}
											<div class="absolute top-full right-0 z-20 mt-1 w-48 overflow-hidden rounded-lg border border-gray-700 bg-gray-800 shadow-xl" role="menu">
												<button type="button" role="menuitem" onclick={() => exportDatabase(db.id, db.name, 'sql')} class="block w-full cursor-pointer px-3 py-2 text-left text-xs text-gray-200 transition hover:bg-gray-700">
													{translate($language, 'db.sql_dump')} <span class="text-gray-500">(.sql)</span>
												</button>
												<button type="button" role="menuitem" onclick={() => exportDatabase(db.id, db.name, 'sql.gz')} class="block w-full cursor-pointer px-3 py-2 text-left text-xs text-gray-200 transition hover:bg-gray-700">
													{translate($language, 'db.sql_gz')} <span class="text-gray-500">(.sql.gz)</span>
												</button>
												<div class="border-t border-gray-700"></div>
												<button type="button" role="menuitem" onclick={() => { exportMenuId = null; pickRestoreFile(db.id, db.name); }} class="block w-full cursor-pointer px-3 py-2 text-left text-xs text-yellow-300 transition hover:bg-gray-700">
													{translate($language, 'db.restore_from_file')}
												</button>
											</div>
										{/if}
									{/if}
									<button
										type="button"
										onclick={() => (confirmAction = { kind: 'delete-db', id: db.id, name: db.name })}
										title={translate($language, 'db.delete_title')}
										aria-label={translate($language, 'db.delete_title')}
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-500/15 hover:text-red-400"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
										</svg>
									</button>
								</div>
							</div>
							<div class="mt-1.5 flex flex-wrap items-center gap-1 pl-12">
								{#if users.length === 0}
									<span class="text-[11px] text-yellow-500/80">{translate($language, 'db.no_users_access')}</span>
								{:else}
									{#each users as g (g.user_id)}
										<button
											type="button"
											onclick={() => openGrant(g.user_id)}
											title={translate($language, 'db.manage_access_for').replace('{name}', g.username)}
											class="inline-flex cursor-pointer items-center gap-1 rounded-md border border-gray-700 bg-gray-900/60 px-1.5 py-0.5 font-mono text-[10px] text-gray-300 transition hover:border-indigo-500/50 hover:text-indigo-200"
										>
											<svg class="h-2.5 w-2.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
												<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0" />
											</svg>
											{g.username}
										</button>
									{/each}
								{/if}
							</div>
							{#each [exportStatus, restoreStatus] as status}
								{#if status?.id === db.id}
									<p class="mt-1.5 pl-12 text-[11px] {status.error ? 'text-red-400' : 'text-green-400'}">{status.message}</p>
								{/if}
							{/each}
						</li>
					{/each}
				</ul>
			{/if}
		</section>

		<!-- USERS -->
		<section class="flex min-w-0 flex-col rounded-2xl border border-white/5 bg-gray-800/60" aria-label={translate($language, 'db.users_title')}>
			<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-4">
				<div class="flex items-center gap-3">
					<span class="flex h-9 w-9 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-400" aria-hidden="true">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6">
							<path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z" />
						</svg>
					</span>
					<div>
						<h3 class="text-sm font-semibold text-white">{translate($language, 'db.users_title')}</h3>
						<p class="text-[11px] text-gray-500">
							{translate($language, 'db.count_users').replace('{count}', String(dbUsers.length))}{ungrantedUsers > 0 ? ` · ${translate($language, 'db.users_without_access').replace('{count}', String(ungrantedUsers))}` : ''}
						</p>
					</div>
				</div>
				<button
					type="button"
					onclick={() => openCreateUser()}
					class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-indigo-700"
				>
					<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" d="M12 5v14m7-7H5" /></svg>
					{translate($language, 'db.create_user')}
				</button>
			</div>

			<div class="flex gap-2 border-b border-white/5 px-5 py-3">
				<div class="relative min-w-0 flex-1">
					<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
					</svg>
					<input
						type="search"
						bind:value={userSearch}
						placeholder={translate($language, 'db.search_users')}
						aria-label={translate($language, 'db.search_users')}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				<select
					bind:value={userEngineFilter}
					aria-label={translate($language, 'db.filter_engine')}
					class="rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1.5 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				>
					<option value="all">{translate($language, 'db.all_engines')}</option>
					<option value="mysql">MySQL</option>
					<option value="postgresql">PostgreSQL</option>
				</select>
			</div>

			{#if loadingUsers}
				<div class="divide-y divide-gray-700/40">
					{#each Array(3) as _}
						<div class="flex items-center gap-3 px-5 py-3">
							<div class="h-9 w-9 animate-pulse rounded-lg bg-gray-700/50"></div>
							<div class="h-4 w-36 animate-pulse rounded bg-gray-700/50"></div>
							<div class="ml-auto h-4 w-24 animate-pulse rounded bg-gray-700/50"></div>
						</div>
					{/each}
				</div>
			{:else if filteredUsers.length === 0}
				<div class="flex flex-col items-center gap-2 px-5 py-12 text-center">
					<p class="text-sm text-gray-400">{userSearch ? translate($language, 'db.no_users_match').replace('{query}', userSearch) : translate($language, 'db.no_users_yet')}</p>
					{#if !userSearch && userEngineFilter === 'all'}
						<button type="button" onclick={() => openCreateUser()} class="cursor-pointer text-sm font-medium text-indigo-400 hover:underline">
							+ {translate($language, 'db.create_user')}
						</button>
					{/if}
				</div>
			{:else}
				<ul class="divide-y divide-gray-700/40">
					{#each filteredUsers as u (u.id)}
						{@const userGrants = grantsForUser(u.id)}
						<li class="px-5 py-3 transition hover:bg-white/[0.02]">
							<div class="flex items-center gap-3">
								<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg {engineIconBg(u.engine)}" title={engineLabel(u.engine)} aria-hidden="true">
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6">
										<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z" />
									</svg>
								</span>
								<div class="min-w-0 flex-1">
									<div class="flex items-center gap-1.5">
										<button
											type="button"
											onclick={() => copyText(u.username, `user-${u.id}`)}
											title={translate($language, 'db.click_copy_username')}
											class="cursor-pointer truncate text-left font-mono text-sm font-medium text-gray-100 hover:text-blue-300"
										>
											{u.username}
										</button>
										{#if copiedField === `user-${u.id}`}
											<span class="text-[10px] font-medium text-green-400">{translate($language, 'db.copied')}</span>
										{/if}
									</div>
									<p class="text-[11px] text-gray-500">
										{engineLabel(u.engine)} · {translate($language, userGrants.length === 1 ? 'db.access_count_one' : 'db.access_count').replace('{count}', String(userGrants.length))}
									</p>
								</div>
								<div class="flex shrink-0 items-center gap-0.5">
									<button
										type="button"
										onclick={() => openGrant(u.id)}
										class="cursor-pointer rounded-lg border border-indigo-500/40 bg-indigo-500/10 px-2.5 py-1 text-[11px] font-semibold text-indigo-200 transition hover:bg-indigo-500/20"
									>
										{translate($language, 'db.grant')}
									</button>
									<button
										type="button"
										onclick={() => manageUser(u.id)}
										title={translate($language, 'db.manage')}
										aria-label={translate($language, 'db.manage')}
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M3.375 19.5h17.25m-17.25 0a1.125 1.125 0 0 1-1.125-1.125M3.375 19.5h7.5c.621 0 1.125-.504 1.125-1.125m-9.75 0V5.625m0 12.75v-1.5c0-.621.504-1.125 1.125-1.125m18.375 2.625V5.625m0 12.75c0 .621-.504 1.125-1.125 1.125m1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125m0 3.75h-7.5A1.125 1.125 0 0 1 12 18.375m9.75-12.75c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125m19.5 0v1.5c0 .621-.504 1.125-1.125 1.125M2.25 5.625v1.5c0 .621.504 1.125 1.125 1.125m0 0h17.25m-17.25 0h7.5c.621 0 1.125.504 1.125 1.125M3.375 8.25c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125m17.25-3.75h-7.5c-.621 0-1.125.504-1.125 1.125m8.625-1.125c.621 0 1.125.504 1.125 1.125v1.5c0 .621-.504 1.125-1.125 1.125m-17.25 0h7.5m-7.5 0c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125M12 10.875v-1.5m0 1.5c0 .621-.504 1.125-1.125 1.125M12 10.875c0 .621.504 1.125 1.125 1.125m-2.25 0c.621 0 1.125.504 1.125 1.125M13.125 12h7.5m-7.5 0c-.621 0-1.125.504-1.125 1.125M20.625 12c.621 0 1.125.504 1.125 1.125v1.5c0 .621-.504 1.125-1.125 1.125m-17.25 0h7.5M12 14.625v-1.5m0 1.5c0 .621-.504 1.125-1.125 1.125M12 14.625c0 .621.504 1.125 1.125 1.125m-2.25 0c.621 0 1.125.504 1.125 1.125m0 1.5v-1.5m0 0c0-.621.504-1.125 1.125-1.125m0 0h7.5" />
										</svg>
									</button>
									<button
										type="button"
										onclick={() => openResetPassword(u.id)}
										title={translate($language, 'db.reset_password')}
										aria-label={translate($language, 'db.reset_password')}
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-yellow-500/15 hover:text-yellow-300"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z" />
										</svg>
									</button>
									<button
										type="button"
										onclick={() => (confirmAction = { kind: 'delete-user', id: u.id, name: u.username })}
										title={translate($language, 'db.delete_user_title')}
										aria-label={translate($language, 'db.delete_user_title')}
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-500/15 hover:text-red-400"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.6" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
										</svg>
									</button>
								</div>
							</div>
							{#if userGrants.length > 0}
								<div class="mt-1.5 flex flex-wrap items-center gap-1 pl-12">
									{#each userGrants as g (g.database_id)}
										<span class="inline-flex items-center gap-1 rounded-md border border-indigo-700/40 bg-indigo-900/20 py-0.5 pl-1.5 pr-0.5 font-mono text-[10px] text-indigo-300">
											{g.database_name}
											<button
												type="button"
												onclick={() => (confirmAction = { kind: 'revoke', userId: u.id, databaseId: g.database_id, username: u.username, name: g.database_name })}
												title={translate($language, 'db.revoke')}
												aria-label="{translate($language, 'db.revoke')} {g.database_name}"
												class="cursor-pointer rounded px-0.5 text-indigo-400/70 transition hover:bg-red-500/20 hover:text-red-300"
											>
												<svg class="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true"><path stroke-linecap="round" d="M6 18L18 6M6 6l12 12" /></svg>
											</button>
										</span>
									{/each}
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_db_task" onComplete={() => { currentTaskId = ''; loadEngines(); loadPgvector(); }} />
</div>

{#snippet passwordField(id: string, value: string, shown: boolean, oninput: (v: string) => void, toggle: () => void, generate: () => void, placeholder: string)}
	<div class="flex items-center gap-2">
		<div class="relative min-w-0 flex-1">
			<input
				{id}
				type={shown ? 'text' : 'password'}
				{value}
				oninput={(e) => oninput(e.currentTarget.value)}
				{placeholder}
				autocomplete="new-password"
				class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 pr-9 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
			/>
			<button
				type="button"
				onclick={toggle}
				title={shown ? translate($language, 'db.hide_password') : translate($language, 'db.show_password')}
				aria-label={shown ? translate($language, 'db.hide_password') : translate($language, 'db.show_password')}
				class="absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer text-gray-500 transition hover:text-gray-200"
			>
				<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
					{#if shown}
						<path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
					{:else}
						<path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.964-7.178z" />
						<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
					{/if}
				</svg>
			</button>
		</div>
		<button
			type="button"
			onclick={generate}
			title={translate($language, 'db.generate_title')}
			class="shrink-0 cursor-pointer rounded-lg border border-blue-400/30 bg-blue-500/10 px-3 py-2 text-sm font-semibold text-blue-300 transition hover:bg-blue-500/20"
		>
			⚄ {translate($language, 'db.generate')}
		</button>
		{#if value}
			<button
				type="button"
				onclick={() => copyText(value, `pw-${id}`)}
				class="shrink-0 cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3 py-2 text-sm text-gray-200 transition hover:bg-gray-600"
			>
				{copiedField === `pw-${id}` ? translate($language, 'db.copied') : translate($language, 'db.copy')}
			</button>
		{/if}
	</div>
	<div class="mt-2 flex flex-wrap items-center gap-4 text-xs text-gray-400">
		<label class="flex items-center gap-2">
			<span>{translate($language, 'db.length')}</span>
			<input type="range" min="12" max="40" bind:value={genLength} class="w-28 accent-blue-500" />
			<span class="w-6 text-center font-mono font-semibold text-gray-200">{genLength}</span>
		</label>
		<label class="flex cursor-pointer items-center gap-2">
			<input type="checkbox" bind:checked={genSymbols} class="accent-blue-500" />
			<span>{translate($language, 'db.include_symbols')}</span>
		</label>
	</div>
{/snippet}

{#if showCreateDb}
	<Modal title={translate($language, 'db.new_database')} onclose={() => (showCreateDb = false)} busy={creatingDb} closeLabel={translate($language, 'db.cancel')}>
		<form id="create-db-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); createDatabase(); }}>
			<div>
				<span class="mb-1.5 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.engine')}</span>
				<div class="grid grid-cols-2 gap-2">
					{#each ['mysql', 'postgresql'] as engine}
						{@const status = sqlEngines.find((e) => e.name === engine)}
						<button
							type="button"
							onclick={() => (newDbEngine = engine)}
							aria-pressed={newDbEngine === engine}
							class="flex cursor-pointer items-center gap-2.5 rounded-xl border px-3 py-2.5 text-left transition {newDbEngine === engine ? 'border-blue-500 bg-blue-500/10' : 'border-gray-600 hover:border-gray-500'}"
						>
							<span class="flex h-8 w-8 items-center justify-center rounded-lg text-sm font-bold {engineIconBg(engine)}">{engineInitial(engine)}</span>
							<span>
								<span class="block text-sm font-medium text-white">{engineLabel(engine)}</span>
								<span class="block text-[11px] {status?.running ? 'text-green-400' : 'text-gray-500'}">
									{status?.installed ? (status.running ? translate($language, 'db.status_running') : translate($language, 'db.status_stopped')) : translate($language, 'db.not_installed')}
								</span>
							</span>
						</button>
					{/each}
				</div>
			</div>
			<div>
				<label for="db-name" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.name')}</label>
				<input
					id="db-name"
					type="text"
					bind:value={newDbName}
					placeholder="my_database"
					autocomplete="off"
					spellcheck="false"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				/>
				<p class="mt-1 text-[11px] text-gray-500">{translate($language, 'db.name_hint')}</p>
			</div>
			{#if newDbEngine === 'mysql'}
				<div>
					<label for="db-charset" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.charset')}</label>
					<input
						id="db-charset"
						type="text"
						list="db-charset-options"
						bind:value={newDbCharset}
						placeholder="utf8mb4"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
					<datalist id="db-charset-options">
						<option value="utf8mb4"></option>
						<option value="utf8mb3"></option>
						<option value="latin1"></option>
					</datalist>
				</div>
			{/if}
		</form>
		{#snippet footer()}
			<button type="button" onclick={() => (showCreateDb = false)} disabled={creatingDb} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'db.cancel')}
			</button>
			<button type="submit" form="create-db-form" disabled={creatingDb || !newDbName.trim()} class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">
				{creatingDb ? translate($language, 'db.adding') : translate($language, 'db.create_database')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if showCreateUser}
	<Modal title={translate($language, 'db.new_user')} onclose={() => (showCreateUser = false)} busy={creatingUser} size="lg" closeLabel={translate($language, 'db.cancel')}>
		<form id="create-user-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); createUser(); }}>
			<div class="grid gap-4 sm:grid-cols-[1fr_auto]">
				<div>
					<label for="user-name" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.username')}</label>
					<input
						id="user-name"
						type="text"
						bind:value={newUsername}
						placeholder="db_user"
						autocomplete="off"
						spellcheck="false"
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				<div>
					<label for="user-engine" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.engine')}</label>
					<select
						id="user-engine"
						bind:value={newUserEngine}
						class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					>
						<option value="mysql">MySQL</option>
						<option value="postgresql">PostgreSQL</option>
					</select>
				</div>
			</div>
			<div>
				<label for="user-pass" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.password')}</label>
				{@render passwordField('user-pass', newPassword, showNewPassword, (v) => (newPassword = v), () => (showNewPassword = !showNewPassword), () => fillGenerated('new'), translate($language, 'db.password_placeholder'))}
				<p class="mt-2 text-[11px] text-gray-500">{translate($language, 'db.save_password_hint')}</p>
			</div>
		</form>
		{#snippet footer()}
			<button type="button" onclick={() => (showCreateUser = false)} disabled={creatingUser} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'db.cancel')}
			</button>
			<button type="submit" form="create-user-form" disabled={creatingUser || !newUsername.trim() || !newPassword.trim()} class="cursor-pointer rounded-lg bg-indigo-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-40">
				{creatingUser ? translate($language, 'db.creating') : translate($language, 'db.create_user')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if resetUser}
	<Modal title={`${translate($language, 'db.reset_for')} ${resetUser.username}`} onclose={closeResetPassword} size="lg" closeLabel={translate($language, 'db.cancel')}>
		<form id="reset-pw-form" onsubmit={(e) => { e.preventDefault(); if (resetUser) resetPassword(resetUser.id); }}>
			<label for="reset-pass" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">{translate($language, 'db.new_password_placeholder')}</label>
			{@render passwordField('reset-pass', resetPasswordValue, showResetPassword, (v) => (resetPasswordValue = v), () => (showResetPassword = !showResetPassword), () => fillGenerated('reset'), translate($language, 'db.new_password_placeholder'))}
			<p class="mt-3 text-[11px] text-yellow-300/80">{translate($language, 'db.reset_warning')}</p>
		</form>
		{#snippet footer()}
			<button type="button" onclick={closeResetPassword} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600">
				{translate($language, 'db.cancel')}
			</button>
			<button type="submit" form="reset-pw-form" disabled={!resetPasswordValue.trim()} class="cursor-pointer rounded-lg bg-yellow-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-yellow-700 disabled:cursor-not-allowed disabled:opacity-40">
				{translate($language, 'db.save')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if grantUser}
	{@const current = grantsForUser(grantUser.id)}
	<Modal title={translate($language, 'db.grant_modal_title').replace('{name}', grantUser.username)} onclose={() => (grantUserId = null)} busy={!!grantBusy} size="lg" closeLabel={translate($language, 'db.cancel')}>
		<div class="space-y-5">
			<div class="flex items-center gap-2 text-xs text-gray-400">
				<span class="rounded-md px-2 py-0.5 text-[11px] font-semibold {engineBadgeClass(grantUser.engine)}">{engineLabel(grantUser.engine)}</span>
				{translate($language, 'db.grant_modal_hint').replace('{engine}', engineLabel(grantUser.engine))}
			</div>

			<div>
				<h4 class="mb-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400">
					{translate($language, 'db.current_access')} <span class="text-gray-500">({current.length})</span>
				</h4>
				{#if current.length === 0}
					<p class="rounded-lg border border-dashed border-gray-600 px-3 py-3 text-center text-xs text-gray-500">{translate($language, 'db.no_access_yet')}</p>
				{:else}
					<ul class="divide-y divide-gray-700/60 rounded-lg border border-gray-700">
						{#each current as g (g.database_id)}
							<li class="flex items-center justify-between gap-2 px-3 py-2">
								<span class="flex min-w-0 items-center gap-2">
									<span class="h-1.5 w-1.5 shrink-0 rounded-full bg-green-400"></span>
									<span class="truncate font-mono text-sm text-gray-100">{g.database_name}</span>
								</span>
								<button
									type="button"
									onclick={() => (confirmAction = { kind: 'revoke', userId: grantUser.id, databaseId: g.database_id, username: grantUser.username, name: g.database_name })}
									class="shrink-0 cursor-pointer rounded-md px-2 py-1 text-xs text-red-300 transition hover:bg-red-500/15"
								>
									{translate($language, 'db.revoke')}
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>

			<div>
				<div class="mb-2 flex items-center justify-between gap-2">
					<h4 class="text-[11px] font-semibold uppercase tracking-wider text-gray-400">{translate($language, 'db.add_access')}</h4>
					<input
						type="search"
						bind:value={grantSearch}
						placeholder={translate($language, 'db.search_databases')}
						aria-label={translate($language, 'db.search_databases')}
						class="w-44 rounded-lg border border-gray-600 bg-gray-900 px-2.5 py-1 text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				{#if grantCandidates.length === 0}
					<div class="rounded-lg border border-dashed border-gray-600 px-3 py-4 text-center text-xs text-gray-500">
						{grantSearch ? translate($language, 'db.no_match').replace('{query}', grantSearch) : translate($language, 'db.no_grantable').replace('{engine}', engineLabel(grantUser.engine))}
						{#if !grantSearch}
							<button
								type="button"
								onclick={() => { const engine = grantUser?.engine || 'mysql'; grantUserId = null; openCreateDb(engine); }}
								class="mt-1 block w-full cursor-pointer font-medium text-blue-400 hover:underline"
							>
								+ {translate($language, 'db.create_database')}
							</button>
						{/if}
					</div>
				{:else}
					<ul class="max-h-64 divide-y divide-gray-700/60 overflow-y-auto rounded-lg border border-gray-700">
						{#each grantCandidates as db (db.id)}
							<li class="flex items-center justify-between gap-2 px-3 py-2">
								<span class="min-w-0">
									<span class="block truncate font-mono text-sm text-gray-200">{db.name}</span>
									<span class="block text-[11px] text-gray-500">{formatSize(db.size_bytes) || '—'} · {formatDate(db.created_at)}</span>
								</span>
								<button
									type="button"
									onclick={() => grantUser && grantPrivileges(grantUser.id, db.id)}
									disabled={!!grantBusy}
									class="shrink-0 cursor-pointer rounded-md bg-green-600 px-3 py-1 text-xs font-semibold text-white transition hover:bg-green-700 disabled:opacity-50"
								>
									{grantBusy === db.id ? '…' : translate($language, 'db.grant')}
								</button>
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		</div>
		{#snippet footer()}
			<button type="button" onclick={() => (grantUserId = null)} disabled={!!grantBusy} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'db.done')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if restorePending}
	<Modal title={translate($language, 'db.restore_title')} onclose={() => (restorePending = null)} busy={restoreBusy} closeLabel={translate($language, 'db.cancel')}>
		<p class="text-sm leading-6 text-gray-300">
			{translate($language, 'db.overwrite')} <span class="font-mono font-semibold text-white">{restorePending.name}</span> {translate($language, 'db.with')}
			<span class="font-mono text-gray-100">{restorePending.fileName}</span>? {translate($language, 'db.data_lost')}
		</p>
		{#snippet footer()}
			<button type="button" onclick={() => (restorePending = null)} disabled={restoreBusy} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'db.cancel')}
			</button>
			<button type="button" onclick={confirmRestore} disabled={restoreBusy} class="cursor-pointer rounded-lg bg-yellow-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-yellow-500 disabled:opacity-50">
				{restoreBusy ? translate($language, 'db.restoring') : translate($language, 'db.restore')}
			</button>
		{/snippet}
	</Modal>
{/if}

{#if confirmAction}
	{@const action = confirmAction}
	<Modal
		title={translate($language, action.kind === 'delete-db' ? 'db.delete_title' : action.kind === 'delete-user' ? 'db.delete_user_title' : 'db.revoke')}
		onclose={() => (confirmAction = null)}
		busy={confirmBusy}
		size="sm"
		closeLabel={translate($language, 'db.cancel')}
	>
		<p class="text-sm leading-6 text-gray-300">
			{#if action.kind === 'delete-db'}
				{translate($language, 'db.delete_db_body')}
			{:else if action.kind === 'delete-user'}
				{translate($language, 'db.delete_user_body')}
			{:else}
				{translate($language, 'db.revoke_body').replace('{user}', action.username)}
			{/if}
		</p>
		<pre class="mt-3 overflow-x-auto rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap text-gray-200">{action.name}</pre>
		{#if action.kind === 'delete-db' && grantCountForDb(action.id) > 0}
			<p class="mt-2 text-xs text-yellow-300">{translate($language, 'db.delete_db_users_note').replace('{names}', grantNamesForDb(action.id))}</p>
		{/if}
		{#snippet footer()}
			<button type="button" onclick={() => (confirmAction = null)} disabled={confirmBusy} class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-40">
				{translate($language, 'db.no')}
			</button>
			<button type="button" onclick={runConfirmed} disabled={confirmBusy} class="cursor-pointer rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-red-700 disabled:opacity-50">
				{confirmBusy ? '…' : translate($language, action.kind === 'revoke' ? 'db.revoke' : 'db.yes_delete')}
			</button>
		{/snippet}
	</Modal>
{/if}
