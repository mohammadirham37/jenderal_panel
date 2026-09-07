<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface FirewallStatus {
		active: boolean;
		default_policy: string;
	}

	interface FirewallRule {
		number: number;
		to: string;
		action: string;
		from: string;
		comment: string;
	}

	let status = $state<FirewallStatus | null>(null);
	let rules = $state<FirewallRule[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');
	let actionInProgress = $state(false);

	// Disable confirm dialog
	let showDisableConfirm = $state(false);

	// Delete confirm dialog
	let deleteConfirm = $state<{ rule: FirewallRule; showSshWarning: boolean } | null>(null);

	// Add rule form
	let newPort = $state('');
	let newProtocol = $state('tcp');
	let newAction = $state('allow');
	let newFrom = $state('');
	let newComment = $state('');
	let addError = $state('');

	async function loadStatus() {
		try {
			const data = await api.get<{ status: FirewallStatus; rules: FirewallRule[] }>('/api/v1/firewall/status');
			status = data.status;
			rules = data.rules || [];
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load firewall status';
		} finally {
			loading = false;
		}
	}

	async function enableFirewall() {
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			await api.post('/api/v1/firewall/enable');
			actionMsg = 'Firewall enabled successfully.';
			await loadStatus();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to enable firewall';
		} finally {
			actionInProgress = false;
		}
	}

	async function disableFirewall() {
		showDisableConfirm = false;
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			await api.post('/api/v1/firewall/disable');
			actionMsg = 'Firewall disabled.';
			await loadStatus();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to disable firewall';
		} finally {
			actionInProgress = false;
		}
	}

	async function deleteRule(rule: FirewallRule, force: boolean = false) {
		deleteConfirm = null;
		actionMsg = '';
		actionError = '';
		actionInProgress = true;
		try {
			const forceParam = force ? '?force=true' : '';
			await api.del(`/api/v1/firewall/rules/${rule.number}${forceParam}`);
			actionMsg = `Rule #${rule.number} deleted.`;
			await loadStatus();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete rule';
		} finally {
			actionInProgress = false;
		}
	}

	function confirmDelete(rule: FirewallRule) {
		const isSshRule = rule.to === '22' || rule.to === '22/tcp' || rule.to === 'OpenSSH';
		deleteConfirm = { rule, showSshWarning: isSshRule };
	}

	async function addRule() {
		addError = '';
		actionMsg = '';
		actionError = '';
		if (!newPort.trim()) {
			addError = 'Port is required.';
			return;
		}
		actionInProgress = true;
		try {
			await api.post('/api/v1/firewall/rules', {
				port: newPort.trim(),
				protocol: newProtocol,
				action: newAction,
				from: newFrom.trim() || undefined,
				comment: newComment.trim() || undefined
			});
			actionMsg = `Rule added for port ${newPort}.`;
			newPort = '';
			newProtocol = 'tcp';
			newAction = 'allow';
			newFrom = '';
			newComment = '';
			await loadStatus();
		} catch (err) {
			addError = err instanceof Error ? err.message : 'Failed to add rule';
		} finally {
			actionInProgress = false;
		}
	}

	function actionColor(action: string): string {
		const lower = action.toLowerCase();
		if (lower === 'allow') return 'bg-green-900/50 text-green-400';
		if (lower === 'deny' || lower === 'reject') return 'bg-red-900/50 text-red-400';
		if (lower === 'limit') return 'bg-yellow-900/50 text-yellow-400';
		return 'bg-gray-700 text-gray-300';
	}

	onMount(loadStatus);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Firewall</h2>

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
		<div class="text-gray-400">Loading firewall status...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if status}
		<!-- Status Card -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.active ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.active ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.active ? 'Active' : 'Inactive'}
				</span>
				{#if status.default_policy}
					<span class="text-xs text-gray-400">Default Policy: <span class="text-gray-200">{status.default_policy}</span></span>
				{/if}
			</div>

			<div class="flex gap-2">
				{#if !status.active}
					<button
						onclick={enableFirewall}
						disabled={actionInProgress}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Enable
					</button>
				{:else}
					<button
						onclick={() => (showDisableConfirm = true)}
						disabled={actionInProgress}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						Disable
					</button>
				{/if}
			</div>

			{#if showDisableConfirm}
				<div class="mt-3 p-4 bg-red-950 border border-red-700 rounded-lg">
					<p class="text-red-300 text-sm font-medium mb-1">Warning: Disabling the firewall may affect SSH access!</p>
					<p class="text-red-400 text-xs mb-3">All firewall rules will be deactivated. Make sure you have alternative access to the server.</p>
					<div class="flex gap-2">
						<button
							onclick={disableFirewall}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Confirm Disable
						</button>
						<button
							onclick={() => (showDisableConfirm = false)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Cancel
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Rules Table -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Rules</h3>

			{#if rules.length === 0}
				<div class="text-gray-400 text-sm">No firewall rules configured.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">#</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">To</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Action</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">From</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Comment</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each rules as rule}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{rule.number}</td>
									<td class="px-4 py-3 text-sm text-white font-mono">{rule.to}</td>
									<td class="px-4 py-3">
										<span class="inline-flex px-2 py-0.5 rounded text-xs font-medium {actionColor(rule.action)}">
											{rule.action}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{rule.from || 'Anywhere'}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{rule.comment || '-'}</td>
									<td class="px-4 py-3 text-right">
										<button
											onclick={() => confirmDelete(rule)}
											disabled={actionInProgress}
											class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
										>
											Delete
										</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			{#if deleteConfirm}
				<div class="mt-3 p-4 bg-gray-900 border border-gray-600 rounded-lg">
					{#if deleteConfirm.showSshWarning}
						<p class="text-red-400 text-sm font-medium mb-1">Warning: This rule appears to be for SSH (port 22)!</p>
						<p class="text-red-500 text-xs mb-3">Deleting this rule may lock you out of the server.</p>
					{:else}
						<p class="text-gray-300 text-sm mb-3">Delete rule #{deleteConfirm.rule.number} ({deleteConfirm.rule.to} {deleteConfirm.rule.action})?</p>
					{/if}
					<div class="flex gap-2">
						<button
							onclick={() => deleteRule(deleteConfirm!.rule, deleteConfirm!.showSshWarning)}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{deleteConfirm.showSshWarning ? 'Force Delete' : 'Confirm Delete'}
						</button>
						<button
							onclick={() => (deleteConfirm = null)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							Cancel
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Add Rule -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Add Rule</h3>

			{#if addError}
				<div class="mb-3 text-red-400 text-sm">{addError}</div>
			{/if}

			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
				<div>
					<label for="fw-port" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Port</label>
					<input
						id="fw-port"
						type="text"
						bind:value={newPort}
						placeholder="e.g. 80, 443, 8080"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="fw-protocol" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Protocol</label>
					<select
						id="fw-protocol"
						bind:value={newProtocol}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="tcp">TCP</option>
						<option value="udp">UDP</option>
						<option value="both">Both</option>
					</select>
				</div>
				<div>
					<label for="fw-action" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Action</label>
					<select
						id="fw-action"
						bind:value={newAction}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="allow">Allow</option>
						<option value="deny">Deny</option>
						<option value="limit">Limit</option>
					</select>
				</div>
				<div>
					<label for="fw-from" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">From IP (optional)</label>
					<input
						id="fw-from"
						type="text"
						bind:value={newFrom}
						placeholder="e.g. 192.168.1.0/24"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="fw-comment" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Comment (optional)</label>
					<input
						id="fw-comment"
						type="text"
						bind:value={newComment}
						placeholder="e.g. Allow HTTP"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div class="flex items-end">
					<button
						onclick={addRule}
						disabled={actionInProgress}
						class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded transition-colors cursor-pointer"
					>
						Add Rule
					</button>
				</div>
			</div>
		</div>
	{/if}
</div>
