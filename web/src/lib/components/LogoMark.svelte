<script lang="ts">
	const sizeClasses = {
		sm: 'h-7 w-7',
		md: 'h-9 w-9',
		lg: 'h-20 w-20'
	} as const;

	type LogoSize = keyof typeof sizeClasses;

	let { size = 'md', decorative = false }: { size?: LogoSize; decorative?: boolean } = $props();
</script>

<img
	src="/jenderal-panel-logo.png"
	alt={decorative ? '' : 'Jenderal Panel'}
	class="{sizeClasses[size]} shrink-0 object-contain"
/>
