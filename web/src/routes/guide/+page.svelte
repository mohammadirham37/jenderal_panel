<script lang="ts">
	import { language, translate } from '$lib/stores/language';
	import { guideContent, sectionAnchor, subAnchor, type GuideBlock } from '$lib/content/guide';

	let scrollY = $state(0);
	let mobileTocOpen = $state(false);

	const doc = $derived(guideContent[$language]);

	const anchors = $derived(
		doc.sections.flatMap((s) => [
			sectionAnchor(s.id),
			...(s.subsections ?? []).map((sub) => subAnchor(s.id, sub.id))
		])
	);

	const activeAnchor = $derived.by(() => {
		void scrollY;
		let current = anchors[0] ?? '';
		if (typeof document === 'undefined') return current;
		for (const id of anchors) {
			const el = document.getElementById(id);
			if (el && el.getBoundingClientRect().top <= 120) current = id;
		}
		return current;
	});

	function scrollTo(anchor: string, event: MouseEvent) {
		event.preventDefault();
		document.getElementById(anchor)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
		history.replaceState(null, '', `#${anchor}`);
		mobileTocOpen = false;
	}

	/** Splits text on backticks so `segments` render as inline code. */
	function inlineSegments(text: string): { code: boolean; text: string }[] {
		return text.split('`').map((part, i) => ({ code: i % 2 === 1, text: part }));
	}
</script>

<svelte:window bind:scrollY />

