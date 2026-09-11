<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { User, SSHKey } from '$lib/types';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Toolbar search
	let search = $state('');
	let filteredUsers = $derived.by(() => {
		const query = search.trim().toLowerCase();
		if (!query) return users;
		return users.filter(
			(u) => u.username.toLowerCase().includes(query) || u.email.toLowerCase().includes(query)
		);
	});

	// Create form
	let showCreate = $state(false);
	let newUsername = $state('');
	let newEmail = $state('');
	let newPassword = $state('');
	let newRole = $state('user');
	let newSSHEnabled = $state(true);
	let creating = $state(false);

	// Edit form (modal)
	let editingUser = $state<User | null>(null);
	let editUsername = $state('');
	let editEmail = $state('');
	let editIsActive = $state(true);
	let editSSHEnabled = $state(false);
	let saving = $state(false);

	// SSH keys panel (modal)
	let keysUser = $state<User | null>(null);
	let keys = $state<SSHKey[]>([]);
	let keysLoading = $state(false);
	let keysError = $state('');
	let newKeyName = $state('');
	let newKeyMaterial = $state('');
	let addingKey = $state(false);

	function initials(name: string): string {
		return name.slice(0, 2).toUpperCase();
	}

	async function loadUsers() {
		try {
			users = (await api.get<User[]>('/api/v1/users')) || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load users';
		} finally {
			loading = false;
		}
	}

	async function createUser() {
		actionMsg = '';
		actionError = '';
		creating = true;

		try {
			await api.post('/api/v1/users', {
				username: newUsername,
				email: newEmail,
				password: newPassword,
				role: newRole,
				ssh_enabled: newSSHEnabled
			});
			actionMsg = `User "${newUsername}" created successfully.`;
			showCreate = false;
			newUsername = '';
			newEmail = '';
			newPassword = '';
			newRole = 'user';
			newSSHEnabled = true;
			await loadUsers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create user';
		} finally {
			creating = false;
		}
	}

	function startEdit(u: User) {
		editingUser = u;
		editUsername = u.username;
		editEmail = u.email;
		editIsActive = u.is_active;
		editSSHEnabled = u.ssh_enabled;
	}

	async function saveEdit() {
		if (!editingUser) return;
		actionMsg = '';
		actionError = '';
		saving = true;

		try {
			await api.put(`/api/v1/users/${editingUser.id}`, {
				username: editUsername,
				email: editEmail,
				is_active: editIsActive,
				ssh_enabled: editSSHEnabled
			});
			actionMsg = `User "${editUsername}" updated successfully.`;
			editingUser = null;
			await loadUsers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update user';
		} finally {
			saving = false;
		}
	}

	async function deleteUser(u: User) {
		const message = u.ssh_enabled
			? `Delete user "${u.username}" AND its Linux SSH account? The home directory /home/${u.username} including all its files will be removed. This cannot be undone.`
			: `Are you sure you want to delete user "${u.username}"?`;
		if (!confirm(message)) return;

		actionMsg = '';
		actionError = '';

		try {
			await api.del(`/api/v1/users/${u.id}`);
			actionMsg = `User "${u.username}" deleted.`;
			await loadUsers();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete user';
		}
	}

	// ─── SSH keys ────────────────────────────────────────────────────

	async function openKeys(u: User) {
		keysUser = u;
		keysError = '';
		newKeyName = '';
		newKeyMaterial = '';
		await loadKeys(u);
	}

	async function loadKeys(u: User) {
		keysLoading = true;
		try {
			keys = (await api.get<SSHKey[]>(`/api/v1/users/${u.id}/ssh-keys`)) || [];
		} catch (err) {
			keysError = err instanceof Error ? err.message : 'Failed to load SSH keys';
		} finally {
			keysLoading = false;
		}
	}

	async function addKey() {
		if (!keysUser || !newKeyMaterial.trim() || addingKey) return;
		addingKey = true;
		keysError = '';
		try {
			await api.post(`/api/v1/users/${keysUser.id}/ssh-keys`, {
				name: newKeyName,
				public_key: newKeyMaterial
			});
			newKeyName = '';
			newKeyMaterial = '';
			await loadKeys(keysUser);
		} catch (err) {
			keysError = err instanceof Error ? err.message : 'Failed to add SSH key';
		} finally {
			addingKey = false;
		}
	}

	async function deleteKey(key: SSHKey) {
		if (!keysUser) return;
		if (!confirm(`Remove key "${key.name || key.fingerprint}"? SSH access with this key stops immediately.`)) return;
		keysError = '';
		try {
			await api.del(`/api/v1/users/${keysUser.id}/ssh-keys/${key.id}`);
			await loadKeys(keysUser);
		} catch (err) {
			keysError = err instanceof Error ? err.message : 'Failed to delete SSH key';
		}
	}

	onMount(loadUsers);
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape') {
			editingUser = null;
			keysUser = null;
		}
	}}
