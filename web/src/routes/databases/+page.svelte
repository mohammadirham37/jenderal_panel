<script lang="ts">
	import { onMount } from 'svelte';
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

	// Delete user confirm
	let deleteUserConfirmId = $state<string | null>(null);

	// Reset password state
	let resetPasswordUserId = $state<string | null>(null);
	let resetPasswordValue = $state('');

	// Grant privileges state
	let grantUserId = $state<string | null>(null);
	let grantDatabase = $state('');

	// ── Helpers ────────────────────────────────────────────────────
	function engineBadgeClass(engine: string): string {
		switch (engine) {
			case 'mysql':
				return 'bg-blue-900/50 text-blue-400';
			case 'postgresql':
				return 'bg-indigo-900/50 text-indigo-400';
			case 'redis':
				return 'bg-red-900/50 text-red-400';
			default:
				return 'bg-gray-700 text-gray-400';
		}
	}

	function engineLabel(engine: string): string {
		switch (engine) {
			case 'mysql':
				return 'MySQL';
			case 'postgresql':
				return 'PostgreSQL';
			case 'redis':
				return 'Redis';
			default:
				return engine;
		}
	}

	function formatDate(dateStr: string): string {
		if (!dateStr) return '-';
		try {
			return new Date(dateStr).toLocaleDateString('en-US', {
				year: 'numeric',
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit'
			});
		} catch {
			return dateStr;
		}
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

	// ── Engine actions ────────────────────────────────────────────
	async function installEngine(engineName: string) {
		actionMsg = '';
		actionError = '';
		actionInProgress = `install-${engineName}`;
		try {
			const result = await api.post<{ task_id: string }>(`/api/v1/databases/engines/${engineName}/install`);
			currentTaskId = result.task_id;
			actionMsg = `${engineLabel(engineName)} installation started.`;
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to install ${engineLabel(engineName)}`;
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
			actionMsg = `${engineLabel(engineName)} ${action}ed successfully.`;
			await loadEngines();
		} catch (err) {
			actionError = err instanceof Error ? err.message : `Failed to ${action} ${engineLabel(engineName)}`;
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
			actionMsg = `Database "${newDbName.trim()}" created successfully.`;
			newDbName = '';
			newDbEngine = 'mysql';
			newDbCharset = 'utf8mb4';
			await loadDatabases();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create database';
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
			actionMsg = `Database "${name}" deleted.`;
			await loadDatabases();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete database';
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
			actionMsg = `User "${newUsername.trim()}" created successfully.`;
			newUsername = '';
			newPassword = '';
			newUserEngine = 'mysql';
			await loadUsers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create database user';
		} finally {
			creatingUser = false;
		}
	}

	async function resetPassword(userId: string) {
		if (!resetPasswordValue.trim()) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/databases/users/${userId}/password`, {
				password: resetPasswordValue.trim()
			});
			actionMsg = 'Password reset successfully.';
			resetPasswordUserId = null;
			resetPasswordValue = '';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to reset password';
		}
	}

	async function grantPrivileges(userId: string) {
		if (!grantDatabase) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.post(`/api/v1/databases/users/${userId}/grant`, {
				database: grantDatabase
			});
			actionMsg = 'Privileges granted successfully.';
			grantUserId = null;
			grantDatabase = '';
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to grant privileges';
		}
	}

	async function deleteUser(id: string, username: string) {
		deleteUserConfirmId = null;
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/databases/users/${id}`);
			actionMsg = `User "${username}" deleted.`;
			await loadUsers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete database user';
		}
	}

	// ── Lifecycle ─────────────────────────────────────────────────
	onMount(() => {
		loadEngines();
		loadDatabases();
		loadUsers();
	});
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Databases</h2>

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

	<!-- ═══════════════════════════ ENGINES ═══════════════════════════ -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">Database Engines</h3>
		{#if loadingEngines}
			<div class="text-gray-400 text-sm">Loading engines...</div>
		{:else if engines.length === 0}
			<div class="text-gray-400 text-sm">No database engine information available.</div>
		{:else}
			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
				{#each engines as eng}
					<div class="bg-gray-900 rounded-lg border border-gray-700 p-4 flex flex-col gap-3">
						<div class="flex items-center justify-between">
							<span class="text-white font-semibold text-base">{engineLabel(eng.name)}</span>
							<div class="flex items-center gap-2">
								{#if eng.installed}
									<span class="text-xs bg-green-900/50 text-green-400 px-2 py-0.5 rounded font-medium">Installed</span>
								{:else}
									<span class="text-xs bg-gray-700 text-gray-400 px-2 py-0.5 rounded font-medium">Not Installed</span>
								{/if}
								{#if eng.installed}
									<span class="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded font-medium {eng.running ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
										<span class="w-1.5 h-1.5 rounded-full {eng.running ? 'bg-green-400' : 'bg-red-400'}"></span>
										{eng.running ? 'Running' : 'Stopped'}
									</span>
								{/if}
							</div>
						</div>

						{#if eng.version}
							<div class="text-xs text-gray-400">Version: <span class="text-gray-300">{eng.version}</span></div>
						{/if}

						<div class="flex items-center gap-2 mt-auto">
							{#if !eng.installed}
								<button
									onclick={() => installEngine(eng.name)}
									disabled={actionInProgress !== null}
									class="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
								>
									{actionInProgress === `install-${eng.name}` ? 'Installing...' : 'Install'}
								</button>
							{:else}
								{#if !eng.running}
									<button
										onclick={() => engineAction(eng.name, 'start')}
										disabled={actionInProgress !== null}
										class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{actionInProgress === `start-${eng.name}` ? '...' : 'Start'}
									</button>
								{:else}
									<button
										onclick={() => engineAction(eng.name, 'stop')}
										disabled={actionInProgress !== null}
										class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
									>
										{actionInProgress === `stop-${eng.name}` ? '...' : 'Stop'}
									</button>
								{/if}
								<button
									onclick={() => engineAction(eng.name, 'restart')}
									disabled={actionInProgress !== null}
									class="px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
								>
									{actionInProgress === `restart-${eng.name}` ? '...' : 'Restart'}
								</button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	<!-- ═══════════════════════════ DATABASES ═══════════════════════════ -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">Databases</h3>

		<!-- Create database form -->
		<div class="flex flex-wrap items-end gap-3 mb-4">
			<div>
				<label for="db-name" class="block text-sm text-gray-400 mb-1">Name</label>
				<input
					id="db-name"
					type="text"
					bind:value={newDbName}
					placeholder="my_database"
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-48"
				/>
			</div>
			<div>
				<label for="db-engine" class="block text-sm text-gray-400 mb-1">Engine</label>
				<select
					id="db-engine"
					bind:value={newDbEngine}
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
				>
					<option value="mysql">MySQL</option>
					<option value="postgresql">PostgreSQL</option>
				</select>
			</div>
			<div>
				<label for="db-charset" class="block text-sm text-gray-400 mb-1">Charset</label>
				<input
					id="db-charset"
					type="text"
					bind:value={newDbCharset}
					placeholder="utf8mb4"
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-32"
				/>
			</div>
			<button
				onclick={createDatabase}
				disabled={creatingDb || !newDbName.trim()}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
			>
				{creatingDb ? 'Adding...' : 'Add'}
			</button>
		</div>

		<!-- Databases table -->
		{#if loadingDatabases}
			<div class="text-gray-400 text-sm">Loading databases...</div>
		{:else if databases.length === 0}
			<div class="text-gray-500 text-sm py-4 text-center">No databases found.</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Name</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Engine</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Charset</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Created</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each databases as db}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white font-medium font-mono">{db.name}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {engineBadgeClass(db.engine)}">
										{engineLabel(db.engine)}
									</span>
								</td>
								<td class="px-4 py-3 text-sm text-gray-300">{db.charset || '-'}</td>
								<td class="px-4 py-3 text-sm text-gray-400">{formatDate(db.created_at)}</td>
								<td class="px-4 py-3 text-right">
									{#if deleteDbConfirmId === db.id}
										<span class="text-xs text-red-400 mr-1">Delete?</span>
										<button
											onclick={() => deleteDatabase(db.id, db.name)}
											class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
										>
											Yes
										</button>
										<button
											onclick={() => (deleteDbConfirmId = null)}
											class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer ml-1"
										>
											Cancel
										</button>
									{:else}
										<button
											onclick={() => (deleteDbConfirmId = db.id)}
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

	<!-- ═══════════════════════════ USERS ═══════════════════════════ -->
	<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
		<h3 class="text-lg font-semibold text-white mb-4">Database Users</h3>

		<!-- Create user form -->
		<div class="flex flex-wrap items-end gap-3 mb-4">
			<div>
				<label for="user-name" class="block text-sm text-gray-400 mb-1">Username</label>
				<input
					id="user-name"
					type="text"
					bind:value={newUsername}
					placeholder="db_user"
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-44"
				/>
			</div>
			<div>
				<label for="user-pass" class="block text-sm text-gray-400 mb-1">Password</label>
				<input
					id="user-pass"
					type="password"
					bind:value={newPassword}
					placeholder="password"
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-44"
				/>
			</div>
			<div>
				<label for="user-engine" class="block text-sm text-gray-400 mb-1">Engine</label>
				<select
					id="user-engine"
					bind:value={newUserEngine}
					class="px-3 py-2 bg-gray-900 border border-gray-600 rounded text-gray-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
				>
					<option value="mysql">MySQL</option>
					<option value="postgresql">PostgreSQL</option>
				</select>
			</div>
			<button
				onclick={createUser}
				disabled={creatingUser || !newUsername.trim() || !newPassword.trim()}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
			>
				{creatingUser ? 'Creating...' : 'Add User'}
			</button>
		</div>

		<!-- Users table -->
		{#if loadingUsers}
			<div class="text-gray-400 text-sm">Loading users...</div>
		{:else if dbUsers.length === 0}
			<div class="text-gray-500 text-sm py-4 text-center">No database users found.</div>
		{:else}
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Username</th>
							<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Engine</th>
							<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each dbUsers as u}
							<tr class="hover:bg-gray-750">
								<td class="px-4 py-3 text-sm text-white font-medium font-mono">{u.username}</td>
								<td class="px-4 py-3">
									<span class="inline-block px-2 py-0.5 rounded text-xs font-medium {engineBadgeClass(u.engine)}">
										{engineLabel(u.engine)}
									</span>
								</td>
								<td class="px-4 py-3 text-right">
									<div class="flex flex-col items-end gap-2">
										<!-- Action buttons row -->
										<div class="flex items-center gap-2">
											{#if deleteUserConfirmId === u.id}
												<span class="text-xs text-red-400">Delete?</span>
												<button
													onclick={() => deleteUser(u.id, u.username)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Yes
												</button>
												<button
													onclick={() => (deleteUserConfirmId = null)}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Cancel
												</button>
											{:else}
												<button
													onclick={() => {
														resetPasswordUserId = resetPasswordUserId === u.id ? null : u.id;
														resetPasswordValue = '';
														grantUserId = null;
													}}
													class="px-2.5 py-1 bg-yellow-600 hover:bg-yellow-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Reset Password
												</button>
												<button
													onclick={() => {
														grantUserId = grantUserId === u.id ? null : u.id;
														grantDatabase = '';
														resetPasswordUserId = null;
													}}
													class="px-2.5 py-1 bg-indigo-600 hover:bg-indigo-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Grant
												</button>
												<button
													onclick={() => (deleteUserConfirmId = u.id)}
													class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Delete
												</button>
											{/if}
										</div>

										<!-- Reset password inline form -->
										{#if resetPasswordUserId === u.id}
											<div class="flex items-center gap-2">
												<input
													type="password"
													bind:value={resetPasswordValue}
													placeholder="New password"
													class="px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-xs focus:outline-none focus:ring-2 focus:ring-blue-500 w-40"
												/>
												<button
													onclick={() => resetPassword(u.id)}
													disabled={!resetPasswordValue.trim()}
													class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Save
												</button>
												<button
													onclick={() => { resetPasswordUserId = null; resetPasswordValue = ''; }}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Cancel
												</button>
											</div>
										{/if}

										<!-- Grant privileges inline form -->
										{#if grantUserId === u.id}
											<div class="flex items-center gap-2">
												<select
													bind:value={grantDatabase}
													class="px-2 py-1 bg-gray-900 border border-gray-600 rounded text-gray-200 text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
												>
													<option value="">Select database...</option>
													{#each databases.filter(d => d.engine === u.engine) as db}
														<option value={db.name}>{db.name}</option>
													{/each}
												</select>
												<button
													onclick={() => grantPrivileges(u.id)}
													disabled={!grantDatabase}
													class="px-2.5 py-1 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Grant
												</button>
												<button
													onclick={() => { grantUserId = null; grantDatabase = ''; }}
													class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer"
												>
													Cancel
												</button>
											</div>
										{/if}
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</div>

	<TaskProgress taskId={currentTaskId} onComplete={() => { currentTaskId = ''; loadEngines(); }} />
</div>
