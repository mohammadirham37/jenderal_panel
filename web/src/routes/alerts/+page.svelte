<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
import { toast } from '$lib/stores/toast';
import { language, translate } from '$lib/stores/language';

	interface AlertRule {
		id: string;
		metric: string;
		target: string;
		operator: string;
		threshold: number;
		duration_s: number;
		enabled: boolean;
		created_at: string;
	}
	interface ServiceStatus { name: string; installed: boolean; }
	interface SSLCertificate { domain: string; status: string; }

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
	let services = $state<ServiceStatus[]>([]);
	let certificates = $state<SSLCertificate[]>([]);
	let loading = $state(true);
	let error = $state('');

	// Create form
	let newMetric = $state('cpu');
	let newOperator = $state('gt');
	let newThreshold = $state(80);
	let newTarget = $state('');
	let newDuration = $state(0);

	// Edit state
	let editingRule = $state<AlertRule | null>(null);
	let editMetric = $state('');
	let editOperator = $state('');
	let editThreshold = $state(0);
	let editTarget = $state('');
	let editDuration = $state(0);

	const metricOptions = ['cpu', 'ram', 'disk', 'load1', 'load5', 'load15', 'ssl_expiry', 'service_down'];
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
		loading = true;
		error = '';
		try {
			const [rulesData, historyData, servicesData, certificatesData] = await Promise.all([
				api.get<AlertRule[]>('/api/v1/alert-rules'),
				api.get<AlertHistory[]>('/api/v1/alert-history'),
				api.get<ServiceStatus[]>('/api/v1/services'),
				api.get<SSLCertificate[]>('/api/v1/ssl')
			]);
			rules = rulesData || [];
			history = (historyData || []).sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
			services = (servicesData || []).filter((service) => service.installed);
			certificates = (certificatesData || []).filter((certificate) => certificate.status === 'active');
		} catch (err) {
			error = err instanceof Error ? err.message : translate($language, 'alr.failed_load');
		} finally {
			loading = false;
		}
	}

	async function addRule() {
		try {
			await api.post('/api/v1/alert-rules', {
				metric: newMetric,
				target: newMetric === 'service_down' || newMetric === 'ssl_expiry' ? newTarget : '',
				operator: newOperator,
				threshold: Number(newThreshold),
				duration_s: Number(newDuration)
			});
			toast.success(translate($language, 'alr.toast_created'));
			newMetric = 'cpu';
			newOperator = 'gt';
			newThreshold = 80;
			newTarget = '';
			newDuration = 0;
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'alr.failed_create'));
		}
	}

	async function toggleRule(rule: AlertRule) {
		try {
			await api.put(`/api/v1/alert-rules/${rule.id}`, { enabled: !rule.enabled });
			toast.success(translate($language, rule.enabled ? 'alr.toast_disabled' : 'alr.toast_enabled'));
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'alr.failed_toggle'));
		}
	}

	function startEdit(rule: AlertRule) {
		editingRule = rule;
		editMetric = rule.metric;
		editOperator = rule.operator;
		editThreshold = rule.threshold;
		editTarget = rule.target || '';
		editDuration = rule.duration_s || 0;
	}

	async function saveEdit() {
		if (!editingRule) return;
		try {
			await api.put(`/api/v1/alert-rules/${editingRule.id}`, {
				metric: editMetric,
				target: editMetric === 'service_down' || editMetric === 'ssl_expiry' ? editTarget : '',
				operator: editOperator,
				threshold: Number(editThreshold),
				duration_s: Number(editDuration),
				enabled: editingRule.enabled
			});
			toast.success(translate($language, 'alr.toast_updated'));
			editingRule = null;
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'alr.failed_update'));
		}
	}

	async function deleteRule(ruleId: string) {
		try {
			await api.del(`/api/v1/alert-rules/${ruleId}`);
			toast.success(translate($language, 'alr.toast_deleted'));
			await loadData();
		} catch (err) {
			toast.error(err instanceof Error ? err.message : translate($language, 'alr.failed_delete'));
		}
	}

	onMount(loadData);