/>

<div class="space-y-5">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 class="text-2xl font-bold text-white">Users</h2>
			<p class="mt-0.5 text-sm text-gray-400">
				{users.length === 0
					? 'Panel accounts and their Linux SSH access.'
					: `${users.length} account${users.length === 1 ? '' : 's'} · ${users.filter((u) => u.ssh_enabled).length} with SSH access`}
			</p>
		</div>
		<button
			onclick={() => (showCreate = !showCreate)}
			class="inline-flex cursor-pointer items-center gap-1.5 rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
		>
			<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
			</svg>
			{showCreate ? 'Cancel' : 'Create User'}
		</button>
	</div>

	{#if actionMsg}
		<div class="flex items-start justify-between gap-3 rounded-lg border border-green-700 bg-green-900/30 px-4 py-2.5 text-sm text-green-300">
			<span>{actionMsg}</span>
			<button onclick={() => (actionMsg = '')} class="cursor-pointer font-medium hover:underline" aria-label="Dismiss">✕</button>
		</div>
	{/if}

	{#if actionError}
		<div class="flex items-start justify-between gap-3 rounded-lg border border-red-700 bg-red-900/30 px-4 py-2.5 text-sm text-red-300">
			<span>{actionError}</span>
			<button onclick={() => (actionError = '')} class="cursor-pointer font-medium hover:underline" aria-label="Dismiss">✕</button>
		</div>
	{/if}

	<!-- Create User Form -->
	{#if showCreate}
		<div class="rounded-xl border border-gray-700 bg-gray-800 p-5">
			<h3 class="text-lg font-semibold text-white">Create New User</h3>
			<p class="mt-0.5 text-xs text-gray-400">Panel login, with an optional Linux SSH account under the same name.</p>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					createUser();
				}}
				class="mt-4 space-y-4"
			>
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<label for="new-username" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Username</label>
						<input
							id="new-username"
							type="text"
							bind:value={newUsername}
							required
							pattern="[a-z_][a-z0-9_-]*"
							spellcheck="false"
							autocomplete="off"
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
						{#if newSSHEnabled}
							<p class="mt-1 text-xs text-gray-500">Lowercase letters, digits, underscore, hyphen. This becomes the Linux/SSH account name and cannot be renamed later.</p>
						{/if}
					</div>
					<div>
						<label for="new-email" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Email</label>
						<input
							id="new-email"
							type="email"
							bind:value={newEmail}
							required
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
					</div>
					<div>
						<label for="new-password" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Password</label>
						<input
							id="new-password"
							type="password"
							bind:value={newPassword}
							required
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
					</div>
					<div>
						<label for="new-role" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Role</label>
						<select
							id="new-role"
							bind:value={newRole}
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						>
							<option value="user">User</option>
							<option value="admin">Admin</option>
						</select>
					</div>
				</div>
				<label class="flex cursor-pointer items-start gap-2.5 rounded-lg border border-gray-700 bg-gray-900/60 p-3" for="new-ssh-enabled">
					<input
						id="new-ssh-enabled"
						type="checkbox"
						bind:checked={newSSHEnabled}
						class="mt-0.5 h-4 w-4 cursor-pointer rounded border-gray-600 bg-gray-700 text-blue-600 focus:ring-blue-500"
					/>
					<span class="text-sm text-gray-300">
						SSH access
						<span class="block text-xs text-gray-500">
							Creates a Linux account with this username (password login disabled). The user can add public SSH keys from the panel and gets access to the websites they own.
						</span>
					</span>
				</label>
				<div class="flex items-center justify-end gap-2 border-t border-gray-700 pt-4">
					<button
						type="button"
						onclick={() => (showCreate = false)}
						class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={creating}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
					>
						{creating ? 'Creating…' : 'Create User'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	<!-- Search -->
	{#if !loading && users.length > 3}
		<div class="relative sm:w-72">
			<svg class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="m21 21-5.197-5.197m0 0A7.5 7.5 0 1 0 5.196 5.196a7.5 7.5 0 0 0 10.607 10.607Z" />
			</svg>
			<input
				type="search"
				bind:value={search}
				placeholder="Search username or email…"
				aria-label="Search users"
				class="w-full rounded-lg border border-gray-600 bg-gray-800 py-2 pl-9 pr-3 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
			/>
		</div>
	{/if}

	<!-- Users List -->
	{#if loading}
		<div class="space-y-3">
			{#each Array(3) as _}
				<div class="flex animate-pulse items-center gap-3 rounded-xl border border-gray-700 bg-gray-800 p-4">
					<div class="h-10 w-10 rounded-full bg-gray-700/60"></div>
					<div class="flex-1 space-y-2">
						<div class="h-3.5 w-1/4 rounded bg-gray-700/60"></div>
						<div class="h-3 w-1/3 rounded bg-gray-700/40"></div>
					</div>
					<div class="h-8 w-24 rounded bg-gray-700/30"></div>
				</div>
			{/each}
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-700 bg-red-900/30 p-4 text-sm text-red-300">{error}</div>
	{:else if users.length === 0}
		<div class="flex flex-col items-center gap-3 rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-12 text-center">
			<span class="flex h-12 w-12 items-center justify-center rounded-xl bg-blue-500/10 text-blue-400">
				<svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z" />
				</svg>
			</span>
			<div>
				<p class="font-medium text-gray-200">No users yet</p>
				<p class="mt-1 text-sm text-gray-400">Create the first account to grant panel and SSH access.</p>
			</div>
			<button
				onclick={() => (showCreate = true)}
				class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700"
			>
				Create User
			</button>
		</div>
	{:else if filteredUsers.length === 0}
		<div class="rounded-xl border border-dashed border-gray-600 bg-gray-800/50 px-6 py-10 text-center">
			<p class="text-sm text-gray-400">No users match "{search}".</p>
			<button
				onclick={() => (search = '')}
				class="mt-2 cursor-pointer text-sm text-blue-400 hover:underline"
			>
				Clear search
			</button>
		</div>
	{:else}
		<!-- Mobile: cards -->
		<div class="space-y-3 md:hidden">
			{#each filteredUsers as u (u.id)}
				<div class="rounded-xl border border-gray-700 bg-gray-800 p-4">
					<div class="flex items-center gap-3">
						<span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-blue-500/15 text-sm font-bold text-blue-300 ring-1 ring-blue-400/20">
							{initials(u.username)}
						</span>
						<div class="min-w-0 flex-1">
							<p class="truncate text-sm font-semibold text-white">{u.username}</p>
							<p class="truncate text-xs text-gray-400">{u.email}</p>
						</div>
					</div>
					<div class="mt-3 flex flex-wrap items-center gap-1.5">
						<span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {u.is_active ? 'bg-green-900/50 text-green-300' : 'bg-gray-700/60 text-gray-300'}">
							<span class="h-1.5 w-1.5 rounded-full {u.is_active ? 'bg-green-400' : 'bg-gray-400'}"></span>
							{u.is_active ? 'Active' : 'Inactive'}
						</span>
						<span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {u.ssh_enabled ? 'bg-purple-900/50 text-purple-300' : 'bg-gray-700/60 text-gray-300'}">
							{u.ssh_enabled ? 'SSH on' : 'SSH off'}
						</span>
						<span class="ml-auto text-xs text-gray-500">{new Date(u.created_at).toLocaleDateString()}</span>
					</div>
					<div class="mt-3 flex flex-wrap gap-2 border-t border-gray-700 pt-3">
						<button
							onclick={() => openKeys(u)}
							class="cursor-pointer rounded-md bg-purple-700/80 px-3 py-1.5 text-xs font-medium text-white transition hover:bg-purple-700"
						>
							SSH Keys
						</button>
						<button
							onclick={() => startEdit(u)}
							class="cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-3 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
						>
							Edit
						</button>
						<button
							onclick={() => deleteUser(u)}
							class="ml-auto cursor-pointer rounded-md px-3 py-1.5 text-xs text-red-400 transition hover:bg-red-500/10"
						>
							Delete
						</button>
					</div>
				</div>
			{/each}
		</div>

		<!-- Desktop: table -->
		<div class="hidden overflow-hidden rounded-xl border border-gray-700 bg-gray-800 md:block">
			<div class="overflow-x-auto">
				<table class="w-full">
					<thead>
						<tr class="border-b border-gray-700">
							<th class="px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wider text-gray-400">User</th>
							<th class="px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wider text-gray-400">Status</th>
							<th class="px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wider text-gray-400">SSH</th>
							<th class="px-4 py-3 text-left text-[11px] font-medium uppercase tracking-wider text-gray-400">Created</th>
							<th class="px-4 py-3 text-right text-[11px] font-medium uppercase tracking-wider text-gray-400">Actions</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-gray-700">
						{#each filteredUsers as u (u.id)}
							<tr class="transition hover:bg-gray-700/30">
								<td class="px-4 py-3">
									<div class="flex items-center gap-3">
										<span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-blue-500/15 text-xs font-bold text-blue-300 ring-1 ring-blue-400/20">
											{initials(u.username)}
										</span>
										<div class="min-w-0">
											<p class="truncate text-sm font-semibold text-white">{u.username}</p>
											<p class="truncate text-xs text-gray-400">{u.email}</p>
										</div>
									</div>
								</td>
								<td class="px-4 py-3">
									<span class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium {u.is_active ? 'bg-green-900/50 text-green-300' : 'bg-gray-700/60 text-gray-300'}">
										<span class="h-1.5 w-1.5 rounded-full {u.is_active ? 'bg-green-400' : 'bg-gray-400'}"></span>
										{u.is_active ? 'Active' : 'Inactive'}
									</span>
								</td>
								<td class="px-4 py-3">
									<span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium {u.ssh_enabled ? 'bg-purple-900/50 text-purple-300' : 'bg-gray-700/60 text-gray-300'}">
										{u.ssh_enabled ? 'Enabled' : 'Off'}
									</span>
								</td>
								<td class="px-4 py-3 text-sm text-gray-400">
									{new Date(u.created_at).toLocaleDateString()}
								</td>
								<td class="px-4 py-3">
									<div class="flex items-center justify-end gap-2">
										<button
											onclick={() => openKeys(u)}
											class="cursor-pointer rounded-md bg-purple-700/80 px-2.5 py-1.5 text-xs font-medium text-white transition hover:bg-purple-700"
										>
											SSH Keys
										</button>
										<button
											onclick={() => startEdit(u)}
											class="cursor-pointer rounded-md border border-gray-600 bg-gray-700 px-2.5 py-1.5 text-xs text-gray-200 transition hover:bg-gray-600"
										>
											Edit
										</button>
										<button
											onclick={() => deleteUser(u)}
											class="cursor-pointer rounded-md px-2.5 py-1.5 text-xs text-red-400 transition hover:bg-red-500/10"
										>
											Delete
										</button>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</div>
	{/if}
</div>

<!-- Edit User modal -->
{#if editingUser}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		aria-label="Edit user"
		tabindex="-1"
	>
		<div class="w-full max-w-lg rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<div class="mb-4 flex items-center justify-between">
				<h3 class="text-lg font-semibold text-white">
					Edit User
					<span class="ml-1 text-blue-400">{editingUser.username}</span>
				</h3>
				<button
					type="button"
					onclick={() => (editingUser = null)}
					class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
					aria-label="Close"
				>
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
					</svg>
				</button>
			</div>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					saveEdit();
				}}
				class="space-y-4"
			>
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<label for="edit-username" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Username</label>
						<input
							id="edit-username"
							type="text"
							bind:value={editUsername}
							disabled={editingUser.ssh_enabled}
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40 disabled:opacity-60"
						/>
						{#if editingUser.ssh_enabled}
							<p class="mt-1 text-xs text-gray-500">Usernames of SSH-enabled accounts cannot be changed.</p>
						{/if}
					</div>
					<div>
						<label for="edit-email" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Email</label>
						<input
							id="edit-email"
							type="email"
							bind:value={editEmail}
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
					</div>
				</div>
				<label class="flex cursor-pointer items-center gap-2.5" for="edit-active">
					<input
						id="edit-active"
						type="checkbox"
						bind:checked={editIsActive}
						class="h-4 w-4 cursor-pointer rounded border-gray-600 bg-gray-700 text-blue-600 focus:ring-blue-500"
					/>
					<span class="text-sm text-gray-300">Active — this account can log in to the panel.</span>
				</label>
				<label class="flex cursor-pointer items-start gap-2.5 rounded-lg border border-gray-700 bg-gray-900/60 p-3" for="edit-ssh-enabled">
					<input
						id="edit-ssh-enabled"
						type="checkbox"
						bind:checked={editSSHEnabled}
						class="mt-0.5 h-4 w-4 cursor-pointer rounded border-gray-600 bg-gray-700 text-blue-600 focus:ring-blue-500"
					/>
					<span class="text-sm text-gray-300">
						SSH access
						<span class="block text-xs text-gray-500">
							{editingUser.ssh_enabled
								? 'Turning this off locks the Linux account (files are kept).'
								: 'Turning this on provisions the Linux account /home/' + editingUser.username + '.'}
						</span>
					</span>
				</label>
				<div class="flex items-center justify-end gap-2 border-t border-gray-700 pt-4">
					<button
						type="button"
						onclick={() => (editingUser = null)}
						class="cursor-pointer rounded-lg border border-gray-600 bg-gray-700 px-4 py-2 text-sm font-medium text-gray-200 transition hover:bg-gray-600"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={saving}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
					>
						{saving ? 'Saving…' : 'Save Changes'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- SSH Keys modal -->
{#if keysUser}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
		role="dialog"
		aria-modal="true"
		aria-label="SSH keys"
		tabindex="-1"
	>
		<div class="w-full max-w-2xl rounded-2xl border border-gray-700 bg-gray-800 p-5 shadow-2xl">
			<div class="mb-4 flex items-start justify-between gap-3">
				<div class="flex items-center gap-3">
					<span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-purple-500/10 text-purple-300">
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 5.25a3 3 0 0 1 3 3m3 0a6 6 0 0 1-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1 1 21.75 8.25Z" />
						</svg>
					</span>
					<div>
						<h3 class="text-lg font-semibold text-white">SSH Keys — {keysUser.username}</h3>
						{#if !keysUser.ssh_enabled}
							<p class="text-xs text-yellow-400">SSH access is disabled for this account</p>
						{:else}
							<p class="text-xs text-gray-400">authorized_keys is rewritten from this list whenever it changes.</p>
						{/if}
					</div>
				</div>
				<button
					type="button"
					onclick={() => (keysUser = null)}
					class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white"
					aria-label="Close"
				>
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
					</svg>
				</button>
			</div>

			{#if keysError}
				<div class="mb-3 flex items-start justify-between gap-3 rounded-lg border border-red-700 bg-red-900/30 px-3 py-2.5 text-sm text-red-300">
					<span>{keysError}</span>
					<button onclick={() => (keysError = '')} class="cursor-pointer font-medium hover:underline" aria-label="Dismiss">✕</button>
				</div>
			{/if}

			{#if keysLoading}
				<div class="space-y-2">
					{#each Array(2) as _}
						<div class="h-14 animate-pulse rounded-lg bg-gray-700/40"></div>
					{/each}
				</div>
			{:else if keys.length === 0}
				<div class="rounded-lg border border-dashed border-gray-600 px-4 py-8 text-center">
					<p class="text-sm font-medium text-gray-300">No SSH keys stored</p>
					<p class="mt-1 text-xs text-gray-500">Add the first public key below to grant key-based SSH access.</p>
				</div>
			{:else}
				<ul class="mb-4 max-h-64 space-y-2 overflow-y-auto">
					{#each keys as key (key.id)}
						<li class="flex items-start justify-between gap-3 rounded-lg border border-gray-700 bg-gray-900/60 px-3 py-2.5">
							<div class="min-w-0">
								<p class="truncate text-sm font-medium text-gray-200">{key.name || '(unlabeled)'}</p>
								<p class="truncate font-mono text-xs text-gray-400">{key.fingerprint}</p>
								<p class="mt-0.5 text-xs text-gray-500">{key.algo} {key.bits}-bit · added {new Date(key.created_at).toLocaleDateString()}</p>
							</div>
							<button
								onclick={() => deleteKey(key)}
								class="shrink-0 cursor-pointer rounded-md px-2.5 py-1.5 text-xs font-medium text-red-400 transition hover:bg-red-500/10"
							>
								Delete
							</button>
						</li>
					{/each}
				</ul>
			{/if}

			<form
				onsubmit={(e) => {
					e.preventDefault();
					addKey();
				}}
				class="space-y-3 border-t border-gray-700 pt-4"
			>
				<div class="grid gap-3 sm:grid-cols-3">
					<div>
						<label for="key-name" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Label</label>
						<input
							id="key-name"
							type="text"
							bind:value={newKeyName}
							placeholder="laptop"
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
					</div>
					<div class="sm:col-span-2">
						<label for="key-material" class="mb-1 block text-[11px] font-medium uppercase tracking-wider text-gray-400">Public key (ssh-ed25519 / ssh-rsa / ecdsa)</label>
						<input
							id="key-material"
							type="text"
							bind:value={newKeyMaterial}
							placeholder="ssh-ed25519 AAAA... user@host"
							spellcheck="false"
							autocomplete="off"
							class="w-full rounded-lg border border-gray-600 bg-gray-900 px-3 py-2 font-mono text-xs text-gray-200 placeholder-gray-500 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/40"
						/>
					</div>
				</div>
				<div class="flex justify-end">
					<button
						type="submit"
						disabled={addingKey || !newKeyMaterial.trim()}
						class="cursor-pointer rounded-lg bg-blue-600 px-4 py-2 text-sm font-semibold text-white transition hover:bg-blue-700 disabled:opacity-50"
					>
						{addingKey ? 'Adding…' : 'Add Key'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
