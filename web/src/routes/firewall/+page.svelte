<script lang="ts">
	import FirewallPanel from '$lib/components/FirewallPanel.svelte';
	import { language, translate } from '$lib/stores/language';
</script>

<svelte:head><title>{translate($language, 'fw.title')} · Jenderal Panel</title></svelte:head>

<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'fw.title')}</h2>
	<FirewallPanel />
</div>
