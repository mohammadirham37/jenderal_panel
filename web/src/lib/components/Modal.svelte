<script lang="ts" module>
	// Open modals in stacking order; only the topmost one reacts to Escape.
	const stack: symbol[] = [];
</script>

<script lang="ts">
	import { onDestroy, type Snippet } from 'svelte';

	// Centered dialog with backdrop. Escape and backdrop clicks call onclose
	// unless `busy` is set, so in-flight requests cannot be orphaned.
	let {
		title,
		onclose,
		busy = false,
		size = 'md',
		closeLabel = 'Close',
		children,
		footer
	}: {
		title: string;
		onclose: () => void;
		busy?: boolean;
		size?: 'sm' | 'md' | 'lg';
		closeLabel?: string;
		children: Snippet;
		footer?: Snippet;
	} = $props();

	const widths = { sm: 'max-w-sm', md: 'max-w-md', lg: 'max-w-xl' };

	const token = Symbol('modal');
	stack.push(token);
	onDestroy(() => {
		const index = stack.indexOf(token);
		if (index >= 0) stack.splice(index, 1);
	});

	function close() {
		if (!busy) onclose();
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && stack[stack.length - 1] === token) close();
	}
</script>

<svelte:window {onkeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div
	class="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4 backdrop-blur-sm"
	role="dialog"
	aria-modal="true"
	aria-label={title}
	tabindex="-1"
	onclick={(e) => { if (e.target === e.currentTarget) close(); }}
>
	<div class="flex max-h-[90vh] w-full {widths[size]} flex-col rounded-2xl border border-gray-700 bg-gray-800 shadow-2xl">
		<div class="flex items-start justify-between gap-3 border-b border-gray-700 px-5 py-4">
			<h3 class="text-base font-semibold text-white">{title}</h3>
			<button
				type="button"
				onclick={close}
				disabled={busy}
				aria-label={closeLabel}
				class="cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-gray-700 hover:text-white disabled:opacity-40"
			>
				<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
				</svg>
			</button>
		</div>
		<div class="overflow-y-auto px-5 py-4">
			{@render children()}
		</div>
		{#if footer}
			<div class="flex items-center justify-end gap-2 border-t border-gray-700 px-5 py-3">
				{@render footer()}
			</div>
		{/if}
	</div>
</div>
