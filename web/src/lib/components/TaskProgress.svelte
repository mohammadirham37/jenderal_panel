<script lang="ts">
	import { api } from '$lib/api';

	let { taskId = $bindable(''), onComplete = () => {} }: { taskId: string; onComplete?: () => void } = $props();

	let task = $state<any>(null);
	let polling = $state(false);
	let intervalId: ReturnType<typeof setInterval> | null = null;

	$effect(() => {
		if (taskId && !polling) {
			polling = true;
			pollTask();
			intervalId = setInterval(pollTask, 2000);
		}
		return () => {
			if (intervalId) clearInterval(intervalId);
		};
	});

	async function pollTask() {
		if (!taskId) return;
		try {
			task = await api.get(`/api/v1/tasks/${taskId}`);
			if (task.status === 'completed' || task.status === 'failed') {
				if (intervalId) clearInterval(intervalId);
				polling = false;
				onComplete();
			}
		} catch {
			if (intervalId) clearInterval(intervalId);
			polling = false;
		}
	}
</script>

{#if task}
	<div class="mt-4 rounded-lg border border-gray-700 bg-gray-950 p-4">
		<div class="flex items-center justify-between mb-2">
			<span class="text-sm font-medium text-gray-300">{task.name}</span>
			{#if task.status === 'running'}
				<span class="inline-flex items-center gap-1 text-xs bg-yellow-900 text-yellow-300 px-2 py-0.5 rounded">
					<span class="w-2 h-2 bg-yellow-400 rounded-full animate-pulse"></span>
					Running
				</span>
			{:else if task.status === 'completed'}
				<span class="text-xs bg-green-900 text-green-300 px-2 py-0.5 rounded">Completed</span>
			{:else if task.status === 'failed'}
				<span class="text-xs bg-red-900 text-red-300 px-2 py-0.5 rounded">Failed</span>
			{/if}
		</div>
		<pre class="text-xs text-gray-400 bg-gray-900 rounded p-3 max-h-64 overflow-y-auto whitespace-pre-wrap font-mono">{task.output || 'Waiting for output...'}</pre>
		{#if task.error}
			<p class="mt-2 text-sm text-red-400">{task.error}</p>
		{/if}
	</div>
{/if}
