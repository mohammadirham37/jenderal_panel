<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	interface AlertRule {
		id: string;
		metric: string;
		operator: string;
		threshold: number;
		enabled: boolean;
		created_at: string;
	}

	interface AlertHistory {
		id: string;
		rule_id: string;
		metric: string;
		value: number;
		message: string;
		resolved: boolean;
		created_at: string;
	}

	let rules = $state<AlertRule[]>([]);
	let history = $state<AlertHistory[]>([]);
	let loading = $state(true);
	let error = $state('');
	let actionMsg = $state('');
	let actionError = $state('');

	// Create form
	let newMetric = $state('cpu');
	let newOperator = $state('gt');
	let newThreshold = $state(80);

	// Edit state
	let editingRule = $state<AlertRule | null>(null);
	let editMetric = $state('');
	let editOperator = $state('');
	let editThreshold = $state(0);

	const metricOptions = ['cpu', 'ram', 'disk', 'ssl_expiry', 'service_down'];
	const operatorOptions = ['gt', 'lt', 'eq'];

	function operatorLabel(op: string): string {
		switch (op) {
			case 'gt': return '>';
			case 'lt': return '<';
			case 'eq': return '=';
			default: return op;
		}
	}

	async function loadData() {
		try {
			const [rulesData, historyData] = await Promise.all([
				api.get<AlertRule[]>('/api/v1/alerts/rules'),
				api.get<AlertHistory[]>('/api/v1/alerts/history')
			]);
			rules = rulesData || [];
			history = (historyData || []).sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load alerts';
		} finally {
			loading = false;
		}
	}

	async function addRule() {
		actionMsg = '';
		actionError = '';
		try {
			await api.post('/api/v1/alerts/rules', {
				metric: newMetric,
				operator: newOperator,
				threshold: newThreshold
			});
			actionMsg = 'Alert rule created.';
			newMetric = 'cpu';
			newOperator = 'gt';
			newThreshold = 80;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to create rule';
		}
	}

	async function toggleRule(rule: AlertRule) {
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/alerts/rules/${rule.id}`, { enabled: !rule.enabled });
			actionMsg = `Rule ${rule.enabled ? 'disabled' : 'enabled'}.`;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to toggle rule';
		}
	}

	function startEdit(rule: AlertRule) {
		editingRule = rule;
		editMetric = rule.metric;
		editOperator = rule.operator;
		editThreshold = rule.threshold;
	}

	async function saveEdit() {
		if (!editingRule) return;
		actionMsg = '';
		actionError = '';
		try {
			await api.put(`/api/v1/alerts/rules/${editingRule.id}`, {
				metric: editMetric,
				operator: editOperator,
				threshold: editThreshold,
				enabled: editingRule.enabled
			});
			actionMsg = 'Rule updated.';
			editingRule = null;
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to update rule';
		}
	}

	async function deleteRule(ruleId: string) {
		actionMsg = '';
		actionError = '';
		try {
			await api.del(`/api/v1/alerts/rules/${ruleId}`);
			actionMsg = 'Rule deleted.';
			await loadData();
		} catch (err) {
			actionError = err instanceof Error ? err.message : 'Failed to delete rule';
		}
	}

	onMount(loadData);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">Alerts</h2>

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
		<div class="text-gray-400">Loading alerts...</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<!-- Alert Rules -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Alert Rules</h3>

			{#if rules.length === 0}
				<div class="text-gray-400 text-sm mb-4">No alert rules configured.</div>
			{:else}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Metric</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Operator</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Threshold</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Enabled</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each rules as rule}
								<tr class="hover:bg-gray-750">
									{#if editingRule?.id === rule.id}
										<td class="px-4 py-3">
											<select bind:value={editMetric} class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
												{#each metricOptions as m}
													<option value={m}>{m}</option>
												{/each}
											</select>
										</td>
										<td class="px-4 py-3">
											<select bind:value={editOperator} class="px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
												{#each operatorOptions as op}
													<option value={op}>{operatorLabel(op)}</option>
												{/each}
											</select>
										</td>
										<td class="px-4 py-3">
											<input type="number" bind:value={editThreshold} class="w-24 px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
										</td>
										<td class="px-4 py-3"></td>
										<td class="px-4 py-3 text-right">
											<div class="flex justify-end gap-2">
												<button onclick={saveEdit} class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer">Save</button>
												<button onclick={() => (editingRule = null)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Cancel</button>
											</div>
										</td>
									{:else}
										<td class="px-4 py-3 text-sm text-white font-mono">{rule.metric}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{operatorLabel(rule.operator)}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{rule.threshold}</td>
										<td class="px-4 py-3">
											<button
												onclick={() => toggleRule(rule)}
												class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors cursor-pointer {rule.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
											>
												<span class="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform {rule.enabled ? 'translate-x-4.5' : 'translate-x-0.5'}"></span>
											</button>
										</td>
										<td class="px-4 py-3 text-right">
											<div class="flex justify-end gap-2">
												<button onclick={() => startEdit(rule)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">Edit</button>
												<button onclick={() => deleteRule(rule.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">Delete</button>
											</div>
										</td>
									{/if}
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			<!-- Add Rule Form -->
			<div class="flex flex-wrap items-end gap-3 pt-3 border-t border-gray-700">
				<div>
					<label for="alert-metric" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Metric</label>
					<select id="alert-metric" bind:value={newMetric} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
						{#each metricOptions as m}
							<option value={m}>{m}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="alert-operator" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Operator</label>
					<select id="alert-operator" bind:value={newOperator} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
						{#each operatorOptions as op}
							<option value={op}>{operatorLabel(op)}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="alert-threshold" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">Threshold</label>
					<input id="alert-threshold" type="number" bind:value={newThreshold} class="w-24 px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
				</div>
				<button onclick={addRule} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					Add
				</button>
			</div>
		</div>

		<!-- Alert History -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">Alert History</h3>

			{#if history.length === 0}
				<div class="text-gray-400 text-sm">No alert history.</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Metric</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Value</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Message</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Status</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">Timestamp</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-gray-700">
							{#each history as entry}
								<tr class="hover:bg-gray-750">
									<td class="px-4 py-3 text-sm text-white font-mono">{entry.metric}</td>
									<td class="px-4 py-3 text-sm text-gray-300 font-mono">{entry.value}</td>
									<td class="px-4 py-3 text-sm text-gray-300">{entry.message}</td>
									<td class="px-4 py-3">
										<span class="inline-flex px-2 py-0.5 rounded text-xs font-medium {entry.resolved ? 'bg-green-900/50 text-green-400' : 'bg-red-900/50 text-red-400'}">
											{entry.resolved ? 'Resolved' : 'Active'}
										</span>
									</td>
									<td class="px-4 py-3 text-sm text-gray-400">{new Date(entry.created_at).toLocaleString()}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}
</div>
