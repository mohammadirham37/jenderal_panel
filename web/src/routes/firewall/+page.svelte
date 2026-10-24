<script lang="ts">
	import FirewallPanel from '#lib/components/FirewallPanel.svelte';
	import { language, translate } from '#lib/stores/language.js';
</script>


<div class="space-y-6">
	<h2 class="text-2xl font-bold text-white">{translate($language, 'fw.title')}</h2>
	<FirewallPanel />
</div>
