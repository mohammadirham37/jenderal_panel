<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';
	import TaskProgress from '$lib/components/TaskProgress.svelte';

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
	}

	interface DbUser {
		id: string;
		username: string;
		engine: string;
	}

	// ── State ──────────────────────────────────────────────────────
	let engines = $state<EngineStatus[]>([]);
	let databases = $state<Database[]>([]);
	let dbUsers = $state<DbUser[]>([]);

	let loadingEngines = $state(true);
	let loadingDatabases = $state(true);
	let loadingUsers = $state(true);

	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let actionInProgress = $state<string | null>(null);
	let currentTaskId = $state('');
	let engineOperationInProgress = $derived(actionInProgress !== null || !!currentTaskId);

	// Search
	let dbSearch = $state('');
	let userSearch = $state('');

	// Create database form
	let newDbName = $state('');
	let newDbEngine = $state('mysql');
	let newDbCharset = $state('utf8mb4');
	let creatingDb = $state(false);

	// Delete database confirm
	let deleteDbConfirmId = $state<string | null>(null);

	// Create user form
	let newUsername = $state('');
	let newPassword = $state('');
	let newUserEngine = $state('mysql');
	let creatingUser = $state(false);
	let showNewPassword = $state(false);

	// Password generator (shared by create + reset forms)
	let genLength = $state(20);
	let genSymbols = $state(true);

	// Delete user confirm
	let deleteUserConfirmId = $state<string | null>(null);

	// Reset password state
	let resetPasswordUserId = $state<string | null>(null);
	let resetPasswordValue = $state('');
	let showResetPassword = $state(false);

	// Grant privileges state
	let grantUserId = $state<string | null>(null);
	let grantDatabase = $state('');

	// Copy feedback
	let copiedField = $state('');

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
		return q ? databases.filter((d) => d.name.toLowerCase().includes(q)) : databases;
	});

	let filteredUsers = $derived.by(() => {
		const q = userSearch.trim().toLowerCase();
		return q ? dbUsers.filter((u) => u.username.toLowerCase().includes(q)) : dbUsers;
	});

	function flash(msg: string) {
		actionMsg = msg;
		actionError = '';
	}

	function fail(err: unknown, fallback: string) {
		actionError = err instanceof Error ? err.message : fallback;
		actionMsg = '';
	}

	// ── Loaders ───────────────────────────────────────────────────
	async function loadEngines() {
		loadingEngines = true;
		try {
			engines = (await api.get<EngineStatus[]>('/api/v1/databases/engines')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load database engines';
		} finally {
			loadingEngines = false;
		}
	}

	async function loadDatabases() {
		loadingDatabases = true;
		try {
			databases = (await api.get<Database[]>('/api/v1/databases')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load databases';
		} finally {
			loadingDatabases = false;
		}
	}

	async function loadUsers() {
		loadingUsers = true;
		try {
			dbUsers = (await api.get<DbUser[]>('/api/v1/databases/users')) || [];
		} catch (err) {
			if (!error) error = err instanceof Error ? err.message : 'Failed to load database users';
		} finally {
			loadingUsers = false;
		}
	}

	async function refreshAll() {
		error = '';
		await Promise.all([loadEngines(), loadDatabases(), loadUsers()]);
	}

	// ── Engine actions ────────────────────────────────────────────
	async function installEngine(engineName: string) {
		actionMsg = '';
		actionError = '';
		actionInProgress = `install-${engineName}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/databases/engines/${engineName}/install`);
			currentTaskId = result.task_id;
			flash(`${engineLabel(engineName)} installation started.`);
		} catch (err) {
			fail(err, `Failed to install ${engineLabel(engineName)}`);
		} finally {
			actionInProgress = null;
		}
	}

	async function engineAction(engineName: string, action: 'start' | 'stop' | 'restart') {
		actionMsg = '';
		actionError = '';
		actionInProgress = `${action}-${engineName}`;
		try {
			await api.post(`/api/v1/databases/engines/${engineName}/${action}`);
			flash(`${engineLabel(engineName)} ${action === 'start' ? 'started' : action === 'stop' ? 'stopped' : 'restarted'} successfully.`);
			await loadEngines();
		} catch (err) {
			fail(err, `Failed to ${action} ${engineLabel(engineName)}`);
		} finally {
			actionInProgress = null;
		}
	}

	// ── Database actions ──────────────────────────────────────────
	async function createDatabase() {
		if (!newDbName.trim() || !newDbEngine) return;
		creatingDb = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/databases', {
				name: newDbName.trim(),
				engine: newDbEngine,
				charset: newDbCharset.trim() || 'utf8mb4'
			});
			flash(`Database "${newDbName.trim()}" created successfully.`);
			newDbName = '';
			newDbEngine = 'mysql';
			newDbCharset = 'utf8mb4';
			await loadDatabases();
		} catch (err) {
			fail(err, 'Failed to create database');
		} finally {
			creatingDb = false;
		}
	}

	async function deleteDatabase(id: string, name: string) {
		deleteDbConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/databases/${id}`);
			flash(`Database "${name}" deleted.`);
			await loadDatabases();
		} catch (err) {
			fail(err, 'Failed to delete database');
		}
	}

	// ── User actions ──────────────────────────────────────────────
	async function createUser() {
		if (!newUsername.trim() || !newPassword.trim() || !newUserEngine) return;
		creatingUser = true;
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/databases/users', {
				username: newUsername.trim(),
				password: newPassword.trim(),
				engine: newUserEngine
			});
			flash(`User "${newUsername.trim()}" created successfully.`);
			newUsername = '';
			newPassword = '';
			showNewPassword = false;
			newUserEngine = 'mysql';
			await loadUsers();
		} catch (err) {
			fail(err, 'Failed to create database user');
		} finally {
			creatingUser = false;
		}
	}

	async function resetPassword(userId: string) {
		if (!resetPasswordValue.trim()) return;
		actionMsg = '';
		actionError = '';
		try {
			// Backend registers this route as POST only.
			await api.post(`/api/v1/databases/users/${userId}/password`, {
				password: resetPasswordValue.trim()
			});
			flash('Password reset successfully.');
			resetPasswordUserId = null;
			resetPasswordValue = '';
			showResetPassword = false;
		} catch (err) {
			fail(err, 'Failed to reset password');
		}
	}

	async function grantPrivileges(userId: string) {
		if (!grantDatabase) return;
		actionMsg = '';
		actionError = '';
		try {
			// Handler reads user_id and database_id from the body; the URL param
			// is ignored. database_id is the managed_databases row ID.
			await api.post(`/api/v1/databases/users/${userId}/grant`, {
				user_id: userId,
				database_id: grantDatabase
			});
			const dbName = databases.find((d) => d.id === grantDatabase)?.name || grantDatabase;
			flash(`Privileges on "${dbName}" granted successfully.`);
			grantUserId = null;
			grantDatabase = '';
		} catch (err) {
			fail(err, 'Failed to grant privileges');
		}
	}

	async function deleteUser(id: string, username: string) {
		deleteUserConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/databases/users/${id}`);
			flash(`User "${username}" deleted.`);
			await loadUsers();
		} catch (err) {
			fail(err, 'Failed to delete database user');
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
	});
