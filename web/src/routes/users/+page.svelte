<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import type { User } from '$lib/types';

	let users = $state<User[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let showCreate = $state(false);
	let newUsername = $state('');
	let newEmail = $state('');
	let newPassword = $state('');
	let newRole = $state('user');
	let creating = $state(false);

	// Edit form
	let editingUser = $state<User | null>(null);
	let editUsername = $state('');
	let editEmail = $state('');
	let editIsActive = $state(true);
	let saving = $state(false);

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
				role: newRole
			});
			actionMsg = `User "${newUsername}" created successfully.`;
			showCreate = false;
			newUsername = '';
			newEmail = '';
			newPassword = '';
			newRole = 'user';
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
				is_active: editIsActive
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
		if (!confirm(`Are you sure you want to delete user "${u.username}"?`)) return;

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

	onMount(loadUsers);
</script>

<div class="space-y-6">
	<div class="flex items-center justify-between">
		<h2 class="text-2xl font-bold text-white">Users</h2>
		<button
			onclick={() => (showCreate = !showCreate)}
			class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
		>
			{showCreate ? 'Cancel' : 'Create User'}
		</button>
	</div>

	{#if actionMsg}
		<div class="p-3 bg-green-900/50 border border-green-700 rounded-lg text-green-300 text-sm">
			{actionMsg}
			<button onclick={() => (actionMsg = '')} class="ml-2 text-green-400 hover:text-green-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	{#if actionError}
		<div class="p-3 bg-red-900/50 border border-red-700 rounded-lg text-red-300 text-sm">
			{actionError}
			<button onclick={() => (actionError = '')} class="ml-2 text-red-400 hover:text-red-200 cursor-pointer">
				Dismiss
			</button>
		</div>
	{/if}

	<!-- Create User Form -->
	{#if showCreate}
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-6">
			<h3 class="text-lg font-semibold text-white mb-4">Create New User</h3>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					createUser();
				}}
				class="grid grid-cols-1 md:grid-cols-2 gap-4"
			>
				<div>
					<label for="new-username" class="block text-sm text-gray-300 mb-1">Username</label>
					<input
						id="new-username"
						type="text"
						bind:value={newUsername}
						required
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="new-email" class="block text-sm text-gray-300 mb-1">Email</label>
					<input
						id="new-email"
						type="email"
						bind:value={newEmail}
						required
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="new-password" class="block text-sm text-gray-300 mb-1">Password</label>
					<input
						id="new-password"
						type="password"
						bind:value={newPassword}
						required
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="new-role" class="block text-sm text-gray-300 mb-1">Role</label>
					<select
						id="new-role"
						bind:value={newRole}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="user">User</option>
						<option value="admin">Admin</option>
					</select>
				</div>
				<div class="md:col-span-2">
					<button
						type="submit"
						disabled={creating}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						{creating ? 'Creating...' : 'Create User'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	<!-- Edit User Modal (inline) -->
	{#if editingUser}
		<div class="bg-gray-800 rounded-lg border border-blue-600 p-6">
			<h3 class="text-lg font-semibold text-white mb-4">
				Edit User: {editingUser.username}
			</h3>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					saveEdit();
				}}
				class="grid grid-cols-1 md:grid-cols-2 gap-4"
			>
				<div>
					<label for="edit-username" class="block text-sm text-gray-300 mb-1">Username</label>
					<input
						id="edit-username"
						type="text"
						bind:value={editUsername}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="edit-email" class="block text-sm text-gray-300 mb-1">Email</label>
					<input
						id="edit-email"
						type="email"
						bind:value={editEmail}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded-lg text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div class="flex items-center gap-2">
					<input
						id="edit-active"
						type="checkbox"
						bind:checked={editIsActive}
						class="w-4 h-4 rounded border-gray-600 bg-gray-700 text-blue-600 focus:ring-blue-500"
					/>
					<label for="edit-active" class="text-sm text-gray-300">Active</label>
				</div>
				<div class="md:col-span-2 flex gap-2">
					<button
						type="submit"
						disabled={saving}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						{saving ? 'Saving...' : 'Save Changes'}
					</button>
					<button
						type="button"
						onclick={() => (editingUser = null)}
						class="px-4 py-2 bg-gray-600 hover:bg-gray-500 text-white text-sm font-medium rounded-lg transition-colors cursor-pointer"
					>
						Cancel
					</button>
				</div>
			</form>
		</div>
	{/if}

	<!-- Users Table -->
	{#if loading}
		<div class="text-gray-400">Loading users...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<div class="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
			<table class="w-full">
				<thead>
					<tr class="border-b border-gray-700 bg-gray-800/80">
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>Username</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>Email</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>Status</th
						>
						<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>Created</th
						>
						<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium"
							>Actions</th
						>
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-700">
					{#each users as u}
						<tr class="hover:bg-gray-750">
							<td class="px-4 py-3 text-sm text-white font-medium">{u.username}</td>
							<td class="px-4 py-3 text-sm text-gray-300">{u.email}</td>
							<td class="px-4 py-3">
								<span
									class="inline-flex px-2 py-0.5 rounded-full text-xs font-medium {u.is_active
										? 'bg-green-900/50 text-green-400'
										: 'bg-gray-700 text-gray-400'}"
								>
									{u.is_active ? 'Active' : 'Inactive'}
								</span>
							</td>
							<td class="px-4 py-3 text-sm text-gray-400">
								{new Date(u.created_at).toLocaleDateString()}
							</td>
							<td class="px-4 py-3 text-right">
								<div class="flex items-center justify-end gap-2">
									<button
										onclick={() => startEdit(u)}
										class="px-2.5 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded cursor-pointer"
									>
										Edit
									</button>
									<button
										onclick={() => deleteUser(u)}
										class="px-2.5 py-1 bg-red-600/80 hover:bg-red-600 text-white text-xs rounded cursor-pointer"
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
	{/if}
</div>