</script>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'alr.title')}</h2>



	{#if loading}
		<div class="text-gray-400">{translate($language, 'alr.loading')}</div>
	{:else if error}
		<div class="p-4 bg-red-900/50 border border-red-700 rounded-lg text-red-300">{error}</div>
	{:else}
		<!-- Alert Rules -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'alr.rules_title')}</h3>

			{#if rules.length === 0}
				<div class="text-gray-400 text-sm mb-4">{translate($language, 'alr.no_rules')}</div>
			{:else}
				<div class="overflow-x-auto mb-4">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_metric')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_target')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_operator')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_threshold')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_duration')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_enabled')}</th>
								<th class="text-right px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_actions')}</th>
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
											{#if editMetric === 'service_down'}
												<select bind:value={editTarget} class="max-w-40 px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm"><option value="">{translate($language, 'alr.select_service')}</option>{#each services as service}<option value={service.name}>{service.name}</option>{/each}</select>
											{:else if editMetric === 'ssl_expiry'}
												<select bind:value={editTarget} class="max-w-44 px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm"><option value="">{translate($language, 'alr.select_domain')}</option>{#each certificates as certificate}<option value={certificate.domain}>{certificate.domain}</option>{/each}</select>
											{:else}<span class="text-gray-500">—</span>{/if}
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
										<td class="px-4 py-3"><input type="number" min="0" bind:value={editDuration} class="w-24 px-2 py-1 bg-gray-700 border border-gray-600 rounded text-white text-sm" /></td>
										<td class="px-4 py-3"></td>
										<td class="px-4 py-3 text-right">
											<div class="flex justify-end gap-2">
												<button onclick={saveEdit} class="px-2.5 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'alr.save')}</button>
												<button onclick={() => (editingRule = null)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'alr.cancel')}</button>
											</div>
										</td>
									{:else}
										<td class="px-4 py-3 text-sm text-white font-mono">{rule.metric}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{rule.target || '—'}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{operatorLabel(rule.operator)}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{rule.threshold}</td>
										<td class="px-4 py-3 text-sm text-gray-300 font-mono">{rule.duration_s}{translate($language, 'alr.unit_seconds')}</td>
										<td class="px-4 py-3">
											<button
												onclick={() => toggleRule(rule)}
												aria-label={translate($language, 'alr.toggle_aria')}
												class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors cursor-pointer {rule.enabled ? 'bg-blue-600' : 'bg-gray-600'}"
											>
												<span class="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform {rule.enabled ? 'translate-x-4.5' : 'translate-x-0.5'}"></span>
											</button>
										</td>
										<td class="px-4 py-3 text-right">
											<div class="flex justify-end gap-2">
												<button onclick={() => startEdit(rule)} class="px-2.5 py-1 bg-gray-600 hover:bg-gray-500 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'alr.edit')}</button>
												<button onclick={() => deleteRule(rule.id)} class="px-2.5 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors cursor-pointer">{translate($language, 'alr.delete')}</button>
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
					<label for="alert-metric" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.th_metric')}</label>
					<select id="alert-metric" bind:value={newMetric} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
						{#each metricOptions as m}
							<option value={m}>{m}</option>
						{/each}
					</select>
				</div>
				{#if newMetric === 'service_down'}
					<div><label for="alert-target-service" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.label_target_service')}</label><select id="alert-target-service" bind:value={newTarget} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm"><option value="">{translate($language, 'alr.select_service')}</option>{#each services as service}<option value={service.name}>{service.name}</option>{/each}</select></div>
				{:else if newMetric === 'ssl_expiry'}
					<div><label for="alert-target-domain" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.label_target_domain')}</label><select id="alert-target-domain" bind:value={newTarget} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm"><option value="">{translate($language, 'alr.select_domain')}</option>{#each certificates as certificate}<option value={certificate.domain}>{certificate.domain}</option>{/each}</select></div>
				{/if}
				<div>
					<label for="alert-operator" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.th_operator')}</label>
					<select id="alert-operator" bind:value={newOperator} class="px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
						{#each operatorOptions as op}
							<option value={op}>{operatorLabel(op)}</option>
						{/each}
					</select>
				</div>
				<div>
					<label for="alert-threshold" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.th_threshold')}</label>
					<input id="alert-threshold" type="number" bind:value={newThreshold} class="w-24 px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
				</div>
				<div>
					<label for="alert-duration" class="block text-xs text-gray-400 uppercase tracking-wider mb-1">{translate($language, 'alr.label_duration')}</label>
					<input id="alert-duration" type="number" min="0" bind:value={newDuration} class="w-28 px-3 py-2 bg-gray-700 border border-gray-600 rounded text-white text-sm" />
				</div>
				<button onclick={addRule} class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-medium rounded transition-colors cursor-pointer">
					{translate($language, 'alr.add')}
				</button>
			</div>
		</div>

		<!-- Alert History -->
		<div class="bg-gray-800 rounded-lg border border-gray-700 p-5">
			<h3 class="text-lg font-semibold text-white mb-3">{translate($language, 'alr.history_title')}</h3>

			{#if history.length === 0}
				<div class="text-gray-400 text-sm">{translate($language, 'alr.no_history')}</div>
			{:else}
				<div class="overflow-x-auto">
					<table class="w-full">
						<thead>
							<tr class="border-b border-gray-700">
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_metric')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_value')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_message')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_status')}</th>
								<th class="text-left px-4 py-3 text-xs text-gray-400 uppercase tracking-wider font-medium">{translate($language, 'alr.th_timestamp')}</th>
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
											{entry.resolved ? translate($language, 'alr.resolved') : translate($language, 'alr.active')}
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