</script>

<div class="space-y-6">
	<!-- Header -->
	<div class="flex flex-wrap items-end justify-between gap-4">
		<div>
			<h2 class="text-2xl font-bold text-white">Databases</h2>
			<div class="mt-1 flex flex-wrap items-center gap-2 text-sm text-gray-400">
				<span>{databases.length} databases</span>
				<span class="text-gray-600">·</span>
				<span>{dbUsers.length} users</span>
				<span class="text-gray-600">·</span>
				<span>{engines.filter((e) => e.running).length} engines running</span>
			</div>
		</div>
		<button
			type="button"
			onclick={refreshAll}
			disabled={loadingEngines || loadingDatabases || loadingUsers}
			class="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-white/8 bg-white/[0.035]
			px-3.5 py-2 text-xs font-medium text-gray-300 transition
			hover:border-blue-400/20 hover:bg-blue-500/8 hover:text-white disabled:opacity-50"
		>
			<svg
				class="h-3.5 w-3.5 {(loadingEngines || loadingDatabases || loadingUsers) ? 'animate-spin' : ''}"
				fill="none"
				stroke="currentColor"
				viewBox="0 0 24 24"
				stroke-width="2"
				aria-hidden="true"
			>
				<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992" />
			</svg>
			Refresh
		</button>
	</div>

	{#if error}
		<div class="flex items-center justify-between gap-2 rounded-xl border border-red-700 bg-red-900/40 px-3.5 py-2.5 text-sm text-red-300">
			<span>{error}</span>
			<button type="button" onclick={() => (error = '')} class="cursor-pointer text-red-400 hover:text-red-200">✕</button>
		</div>
	{/if}

	{#if actionMsg}
		<div class="flex items-center justify-between gap-2 rounded-xl border border-green-700 bg-green-900/40 px-3.5 py-2.5 text-sm text-green-300">
			<span>{actionMsg}</span>
			<button type="button" onclick={() => (actionMsg = '')} class="cursor-pointer text-green-400 hover:text-green-200">✕</button>
		</div>
	{/if}

	{#if actionError}
		<div class="flex items-center justify-between gap-2 rounded-xl border border-red-700 bg-red-900/40 px-3.5 py-2.5 text-sm text-red-300">
			<span>{actionError}</span>
			<button type="button" onclick={() => (actionError = '')} class="cursor-pointer text-red-400 hover:text-red-200">✕</button>
		</div>
	{/if}

	<!-- ═══════════════════════════ ENGINES ═══════════════════════════ -->
	<section aria-label="Database engines">
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
									<p class="text-xs text-gray-500">{eng.version || (eng.installed ? 'Installed' : 'Not installed')}</p>
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
										{eng.running ? 'Running' : 'Stopped'}
									</span>
								{:else}
									<span class="rounded-full bg-gray-700/60 px-2 py-0.5 text-[11px] font-semibold text-gray-400">Not installed</span>
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
									{actionInProgress === `install-${eng.name}` ? 'Installing…' : 'Install'}
								</button>
							{:else}
								{#if !eng.running}
									<button
										type="button"
										onclick={() => engineAction(eng.name, 'start')}
										disabled={engineOperationInProgress}
										class="cursor-pointer rounded-lg bg-green-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-green-700 disabled:opacity-50"
									>
										{actionInProgress === `start-${eng.name}` ? 'Starting…' : 'Start'}
									</button>
								{:else}
									<button
										type="button"
										onclick={() => engineAction(eng.name, 'stop')}
										disabled={engineOperationInProgress}
										class="cursor-pointer rounded-lg bg-red-600 px-3.5 py-1.5 text-xs font-semibold text-white transition hover:bg-red-700 disabled:opacity-50"
									>
										{actionInProgress === `stop-${eng.name}` ? 'Stopping…' : 'Stop'}
									</button>
								{/if}
								<button
									type="button"
									onclick={() => engineAction(eng.name, 'restart')}
									disabled={engineOperationInProgress}
									class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-3.5 py-1.5 text-xs font-medium text-gray-200 transition hover:bg-gray-600 disabled:opacity-50"
								>
									{actionInProgress === `restart-${eng.name}` ? 'Restarting…' : 'Restart'}
								</button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<!-- ═══════════════════════════ DATABASES ═══════════════════════════ -->
	<section class="rounded-2xl border border-white/5 bg-gray-800/60" aria-label="Databases">
		<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-4">
			<div class="flex items-center gap-2.5">
				<h3 class="text-sm font-semibold text-white">Databases</h3>
				{#if !loadingDatabases}
					<span class="rounded-full bg-blue-900/40 px-2 py-0.5 text-[11px] font-semibold text-blue-300">{databases.length}</span>
				{/if}
			</div>
			<div class="relative w-full sm:w-56">
				<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
				</svg>
				<input
					type="text"
					bind:value={dbSearch}
					placeholder="Search databases…"
					aria-label="Search databases"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				/>
			</div>
		</div>

		<!-- Create database form -->
		<form
			class="flex flex-wrap items-end gap-3 border-b border-white/5 bg-gray-900/40 px-5 py-4"
			onsubmit={(e) => { e.preventDefault(); createDatabase(); }}
		>
			<div>
				<label for="db-name" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Name</label>
				<input
					id="db-name"
					type="text"
					bind:value={newDbName}
					placeholder="my_database"
					class="w-44 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				/>
			</div>
			<div>
				<label for="db-engine" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Engine</label>
				<select
					id="db-engine"
					bind:value={newDbEngine}
					class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				>
					<option value="mysql">MySQL</option>
					<option value="postgresql">PostgreSQL</option>
				</select>
			</div>
			<div>
				<label for="db-charset" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Charset</label>
				<input
					id="db-charset"
					type="text"
					bind:value={newDbCharset}
					placeholder="utf8mb4"
					class="w-28 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				/>
			</div>
			<button
				type="submit"
				disabled={creatingDb || !newDbName.trim()}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
			>
				{creatingDb ? 'Adding…' : 'Create Database'}
			</button>
		</form>

		<!-- Databases table -->
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
			<div class="px-5 py-10 text-center">
				<p class="text-sm text-gray-400">{dbSearch ? `No databases match "${dbSearch}".` : 'No databases yet.'}</p>
				{#if !dbSearch}
					<p class="mt-1 text-xs text-gray-500">Create one with the form above.</p>
				{/if}
			</div>
		{:else}
			<div class="divide-y divide-gray-700/40">
				{#each filteredDatabases as db (db.id)}
					<div class="group flex items-center gap-3 px-5 py-3 transition hover:bg-gray-750">
						<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-900/70 text-[10px] font-bold {engineIconBg(db.engine)}">
							{engineInitial(db.engine)}
						</span>
						<div class="min-w-0 flex-1">
							<button
								type="button"
								onclick={() => copyText(db.name, `db-${db.id}`)}
								title="Click to copy name"
								class="block cursor-pointer truncate text-left font-mono text-sm font-medium text-gray-100 hover:text-blue-300"
							>
								{db.name}
							</button>
							<p class="text-[11px] text-gray-500">
								{db.charset || '—'} · created {formatDate(db.created_at)}
							</p>
						</div>
						<span class="hidden shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold sm:inline {engineBadgeClass(db.engine)}">
							{engineLabel(db.engine)}
						</span>
						<div class="flex w-20 shrink-0 items-center justify-end gap-1">
							{#if deleteDbConfirmId === db.id}
								<span class="mr-1 text-[11px] text-red-400">Delete?</span>
								<button
									type="button"
									onclick={() => deleteDatabase(db.id, db.name)}
									class="cursor-pointer rounded-lg bg-red-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-red-700"
								>
									Yes
								</button>
								<button
									type="button"
									onclick={() => (deleteDbConfirmId = null)}
									class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600"
								>
									No
								</button>
							{:else}
								<button
									type="button"
									onclick={() => copyText(db.name, `db-${db.id}`)}
									title="Copy name"
									class="cursor-pointer rounded-lg p-1.5 text-gray-400 opacity-0 transition hover:bg-gray-600 hover:text-white group-hover:opacity-100"
								>
									{#if copiedField === `db-${db.id}`}
										<svg class="h-4 w-4 text-green-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2.5" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
										</svg>
									{:else}
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 011.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 00-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 01-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 00-3.375-3.375h-1.5a1.125 1.125 0 01-1.125-1.125v-1.5a3.375 3.375 0 00-3.375-3.375H9.75" />
										</svg>
									{/if}
								</button>
								<button
									type="button"
									onclick={() => (deleteDbConfirmId = db.id)}
									title="Delete database"
									class="cursor-pointer rounded-lg p-1.5 text-gray-400 opacity-0 transition hover:bg-red-600 hover:text-white group-hover:opacity-100"
								>
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
									</svg>
								</button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<!-- ═══════════════════════════ USERS ═══════════════════════════ -->
	<section class="rounded-2xl border border-white/5 bg-gray-800/60" aria-label="Database users">
		<div class="flex flex-wrap items-center justify-between gap-3 border-b border-white/5 px-5 py-4">
			<div class="flex items-center gap-2.5">
				<h3 class="text-sm font-semibold text-white">Database Users</h3>
				{#if !loadingUsers}
					<span class="rounded-full bg-blue-900/40 px-2 py-0.5 text-[11px] font-semibold text-blue-300">{dbUsers.length}</span>
				{/if}
			</div>
			<div class="relative w-full sm:w-56">
				<svg class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35M17 10.5a6.5 6.5 0 11-13 0 6.5 6.5 0 0113 0z" />
				</svg>
				<input
					type="text"
					bind:value={userSearch}
					placeholder="Search users…"
					aria-label="Search users"
					class="w-full rounded-lg border border-gray-600 bg-gray-900 py-1.5 pl-8 pr-3 text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
				/>
			</div>
		</div>

		<!-- Create user form -->
		<form
			class="border-b border-white/5 bg-gray-900/40 px-5 py-4"
			onsubmit={(e) => { e.preventDefault(); createUser(); }}
		>
			<div class="flex flex-wrap items-end gap-3">
				<div>
					<label for="user-name" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Username</label>
					<input
						id="user-name"
						type="text"
						bind:value={newUsername}
						placeholder="db_user"
						autocomplete="off"
						class="w-40 rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					/>
				</div>
				<div>
					<label for="user-engine" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Engine</label>
					<select
						id="user-engine"
						bind:value={newUserEngine}
						class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
					>
						<option value="mysql">MySQL</option>
						<option value="postgresql">PostgreSQL</option>
					</select>
				</div>
				<div class="min-w-56 flex-1">
					<label for="user-pass" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Password</label>
					<div class="flex items-center gap-2">
						<div class="relative min-w-0 flex-1">
							<input
								id="user-pass"
								type={showNewPassword ? 'text' : 'password'}
								bind:value={newPassword}
								placeholder="Password or click ⚄ to generate"
								autocomplete="new-password"
								class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 pr-9 font-mono text-sm text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
							/>
							<button
								type="button"
								onclick={() => (showNewPassword = !showNewPassword)}
								title={showNewPassword ? 'Hide password' : 'Show password'}
								class="absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer text-gray-500 transition hover:text-gray-200"
							>
								{#if showNewPassword}
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
									</svg>
								{:else}
									<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
										<path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.964-7.178z" />
										<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
									</svg>
								{/if}
							</button>
						</div>
						<button
							type="button"
							onclick={() => fillGenerated('new')}
							title="Generate strong password"
							class="shrink-0 cursor-pointer rounded-lg border border-blue-400/30 bg-blue-500/10 px-3 py-2 text-sm font-semibold text-blue-300 transition hover:bg-blue-500/20"
						>
							⚄ Generate
						</button>
					</div>
				</div>
				<button
					type="submit"
					disabled={creatingUser || !newUsername.trim() || !newPassword.trim()}
					class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40"
				>
					{creatingUser ? 'Creating…' : 'Create User'}
				</button>
			</div>
			<!-- Generator options -->
			<details class="mt-3 text-xs text-gray-400">
				<summary class="inline-flex cursor-pointer select-none items-center gap-1.5 hover:text-gray-200">
					<svg class="h-3 w-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M10.343 3.94c.09-.542.56-.94 1.11-.94h1.093c.55 0 1.02.398 1.11.94l.149.894c.07.424.384.764.78.93.398.164.855.142 1.205-.108l.737-.527a1.125 1.125 0 011.45.12l.773.774c.39.389.44 1.002.12 1.45l-.527.737c-.25.35-.272.806-.107 1.204.165.397.505.71.93.78l.893.15c.543.09.94.56.94 1.109v1.094c0 .55-.397 1.02-.94 1.11l-.893.149c-.425.07-.765.383-.93.78-.165.398-.143.854.107 1.204l.527.738c.32.447.269 1.06-.12 1.45l-.774.773a1.125 1.125 0 01-1.449.12l-.738-.527c-.35-.25-.806-.272-1.203-.107-.397.165-.71.505-.781.929l-.149.894c-.09.542-.56.94-1.11.94h-1.094c-.55 0-1.019-.398-1.11-.94l-.148-.894c-.071-.424-.384-.764-.781-.93-.398-.164-.854-.142-1.204.108l-.738.527c-.447.32-1.06.269-1.45-.12l-.773-.774a1.125 1.125 0 01-.12-1.45l.527-.737c.25-.35.273-.806.108-1.204-.165-.397-.506-.71-.93-.78l-.894-.15c-.542-.09-.94-.56-.94-1.109v-1.094c0-.55.398-1.02.94-1.11l.894-.149c.424-.07.765-.383.93-.78.165-.398.143-.854-.108-1.204l-.526-.738a1.125 1.125 0 01.12-1.45l.773-.773a1.125 1.125 0 011.45-.12l.737.527c.35.25.807.272 1.204.107.397-.165.71-.505.78-.929l.15-.894z" />
						<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
					</svg>
					Generator options — length {genLength}{genSymbols ? ', with symbols' : ', letters & digits only'}
				</summary>
				<div class="mt-2 flex flex-wrap items-center gap-4 rounded-lg border border-gray-700 bg-gray-900/60 px-3.5 py-2.5">
					<label class="flex items-center gap-2.5">
						<span>Length</span>
						<input type="range" min="12" max="40" bind:value={genLength} class="w-36 accent-blue-500" />
						<span class="w-6 text-center font-mono font-semibold text-gray-200">{genLength}</span>
					</label>
					<label class="flex cursor-pointer items-center gap-2">
						<input type="checkbox" bind:checked={genSymbols} class="accent-blue-500" />
						<span>Include symbols</span>
					</label>
					<span class="font-mono text-gray-500">preview: {generatedPreview}</span>
				</div>
			</details>
		</form>

		<!-- Users table -->
		{#if loadingUsers}
			<div class="divide-y divide-gray-700/40">
				{#each Array(3) as _}
					<div class="flex items-center gap-3 px-5 py-3">
						<div class="h-9 w-9 animate-pulse rounded-lg bg-gray-700/50"></div>
						<div class="h-4 w-36 animate-pulse rounded bg-gray-700/50"></div>
						<div class="ml-auto h-4 w-40 animate-pulse rounded bg-gray-700/50"></div>
					</div>
				{/each}
			</div>
		{:else if filteredUsers.length === 0}
			<div class="px-5 py-10 text-center">
				<p class="text-sm text-gray-400">{userSearch ? `No users match "${userSearch}".` : 'No database users yet.'}</p>
				{#if !userSearch}
					<p class="mt-1 text-xs text-gray-500">Create one with the form above.</p>
				{/if}
			</div>
		{:else}
			<div class="divide-y divide-gray-700/40">
				{#each filteredUsers as u (u.id)}
					<div class="group px-5 py-3 transition hover:bg-gray-750">
						<div class="flex items-center gap-3">
							<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-gray-900/70 {engineIconBg(u.engine)}" aria-hidden="true">
								<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
									<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z" />
								</svg>
							</span>
							<div class="min-w-0 flex-1">
								<button
									type="button"
									onclick={() => copyText(u.username, `user-${u.id}`)}
									title="Click to copy username"
									class="block cursor-pointer truncate text-left font-mono text-sm font-medium text-gray-100 hover:text-blue-300"
								>
									{u.username}
								</button>
								<p class="text-[11px] text-gray-500">
									{copiedField === `user-${u.id}` ? 'copied!' : 'click name to copy'}
								</p>
							</div>
							<span class="hidden shrink-0 rounded-md px-2 py-0.5 text-[11px] font-semibold sm:inline {engineBadgeClass(u.engine)}">
								{engineLabel(u.engine)}
							</span>
							<div class="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
								{#if deleteUserConfirmId === u.id}
									<span class="mr-1 text-[11px] text-red-400">Delete user?</span>
									<button
										type="button"
										onclick={() => deleteUser(u.id, u.username)}
										class="cursor-pointer rounded-lg bg-red-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-red-700"
									>
										Yes
									</button>
									<button
										type="button"
										onclick={() => (deleteUserConfirmId = null)}
										class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1 text-[11px] text-gray-200 transition hover:bg-gray-600"
									>
										No
									</button>
								{:else}
									<button
										type="button"
										onclick={() => manageUser(u.id)}
										class="cursor-pointer rounded-lg bg-blue-600 px-2.5 py-1 text-[11px] font-semibold text-white transition hover:bg-blue-700"
									>
										Manage
									</button>
									<button
										type="button"
										onclick={() => {
											resetPasswordUserId = resetPasswordUserId === u.id ? null : u.id;
											resetPasswordValue = '';
											showResetPassword = false;
											grantUserId = null;
										}}
										class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2.5 py-1 text-[11px] font-medium text-gray-200 transition hover:border-yellow-400/40 hover:bg-yellow-600/20 hover:text-yellow-200"
									>
										Reset Password
									</button>
									<button
										type="button"
										onclick={() => {
											grantUserId = grantUserId === u.id ? null : u.id;
											grantDatabase = '';
											resetPasswordUserId = null;
										}}
										class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-2.5 py-1 text-[11px] font-medium text-gray-200 transition hover:border-indigo-400/40 hover:bg-indigo-600/20 hover:text-indigo-200"
									>
										Grant
									</button>
									<button
										type="button"
										onclick={() => (deleteUserConfirmId = u.id)}
										title="Delete user"
										class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-red-600 hover:text-white"
									>
										<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
											<path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
										</svg>
									</button>
								{/if}
							</div>
						</div>

						<!-- Reset password inline form with generator -->
						{#if resetPasswordUserId === u.id}
							<div class="mt-3 rounded-xl border border-yellow-700/40 bg-yellow-900/10 px-4 py-3">
								<p class="mb-2 text-xs font-semibold text-yellow-300">
									Reset password for <span class="font-mono">{u.username}</span>
								</p>
								<div class="flex flex-wrap items-center gap-2">
									<div class="relative min-w-52 flex-1">
										<input
											type={showResetPassword ? 'text' : 'password'}
											bind:value={resetPasswordValue}
											placeholder="New password or click ⚄"
											autocomplete="new-password"
											class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-1.5 pr-9 font-mono text-xs text-gray-200 placeholder:text-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
										/>
										<button
											type="button"
											onclick={() => (showResetPassword = !showResetPassword)}
											title={showResetPassword ? 'Hide password' : 'Show password'}
											class="absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer text-gray-500 transition hover:text-gray-200"
										>
											{#if showResetPassword}
												<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
													<path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
												</svg>
											{:else}
												<svg class="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
													<path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.964-7.178z" />
													<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
												</svg>
											{/if}
										</button>
									</div>
									<button
										type="button"
										onclick={() => fillGenerated('reset')}
										title="Generate strong password"
										class="shrink-0 cursor-pointer rounded-lg border border-blue-400/30 bg-blue-500/10 px-3 py-1.5 text-xs font-semibold text-blue-300 transition hover:bg-blue-500/20"
									>
										⚄ Generate
									</button>
									{#if resetPasswordValue}
										<button
											type="button"
											onclick={() => copyText(resetPasswordValue, `reset-${u.id}`)}
											class="shrink-0 cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1.5 text-[11px] font-medium text-gray-200 transition hover:bg-gray-600"
										>
											{copiedField === `reset-${u.id}` ? 'Copied!' : 'Copy'}
										</button>
									{/if}
									<button
										type="button"
										onclick={() => resetPassword(u.id)}
										disabled={!resetPasswordValue.trim()}
										class="shrink-0 cursor-pointer rounded-lg bg-green-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-green-700 disabled:cursor-not-allowed disabled:opacity-40"
									>
										Save
									</button>
									<button
										type="button"
										onclick={() => { resetPasswordUserId = null; resetPasswordValue = ''; showResetPassword = false; }}
										class="shrink-0 cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
									>
										Cancel
									</button>
								</div>
								<p class="mt-2 text-[11px] text-gray-500">
									Uses the generator options from the form above (length {genLength}{genSymbols ? ', with symbols' : ''}).
								</p>
							</div>
						{/if}

						<!-- Grant privileges inline form -->
						{#if grantUserId === u.id}
							<div class="mt-3 rounded-xl border border-indigo-700/40 bg-indigo-900/10 px-4 py-3">
								<p class="mb-2 text-xs font-semibold text-indigo-300">
									Grant privileges to <span class="font-mono">{u.username}</span>
								</p>
								<div class="flex flex-wrap items-center gap-2">
									<select
										bind:value={grantDatabase}
										class="rounded-lg border border-gray-600 bg-gray-900 px-3 py-1.5 text-xs text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
									>
										<option value="">Select database…</option>
										{#each databases.filter((d) => d.engine === u.engine) as db (db.id)}
											<option value={db.id}>{db.name}</option>
										{/each}
									</select>
									<button
										type="button"
										onclick={() => grantPrivileges(u.id)}
										disabled={!grantDatabase}
										class="cursor-pointer rounded-lg bg-green-600 px-3 py-1.5 text-xs font-semibold text-white transition hover:bg-green-700 disabled:cursor-not-allowed disabled:opacity-40"
									>
										Grant
									</button>
									<button
										type="button"
										onclick={() => { grantUserId = null; grantDatabase = ''; }}
										class="cursor-pointer rounded-lg bg-gray-700 px-2.5 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
									>
										Cancel
									</button>
								</div>
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</section>

	<TaskProgress bind:taskId={currentTaskId} storageKey="jenderal_db_task" onComplete={() => { currentTaskId = ''; loadEngines(); }} />
</div>
