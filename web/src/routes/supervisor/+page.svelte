<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { language, translate } from '$lib/stores/language';
	import { toast } from '$lib/stores/toast';

	interface Process {
		id: string;
		name: string;
		command: string;
		working_dir: string;
		run_as: string;
		env: string;
		auto_restart: boolean;
		status: string;
	}

	interface ProcessForm {
		name: string;
		command: string;
		working_dir: string;
		run_as: string;
		env: string;
		auto_restart: boolean;
	}

	let processes = $state<Process[]>([]);
	let loading = $state(true);
	let error = $state('');

	let showForm = $state(false);
	let editingId = $state('');
	let saving = $state(false);
	let formError = $state('');
	let form: ProcessForm = $state({ name: '', command: '', working_dir: '', run_as: '', env: '', auto_restart: true });

	let busyId = $state('');
	let actionError = $state('');
	let deleteConfirmId = $state('');

	let logsFor = $state('');
	let logs = $state('');
	let logsLoading = $state(false);

	const t = (key: string) => translate($language, key);

	async function load() {
		error = '';
		try {
			processes = await api.get<Process[]>('/api/v1/supervisor');
		} catch (e) {
			error = e instanceof Error ? e.message : t('supp.loadFailed');
		} finally {
			loading = false;
		}
	}

	function openCreate() {
		editingId = '';
		form = { name: '', command: '', working_dir: '', run_as: '', env: '', auto_restart: true };
		formError = '';
		showForm = true;
	}

	function openEdit(p: Process) {
		editingId = p.id;
		form = { name: p.name, command: p.command, working_dir: p.working_dir, run_as: p.run_as, env: p.env, auto_restart: p.auto_restart };
		formError = '';
		showForm = true;
	}

	async function save() {
		formError = '';
		saving = true;
		try {
			if (editingId) {
				await api.put(`/api/v1/supervisor/${editingId}`, form);
				toast.success(t('supp.toastUpdated'));
			} else {
				await api.post('/api/v1/supervisor', form);
				toast.success(t('supp.toastCreated'));
			}
			showForm = false;
			await load();
		} catch (e) {
			formError = e instanceof Error ? e.message : t('supp.errorAction');
		} finally {
			saving = false;
		}
	}

	async function action(p: Process, act: string) {
		actionError = '';
		busyId = p.id;
		try {
			await api.post(`/api/v1/supervisor/${p.id}/${act}`);
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('supp.errorAction');
		} finally {
			busyId = '';
		}
	}

	async function remove(p: Process) {
		if (!confirm(t('supp.deleteConfirm'))) return;
		actionError = '';
		busyId = p.id;
		try {
			await api.del(`/api/v1/supervisor/${p.id}`);
			toast.success(t('supp.toastDeleted'));
			if (logsFor === p.id) {
				logsFor = '';
				logs = '';
			}
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : t('supp.errorAction');
		} finally {
			busyId = '';
		}
	}

	async function loadLogs(p: Process) {
		if (logsFor === p.id) {
			logsFor = '';
			logs = '';
			return;
		}
		logsFor = p.id;
		logsLoading = true;
		try {
			const res = await api.get<{ logs: string }>(`/api/v1/supervisor/${p.id}/logs?lines=100`);
			logs = res.logs ?? '';
		} catch {
			logs = '';
		} finally {
			logsLoading = false;
		}
	}

	function stateBadgeClass(state: string): string {
		if (state === 'active') return 'bg-green-500/15 text-green-300 border-green-500/30';
		if (state === 'failed') return 'bg-red-500/15 text-red-300 border-red-500/30';
		return 'bg-gray-500/15 text-gray-300 border-gray-500/30';
	}

	onMount(() => {
		load();
	});
</script>