{#snippet inlineText(text: string)}
	{#each inlineSegments(text) as seg}{#if seg.code}<code
				class="rounded bg-gray-700/60 px-1.5 py-0.5 font-mono text-[0.85em] text-blue-200">{seg.text}</code
			>{:else}{seg.text}{/if}{/each}
{/snippet}

{#snippet block(b: GuideBlock)}
	{#if b.type === 'p'}
		<p class="text-sm leading-relaxed text-gray-300">{@render inlineText(b.text)}</p>
	{:else if b.type === 'list'}
		<ul class="list-disc space-y-1.5 pl-5 text-sm leading-relaxed text-gray-300">
			{#each b.items as item}<li>{@render inlineText(item)}</li>{/each}
		</ul>
	{:else if b.type === 'steps'}
		<ol class="list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-gray-300">
			{#each b.items as item}<li>{@render inlineText(item)}</li>{/each}
		</ol>
	{:else if b.type === 'tip'}
		<div class="flex gap-3 rounded-xl border border-blue-400/25 bg-blue-500/10 p-4">
			<svg class="mt-0.5 h-5 w-5 shrink-0 text-blue-300" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M12 18v-5.25m0 0a6.01 6.01 0 001.5-.189m-1.5.189a6.01 6.01 0 01-1.5-.189m3.75 7.478a12.06 12.06 0 01-4.5 0m3.75 2.383a14.406 14.406 0 01-3 0M14.25 18v-.192c0-.983.658-1.823 1.508-2.316a7.5 7.5 0 10-7.517 0c.85.493 1.509 1.333 1.509 2.316V18" />
			</svg>
			<p class="text-sm leading-relaxed text-blue-100"><span class="font-semibold">{translate($language, 'guide.tip')}: </span>{@render inlineText(b.text)}</p>
		</div>
	{:else if b.type === 'warning'}
		<div class="flex gap-3 rounded-xl border border-red-400/25 bg-red-500/10 p-4">
			<svg class="mt-0.5 h-5 w-5 shrink-0 text-red-300" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5" aria-hidden="true">
				<path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
			</svg>
			<p class="text-sm leading-relaxed text-red-100"><span class="font-semibold">{translate($language, 'guide.warning')}: </span>{@render inlineText(b.text)}</p>
		</div>
	{:else if b.type === 'table'}
		<div class="overflow-x-auto rounded-xl border border-gray-700">
			<table class="w-full text-left text-sm">
				<thead class="bg-gray-700/40 text-xs uppercase tracking-wider text-gray-400">
					<tr>
						{#each b.headers as h}<th class="px-4 py-2.5 font-semibold">{h}</th>{/each}
					</tr>
				</thead>
				<tbody class="divide-y divide-gray-700/60">
					{#each b.rows as row}
						<tr>
							{#each row as cell, ci}<td class="px-4 py-2.5 leading-relaxed {ci === 0 ? 'font-medium text-gray-200' : 'text-gray-300'}">{@render inlineText(cell)}</td>{/each}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if b.type === 'terms'}
		<dl class="space-y-3">
			{#each b.items as item}
				<div class="rounded-xl border border-gray-700 bg-gray-700/20 p-4">
					<dt class="text-sm font-semibold text-gray-100">{@render inlineText(item.term)}</dt>
					<dd class="mt-1 text-sm leading-relaxed text-gray-300">{@render inlineText(item.def)}</dd>
				</div>
			{/each}
		</dl>
	{/if}
{/snippet}

{#snippet tocList(ulClass: string)}
	<ul class={ulClass}>
		{#each doc.sections as section (section.id)}
			<li>
				<a
					href="#{sectionAnchor(section.id)}"
					onclick={(e) => scrollTo(sectionAnchor(section.id), e)}
					class="block rounded-lg px-3 py-1.5 text-sm transition {activeAnchor === sectionAnchor(section.id)
						? 'bg-blue-500/15 font-medium text-blue-200'
						: 'text-gray-400 hover:bg-white/5 hover:text-gray-100'}"
				>
					{section.title}
				</a>
				{#if section.subsections?.length}
					<ul class="ml-3 border-l border-gray-700/70 pl-1">
						{#each section.subsections as sub (sub.id)}
							<li>
								<a
									href="#{subAnchor(section.id, sub.id)}"
									onclick={(e) => scrollTo(subAnchor(section.id, sub.id), e)}
									class="block rounded-lg px-3 py-1 text-xs transition {activeAnchor === subAnchor(section.id, sub.id)
										? 'bg-blue-500/15 font-medium text-blue-200'
										: 'text-gray-400 hover:bg-white/5 hover:text-gray-100'}"
								>
									{sub.title}
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</li>
		{/each}
	</ul>
{/snippet}

<div class="mx-auto max-w-6xl space-y-6">
	<div>
		<h2 class="text-2xl font-bold text-white">{doc.title}</h2>
		<p class="mt-1 text-sm text-gray-400">{doc.subtitle}</p>
	</div>

	<!-- Mobile table of contents -->
	<details class="rounded-xl border border-gray-700 bg-gray-800 lg:hidden" bind:open={mobileTocOpen}>
		<summary class="cursor-pointer select-none px-4 py-3 text-sm font-semibold text-gray-200">
			{translate($language, 'guide.contents')}
		</summary>
		<div class="border-t border-gray-700 px-2 py-2">
			{@render tocList('space-y-0.5')}
		</div>
	</details>

	<div class="lg:grid lg:grid-cols-[250px_1fr] lg:gap-8">
		<!-- Desktop table of contents -->
		<aside class="hidden lg:block">
			<nav class="sticky top-6 max-h-[calc(100vh-3rem)] overflow-y-auto rounded-xl border border-gray-700 bg-gray-800 p-2" aria-label={translate($language, 'guide.contents')}>
				{@render tocList('space-y-0.5')}
			</nav>
		</aside>

		<div class="min-w-0 space-y-10">
			{#each doc.sections as section (section.id)}
				<section id={sectionAnchor(section.id)} class="scroll-mt-24 space-y-4">
					<h3 class="border-b border-gray-700/70 pb-2 text-xl font-semibold text-white">{section.title}</h3>
					{#each section.blocks as b}<div class="space-y-3">{@render block(b)}</div>{/each}

					{#if section.subsections?.length}
						<div class="space-y-6 border-l-2 border-gray-700/60 pl-5">
							{#each section.subsections as sub (sub.id)}
								<div id={subAnchor(section.id, sub.id)} class="scroll-mt-24 space-y-3">
									<h4 class="text-lg font-semibold text-gray-100">{sub.title}</h4>
									{#each sub.blocks as b}<div class="space-y-3">{@render block(b)}</div>{/each}
								</div>
							{/each}
						</div>
					{/if}
				</section>
			{/each}
		</div>
	</div>
</div>
