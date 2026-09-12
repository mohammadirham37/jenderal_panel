<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { firewallActionTone, normalizeFirewallStatus, parseFirewallPort } from '$lib/firewall.js';
import { toast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';

	interface FirewallStatus {
		active: boolean;
		defaultPolicy: string;
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
		loading = true;
		error = '';
		try {
			const data = await api.get<{ active: boolean; default: string; rules: FirewallRule[] }>('/api/v1/firewall/status');
			const normalized = normalizeFirewallStatus(data);
			status = { active: normalized.active, defaultPolicy: normalized.defaultPolicy };
			rules = normalized.rules as FirewallRule[];
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'fw.loadFailed');
		} finally {
			loading = false;
		}
	}

	async function enableFirewall() {
		actionInProgress = true;
		try {
			await api.post('/api/v1/firewall/enable');
			toast.success(translate($language, 'fw.enabledToast'));
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'fw.enableFailed'));
		} finally {
			actionInProgress = false;
		}
	}

	async function disableFirewall() {
		showDisableConfirm = false;
		actionInProgress = true;
		try {
			await api.post('/api/v1/firewall/disable');
			toast.success(translate($language, 'fw.disabledToast'));
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'fw.disableFailed'));
		} finally {
			actionInProgress = false;
		}
	}

	async function deleteRule(rule: FirewallRule, force: boolean = false) {
		deleteConfirm = null;
		actionInProgress = true;
		try {
			const forceParam = force ? '?force=true' : '';
			await api.del(`/api/v1/firewall/rules/${rule.number}${forceParam}`);
			toast.success(translate($language, 'fw.ruleDeleted').replace('{n}', String(rule.number)));
			await loadStatus();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'fw.deleteFailed'));
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
		if (!newPort.trim()) {
			addError = translate($language, 'fw.portRequired');
			return;
		}
		actionInProgress = true;
		try {
			const port = parseFirewallPort(newPort);
			await api.post('/api/v1/firewall/rules', {
				port,
				protocol: newProtocol,
				action: newAction,
				from: newFrom.trim() || undefined,
				comment: newComment.trim() || undefined
			});
			toast.success(translate($language, 'fw.ruleAdded').replace('{port}', newPort));
			newPort = '';
			newProtocol = 'tcp';
			newAction = 'allow';
			newFrom = '';
			newComment = '';
			await loadStatus();
		} catch (err) {
			addError = err instanceof Error ? err.message : translate($language, 'fw.addFailed');
		} finally {
			actionInProgress = false;
		}
	}

	function actionColor(action: string): string {
		const tone = firewallActionTone(action);
		if (tone === 'allow') return 'bg-green-900/50 text-green-400';
		if (tone === 'deny') return 'bg-red-900/50 text-red-400';
		if (tone === 'limit') return 'bg-yellow-900/50 text-yellow-400';
		return 'bg-gray-700 text-gray-300';
	}

	onMount(loadStatus);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'fw.title')}</h2>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'fw.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else if status}
		<!-- Status Card -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<div class="flex flex-wrap items-center gap-3 mb-4">
				<span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium {status.active ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
					<span class="w-1.5 h-1.5 rounded-full {status.active ? 'bg-green-400' : 'bg-red-400'}"></span>
					{status.active ? translate($language, 'fw.active') : translate($language, 'fw.inactive')}
				</span>
				{#if status.defaultPolicy}
					<span class="text-xs text-gray-400">{translate($language, 'fw.defaultPolicy')} <span class="text-gray-200">{status.defaultPolicy}</span></span>
				{/if}
			</div>

			<div class="flex gap-2">
				{#if !status.active}
					<button
						onclick={enableFirewall}
						disabled={actionInProgress}
						class="px-3 py-1.5 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{translate($language, 'fw.enable')}
					</button>
				{:else}
					<button
						onclick={() => (showDisableConfirm = true)}
						disabled={actionInProgress}
						class="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm rounded transition-colors cursor-pointer"
					>
						{translate($language, 'fw.disable')}
					</button>
				{/if}
			</div>

			{#if showDisableConfirm}
				<div class="mt-3 p-4 bg-red-950 border border-red-700 rounded-lg">
					<p class="text-red-300 text-sm font-medium mb-1">{translate($language, 'fw.disableWarning')}</p>
					<p class="text-red-400 text-xs mb-3">{translate($language, 'fw.disableHint')}</p>
					<div class="flex gap-2">
						<button
							onclick={disableFirewall}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'fw.confirmDisable')}
						</button>
						<button
							onclick={() => (showDisableConfirm = false)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'fw.cancel')}
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Rules Table -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'fw.rules')}</h3>

			{#if rules.length === 0}
				<div class="text-gray-400 text-sm">{translate($language, 'fw.noRules')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">#</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'fw.thTo')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'fw.thAction')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'fw.thFrom')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'fw.thComment')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'fw.thActions')}</th>
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
									<td class="px-4 py-3 text-sm text-gray-400 font-mono">{rule.from || translate($language, 'fw.anywhere')}</td>
									<td class="px-4 py-3 text-sm text-gray-400">{rule.comment || '-'}</td>
									<td class="px-4 py-3 text-right">
										<button
											onclick={() => confirmDelete(rule)}
											disabled={actionInProgress}
											class="px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded transition-colors cursor-pointer"
										>
											{translate($language, 'fw.delete')}
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
						<p class="text-red-400 text-sm font-medium mb-1">{translate($language, 'fw.sshWarning')}</p>
						<p class="text-red-500 text-xs mb-3">{translate($language, 'fw.lockoutHint')}</p>
					{:else}
						<p class="text-gray-300 text-sm mb-3">{translate($language, 'fw.deleteAsk').replace('{n}', String(deleteConfirm.rule.number)).replace('{to}', deleteConfirm.rule.to).replace('{action}', deleteConfirm.rule.action)}</p>
					{/if}
					<div class="flex gap-2">
						<button
							onclick={() => deleteRule(deleteConfirm!.rule, deleteConfirm!.showSshWarning)}
							class="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{deleteConfirm.showSshWarning ? translate($language, 'fw.forceDelete') : translate($language, 'fw.confirmDelete')}
						</button>
						<button
							onclick={() => (deleteConfirm = null)}
							class="px-3 py-1.5 bg-gray-600 hover:bg-gray-500 text-white text-sm rounded transition-colors cursor-pointer"
						>
							{translate($language, 'fw.cancel')}
						</button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Add Rule -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'fw.addRule')}</h3>

			{#if addError}
				<div class="mb-3 text-red-400 text-sm">{addError}</div>
			{/if}

			<div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
				<div>
					<label for="fw-port" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'fw.port')}</label>
					<input
						id="fw-port"
						type="text"
						bind:value={newPort}
						placeholder="e.g. 80, 443, 8080"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="fw-protocol" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'fw.protocol')}</label>
					<select
						id="fw-protocol"
						bind:value={newProtocol}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="tcp">TCP</option>
						<option value="udp">UDP</option>
						<option value="both">{translate($language, 'fw.both')}</option>
					</select>
				</div>
				<div>
					<label for="fw-action" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Action</label>
					<select
						id="fw-action"
						bind:value={newAction}
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					>
						<option value="allow">{translate($language, 'fw.allow')}</option>
						<option value="deny">{translate($language, 'fw.deny')}</option>
						<option value="limit">{translate($language, 'fw.limit')}</option>
					</select>
				</div>
				<div>
					<label for="fw-from" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'fw.fromIp')}</label>
					<input
						id="fw-from"
						type="text"
						bind:value={newFrom}
						placeholder="e.g. 192.168.1.0/24"
						class="w-full px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
				<div>
					<label for="fw-comment" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'fw.commentOptional')}</label>
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
						{translate($language, 'fw.addRule')}
					</button>
				</div>
			</div>
		</div>
	{/if}
</div>
