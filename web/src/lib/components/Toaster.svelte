<script lang="ts">
	import { toasts, dismissToast } from '$lib/stores/toast';
	import { language, translate } from '$lib/stores/language';
	import { fly } from 'svelte/transition';

	interface ToastStyle {
		iconPath: string;
		iconClass: string;
	}
	const styles: Record<string, ToastStyle> = {
		success: {
			iconPath: 'M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
			iconClass: 'bg-green-500/10 text-green-400'
		},
		error: {
			iconPath:
				'M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z',
			iconClass: 'bg-red-500/10 text-red-400'
		},
		info: {
			iconPath: 'M11.25 11.25l.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25z',
			iconClass: 'bg-blue-500/10 text-blue-400'
		}
	};
</script>

<div
	class="pointer-events-none fixed inset-x-4 top-4 z-[70] flex flex-col items-stretch gap-2 sm:inset-x-auto sm:right-6 sm:top-6 sm:items-end"
	aria-live="polite"
>
	{#each $toasts as t (t.id)}
		<div
			transition:fly={{ x: 24, duration: 180 }}
			role={t.type === 'error' ? 'alert' : 'status'}
			class="pointer-events-auto flex w-full max-w-sm items-start gap-2.5 rounded-xl border border-gray-700 bg-gray-800 p-3 shadow-2xl"
		>
			<span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg {styles[t.type].iconClass}">
				<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d={styles[t.type].iconPath} />
				</svg>
			</span>
			<p class="min-w-0 flex-1 break-words pt-0.5 text-sm text-gray-200">{t.message}</p>
			<button
				type="button"
				onclick={() => dismissToast(t.id)}
				class="-m-1 cursor-pointer rounded-md p-1 text-gray-500 transition hover:bg-gray-700 hover:text-gray-200"
				aria-label={translate($language, 'common.dismissNotification')}
			>
				<svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2" aria-hidden="true">
					<path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
				</svg>
			</button>
		</div>
	{/each}
</div>