<div class="mx-auto max-w-6xl px-4 py-8 space-y-6">
	<header class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<h1 class="text-2xl font-semibold text-gray-100">{t('supp.title')}</h1>
			<p class="mt-1 text-sm text-gray-400">{t('supp.subtitle')}</p>
		</div>
		<button class="rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500" onclick={showForm ? () => (showForm = false) : openCreate}>
			{showForm ? t('supp.cancel') : t('supp.add')}
		</button>
	</header>

	{#if error}
		<div class="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{error}</div>
	{/if}
	{#if actionError}
		<div class="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{actionError}</div>
	{/if}

	{#if showForm}
		<section class="rounded-2xl border border-white/5 bg-gray-800/60 p-5">
			<h2 class="text-base font-semibold text-gray-100">{editingId ? t('supp.form.titleEdit') : t('supp.form.title')}</h2>
			{#if formError}
				<div class="mt-3 rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">{formError}</div>
			{/if}
			<div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
				<div>
					<label for="proc-name" class="mb-1 block text-xs font-medium uppercase tracking-wider text-gray-400">{t('supp.form.name')}</label>
					<input id="proc-name" type="text" bind:value={form.name} placeholder="worker-1" class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-xs text-gray-500">{t('supp.form.nameHint')}</p>
				</div>
				<div>
					<label for="proc-runas" class="mb-1 block text-xs font-medium uppercase tracking-wider text-gray-400">{t('supp.form.runAs')}</label>
					<input id="proc-runas" type="text" bind:value={form.run_as} placeholder="www-data" class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-xs text-gray-500">{t('supp.form.runAsHint')}</p>
				</div>
				<div class="md:col-span-2">
					<label for="proc-command" class="mb-1 block text-xs font-medium uppercase tracking-wider text-gray-400">{t('supp.form.command')}</label>
					<input id="proc-command" type="text" bind:value={form.command} placeholder="node /opt/app/server.js" class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 font-mono text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-xs text-gray-500">{t('supp.form.commandHint')}</p>
				</div>
				<div>
					<label for="proc-dir" class="mb-1 block text-xs font-medium uppercase tracking-wider text-gray-400">{t('supp.form.workingDir')}</label>
					<input id="proc-dir" type="text" bind:value={form.working_dir} placeholder="/opt/app" class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none" />
					<p class="mt-1 text-xs text-gray-500">{t('supp.form.workingDirHint')}</p>
				</div>
				<div>
					<label for="proc-env" class="mb-1 block text-xs font-medium uppercase tracking-wider text-gray-400">{t('supp.form.env')}</label>
					<textarea id="proc-env" rows="3" bind:value={form.env} placeholder={'PORT=3000\nLOG_LEVEL=info'} class="w-full rounded-xl border border-gray-700 bg-gray-950 px-3 py-2 font-mono text-sm text-gray-100 placeholder:text-gray-600 focus:border-blue-500 focus:outline-none"></textarea>
					<p class="mt-1 text-xs text-gray-500">{t('supp.form.envHint')}</p>
				</div>
				<label class="flex items-center gap-2 text-sm text-gray-300">
					<input type="checkbox" bind:checked={form.auto_restart} class="h-4 w-4 rounded border-gray-600 bg-gray-900" />
					{t('supp.form.autoRestart')}
				</label>
			</div>
			<button class="mt-4 rounded-xl bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50" disabled={saving} onclick={save}>
				{saving ? t('supp.form.saving') : editingId ? t('supp.form.saveEdit') : t('supp.form.save')}
			</button>
		</section>
	{/if}

	{#if loading}
		<div class="h-24 animate-pulse rounded-2xl border border-white/5 bg-gray-800/60"></div>
	{:else if processes.length === 0}
		<div class="rounded-2xl border border-white/5 bg-gray-800/60 p-8 text-center text-sm text-gray-400">{t('supp.empty')}</div>
	{:else}
		<section class="overflow-hidden rounded-2xl border border-white/5 bg-gray-800/60">
			<div class="overflow-x-auto">
				<table class="w-full text-left text-sm">
					<thead class="border-b border-white/5 text-xs font-semibold uppercase tracking-[0.14em] text-gray-400">
						<tr>
							<th class="px-4 py-3">{t('supp.table.name')}</th>
							<th class="px-4 py-3">{t('supp.table.command')}</th>
							<th class="px-4 py-3">{t('supp.table.user')}</th>
							<th class="px-4 py-3">{t('supp.table.status')}</th>
							<th class="px-4 py-3 text-right">{t('supp.table.actions')}</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-white/5">
						{#each processes as p (p.id)}
							<tr class="align-top">
								<td class="px-4 py-3 font-medium text-gray-100">{p.name}</td>
								<td class="px-4 py-3 font-mono text-xs text-gray-300">{p.command}</td>
								<td class="px-4 py-3 text-gray-400">{p.run_as}</td>
								<td class="px-4 py-3">
									<span class="inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-semibold {stateBadgeClass(p.status)}">
										{t('supp.state.' + p.status)}
									</span>
								</td>
								<td class="px-4 py-3">
									<div class="flex flex-wrap justify-end gap-1.5">
										<button class="rounded-lg border border-gray-700 px-2.5 py-1 text-xs text-gray-200 hover:bg-white/5 disabled:opacity-50" disabled={busyId === p.id} onclick={() => action(p, p.status === 'active' ? 'restart' : 'start')}>
											{p.status === 'active' ? t('supp.action.restart') : t('supp.action.start')}
										</button>
										<button class="rounded-lg border border-gray-700 px-2.5 py-1 text-xs text-gray-200 hover:bg-white/5 disabled:opacity-50" disabled={busyId === p.id || p.status !== 'active'} onclick={() => action(p, 'stop')}>
											{t('supp.action.stop')}
										</button>
										<button class="rounded-lg border border-gray-700 px-2.5 py-1 text-xs text-gray-200 hover:bg-white/5" onclick={() => loadLogs(p)}>
											{t('supp.action.logs')}
										</button>
										<button class="rounded-lg border border-gray-700 px-2.5 py-1 text-xs text-gray-200 hover:bg-white/5" onclick={() => openEdit(p)}>
											{t('supp.edit')}
										</button>
										{#if deleteConfirmId === p.id}
											<button class="rounded-lg bg-red-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-red-500" disabled={busyId === p.id} onclick={() => remove(p)}>
												{t('supp.action.delete')}?
											</button>
										{:else}
											<button class="rounded-lg border border-red-500/40 px-2.5 py-1 text-xs text-red-300 hover:bg-red-500/10" onclick={() => (deleteConfirmId = p.id)}>
												{t('supp.action.delete')}
											</button>
										{/if}
									</div>
								</td>
							</tr>
							{#if logsFor === p.id}
								<tr>
									<td colspan="5" class="px-4 pb-4">
										<div class="flex items-center justify-between rounded-xl bg-gray-950/60 px-3 py-2">
											<span class="text-xs text-gray-400">{t('supp.logs.title')}</span>
											<button class="text-xs text-gray-300 hover:text-gray-100" disabled={logsLoading} onclick={() => loadLogs(p)}>{t('supp.logs.refresh')}</button>
										</div>
										<pre class="mt-2 max-h-72 overflow-auto rounded-xl bg-gray-950 p-3 text-xs leading-relaxed text-gray-300">{logs || t('supp.logs.empty')}</pre>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}
</div>
