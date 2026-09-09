<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import LogoMark from '$lib/components/LogoMark.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { isAuthenticated, user, logout, checkAuth } from '$lib/stores/auth';
	import { language, translate } from '$lib/stores/language';
	import '../app.css';

	let { children } = $props();

	let loading = $state(true);
	let sidebarOpen = $state(true);
	let mobileSidebarOpen = $state(false);
	let isMobile = $state(false);
	let mobileMenuButton: HTMLButtonElement | undefined = $state();
	let mobileCloseButton: HTMLButtonElement | undefined = $state();
	let sidebarExpanded = $derived(sidebarOpen || mobileSidebarOpen);

	const navGroups = [
		{
			key: 'nav.group.overview',
			items: [
				{ href: '/dashboard', labelKey: 'nav.dashboard', icon: 'grid' },
			]
		},
		{
			key: 'nav.group.web',
			items: [
				{ href: '/websites', labelKey: 'nav.websites', icon: 'globe-alt' },
				{ href: '/php', labelKey: 'nav.php', icon: 'code' },
				{ href: '/nodejs', labelKey: 'nav.nodejs', icon: 'terminal' },
				{ href: '/ssl', labelKey: 'nav.ssl', icon: 'lock' },
				{ href: '/deployments', labelKey: 'nav.deploy', icon: 'upload' },
				{ href: '/cron', labelKey: 'nav.cron', icon: 'clock' },
				{ href: '/queue-workers', labelKey: 'nav.queue', icon: 'refresh' },
			]
		},
		{
			key: 'nav.group.infrastructure',
			items: [
				{ href: '/server', labelKey: 'nav.server', icon: 'server' },
				{ href: '/services', labelKey: 'nav.services', icon: 'layers' },
				{ href: '/nginx', labelKey: 'nav.nginx', icon: 'globe' },
				{ href: '/databases', labelKey: 'nav.databases', icon: 'database' },
				{ href: '/docker', labelKey: 'nav.docker', icon: 'cube' },
			]
		},
		{
			key: 'nav.group.security',
			items: [
				{ href: '/security', labelKey: 'nav.security_center', icon: 'shield' },
				{ href: '/firewall', labelKey: 'nav.firewall', icon: 'shield' },
				{ href: '/users', labelKey: 'nav.users', icon: 'users' },
				{ href: '/alerts', labelKey: 'nav.alerts', icon: 'bell' },
				{ href: '/notifications', labelKey: 'nav.notifications', icon: 'megaphone' },
			]
		},
		{
			key: 'nav.group.operations',
			items: [
				{ href: '/backups', labelKey: 'nav.backups', icon: 'archive' },
				{ href: '/processes', labelKey: 'nav.processes', icon: 'activity' },
				{ href: '/terminal', labelKey: 'nav.terminal', icon: 'command-line' },
			]
		},
		{
			key: 'nav.group.system',
			items: [
				{ href: '/update', labelKey: 'nav.update', icon: 'arrow-path' },
				{ href: '/audit-logs', labelKey: 'nav.audit', icon: 'file-text' },
				{ href: '/settings', labelKey: 'nav.settings', icon: 'settings' },
			]
		}
	];

	function isActive(href: string): boolean {
		return page.url.pathname === href || page.url.pathname.startsWith(href + '/');
	}

	async function handleLogout() {
		await logout();
		goto('/login');
	}

	async function openMobileSidebar() {
		mobileSidebarOpen = true;
		await tick();
		mobileCloseButton?.focus();
	}

	async function closeMobileSidebar() {
		mobileSidebarOpen = false;
		await tick();
		mobileMenuButton?.focus();
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && mobileSidebarOpen) {
			closeMobileSidebar();
		}
	}

	onMount(async () => {
		const authed = await checkAuth();
		loading = false;
		if (!authed && page.url.pathname !== '/login') {
			goto('/login');
		}
	});

	onMount(() => {
		const mediaQuery = window.matchMedia('(max-width: 1023px)');
		const updateViewport = () => {
			isMobile = mediaQuery.matches;
			if (!isMobile) mobileSidebarOpen = false;
		};

		updateViewport();
		mediaQuery.addEventListener('change', updateViewport);
		return () => mediaQuery.removeEventListener('change', updateViewport);
	});

	// Redirect when auth state changes
	$effect(() => {
		if (!loading && !$isAuthenticated && page.url.pathname !== '/login') {
			goto('/login');
		}
	});
</script>

<svelte:window onkeydown={handleKeydown} />

<svelte:head>
	<title>Jenderal Panel</title>
</svelte:head>

{#if loading}
	<div class="app-shell flex h-screen items-center justify-center">
		<div class="flex flex-col items-center gap-4 text-sm text-gray-400">
			<div class="h-8 w-8 rounded-full border-2 border-gray-700 border-t-blue-400 motion-safe:animate-spin"></div>
			Loading your workspace...
		</div>
	</div>
{:else if !$isAuthenticated}
	{@render children()}
{:else}
	<div class="app-shell flex h-screen text-gray-100">
		{#if mobileSidebarOpen}
			<button
				type="button"
				onclick={closeMobileSidebar}
				class="fixed inset-0 z-30 cursor-default bg-black/60 backdrop-blur-sm lg:hidden"
				aria-label="Close navigation"
				tabindex="-1"
			></button>
		{/if}

		<!-- Sidebar -->
		<aside
			id="primary-sidebar"
			class="sidebar-surface fixed inset-y-0 left-0 z-40 flex w-60 flex-col border-r border-white/5 transition-[transform,width] duration-200 lg:static lg:translate-x-0
			{mobileSidebarOpen ? 'translate-x-0' : '-translate-x-full'}
			{sidebarOpen ? 'lg:w-60' : 'lg:w-16'}"
			aria-label="Primary navigation"
			inert={isMobile && !mobileSidebarOpen ? true : undefined}
		>
			<div
				class="border-b border-white/5 {sidebarExpanded
					? 'flex items-center gap-2 px-4 py-4'
					: 'flex flex-col items-center gap-2 px-2 py-3'}"
			>
				<div class="rounded-xl border border-blue-400/15 bg-blue-500/8 p-1.5">
					<LogoMark size={sidebarExpanded ? 'md' : 'sm'} decorative />
				</div>
				{#if sidebarExpanded}
					<div class="min-w-0">
						<span class="block truncate text-base font-semibold tracking-tight text-white">Jenderal Panel</span>
						<span class="block text-[9px] font-semibold uppercase tracking-[0.18em] text-blue-400">Control center</span>
					</div>
				{/if}
				<button
					type="button"
					onclick={closeMobileSidebar}
					bind:this={mobileCloseButton}
					class="ml-auto cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-white/5 hover:text-white lg:hidden"
					aria-label="Close navigation"
				>
					<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
					</svg>
				</button>
				<button
					onclick={() => (sidebarOpen = !sidebarOpen)}
					class="{sidebarOpen ? 'ml-auto' : ''} hidden cursor-pointer rounded-lg p-1.5 text-gray-400 transition hover:bg-white/5 hover:text-white lg:block"
					aria-label={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'}
				>
					<svg
						class="w-5 h-5"
						fill="none"
						stroke="currentColor"
						viewBox="0 0 24 24"
						stroke-width="2"
					>
						{#if sidebarOpen}
							<path stroke-linecap="round" stroke-linejoin="round" d="M15 19l-7-7 7-7" />
						{:else}
							<path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
						{/if}
					</svg>
				</button>
			</div>

			<nav class="flex-1 overflow-y-auto px-2 py-2">
				{#each navGroups as group}
					{#if sidebarExpanded}
						<div class="px-3 pt-4 pb-1">
							<span class="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">
								{translate($language, group.key)}
							</span>
						</div>
					{:else}
						<div class="mx-2 border-t border-white/5 pb-1 pt-3"></div>
					{/if}
					{#each group.items as item}
						<a
							href={item.href}
							onclick={() => (mobileSidebarOpen = false)}
							class="flex items-center gap-3 rounded-xl px-3 py-2 text-sm transition-all
							{isActive(item.href)
								? 'bg-blue-500/15 text-blue-100 shadow-[inset_0_0_0_1px_rgba(45,212,191,0.16)]'
								: 'text-gray-400 hover:bg-white/5 hover:text-gray-100'}"
						>
							<svg class="w-5 h-5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
								{#if item.icon === 'globe-alt'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
								{:else if item.icon === 'code'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M17.25 6.75L22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3l-4.5 16.5" />
								{:else if item.icon === 'grid'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
								{:else if item.icon === 'server'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M21.75 17.25v-.228a4.5 4.5 0 00-.12-1.03l-2.268-9.64a3.375 3.375 0 00-3.285-2.602H7.923a3.375 3.375 0 00-3.285 2.602l-2.268 9.64a4.5 4.5 0 00-.12 1.03v.228m19.5 0a3 3 0 01-3 3H5.25a3 3 0 01-3-3m19.5 0a3 3 0 00-3-3H5.25a3 3 0 00-3 3m16.5 0h.008v.008h-.008v-.008zm-3 0h.008v.008h-.008v-.008z" />
								{:else if item.icon === 'layers'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M6.429 9.75L2.25 12l4.179 2.25m0-4.5l5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0l4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0l-5.571 3-5.571-3" />
								{:else if item.icon === 'globe'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
								{:else if item.icon === 'lock'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
								{:else if item.icon === 'shield'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
								{:else if item.icon === 'activity'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M3 12h4l3-9 4 18 3-9h4" />
								{:else if item.icon === 'users'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z" />
								{:else if item.icon === 'file-text'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z" />
								{:else if item.icon === 'upload'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5" />
								{:else if item.icon === 'clock'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
								{:else if item.icon === 'refresh'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992" />
								{:else if item.icon === 'terminal'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M6.75 7.5l3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0021 18V6a2.25 2.25 0 00-2.25-2.25H5.25A2.25 2.25 0 003 6v12a2.25 2.25 0 002.25 2.25z" />
								{:else if item.icon === 'database'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M20.25 6.375c0 2.278-3.694 4.125-8.25 4.125S3.75 8.653 3.75 6.375m16.5 0c0-2.278-3.694-4.125-8.25-4.125S3.75 4.097 3.75 6.375m16.5 0v11.25c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125V6.375m16.5 0v3.75c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125v-3.75m16.5 3.75v3.75c0 2.278-3.694 4.125-8.25 4.125s-8.25-1.847-8.25-4.125v-3.75" />
								{:else if item.icon === 'cube'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M21 7.5l-9-5.25L3 7.5m18 0l-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9" />
								{:else if item.icon === 'bell'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M14.857 17.082a23.848 23.848 0 005.454-1.31A8.967 8.967 0 0118 9.75v-.7V9A6 6 0 006 9v.75a8.967 8.967 0 01-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 01-5.714 0m5.714 0a3 3 0 11-5.714 0" />
								{:else if item.icon === 'megaphone'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M10.34 15.84c-.688-.06-1.386-.09-2.09-.09H7.5a4.5 4.5 0 110-9h.75c.704 0 1.402-.03 2.09-.09m0 9.18c.253.962.584 1.892.985 2.783.247.55.06 1.21-.463 1.511l-.657.38a.954.954 0 01-1.305-.39 13.493 13.493 0 01-1.167-3.453m2.607-5.831c1.96-.164 3.878-.504 5.733-1.003a7.28 7.28 0 00.413 2.571c.363 1.013.592 2.085.674 3.188a25.226 25.226 0 01-6.82-1.386m0-3.37a24.71 24.71 0 010 3.37m0-3.37c.18-1.663.178-3.37 0-5.036M12 3c3.28 0 6.39.713 9.185 1.992A1.18 1.18 0 0122 6.114v3.772a1.18 1.18 0 01-.815 1.122A27.418 27.418 0 0012 12.5" />
								{:else if item.icon === 'command-line'}
									<path stroke-linecap="round" stroke-linejoin="round" d="m6.75 7.5 3 2.25-3 2.25m4.5 0h3m-9 8.25h13.5A2.25 2.25 0 0 0 21 18V6a2.25 2.25 0 0 0-2.25-2.25H5.25A2.25 2.25 0 0 0 3 6v12a2.25 2.25 0 0 0 2.25 2.25z" />
								{:else if item.icon === 'arrow-path'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.992 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182M21.015 4.353v4.992" />
								{:else if item.icon === 'archive'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H2.25c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z" />
								{:else if item.icon === 'settings'}
									<path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 010 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 010-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281z" />
									<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
								{/if}
							</svg>
							<span class={sidebarExpanded ? '' : 'sr-only'}>{translate($language, item.labelKey)}</span>
						</a>
					{/each}
				{/each}
			</nav>

			<!-- User info at bottom -->
			{#if sidebarExpanded && $user}
				<div class="border-t border-white/5 px-3 py-3">
					<div class="flex items-center gap-3 rounded-xl bg-white/[0.025] p-2.5">
						<div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-blue-500/12 text-xs font-semibold uppercase text-blue-300">
							{$user.username.slice(0, 1)}
						</div>
						<div class="min-w-0">
							<div class="truncate text-sm font-medium text-gray-200">{$user.username}</div>
							<div class="text-[10px] uppercase tracking-wider text-gray-400">Administrator</div>
						</div>
					</div>
				</div>
			{/if}
		</aside>

		<!-- Main Content -->
		<div class="flex min-w-0 flex-1 flex-col overflow-hidden" inert={isMobile && mobileSidebarOpen ? true : undefined}>
			<!-- Top Bar -->
			<header
				class="topbar-surface flex items-center justify-between border-b border-white/5 px-4 py-3 sm:px-6"
			>
				<div class="flex items-center gap-3">
					<button
						type="button"
						onclick={openMobileSidebar}
						bind:this={mobileMenuButton}
						class="cursor-pointer rounded-lg border border-white/8 bg-white/[0.035] p-2 text-gray-300 transition hover:border-blue-400/20 hover:bg-blue-500/8 hover:text-white lg:hidden"
						aria-label="Open navigation"
						aria-expanded={mobileSidebarOpen}
						aria-controls="primary-sidebar"
					>
						<svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
						</svg>
					</button>
					<span class="h-2.5 w-2.5 rounded-full bg-blue-400 shadow-[0_0_10px_rgba(45,212,191,0.55)]"></span>
					<div>
						<h1 class="text-sm font-semibold text-white">Server workspace</h1>
						<p class="hidden text-[10px] uppercase tracking-[0.16em] text-gray-400 sm:block">Jenderal Panel</p>
					</div>
				</div>
				<div class="flex items-center gap-2">
					<ThemeToggle />
					<button
						onclick={handleLogout}
						class="cursor-pointer rounded-xl border border-white/8 bg-white/[0.035] px-3.5 py-2 text-xs font-medium text-gray-300 transition hover:border-blue-400/20 hover:bg-blue-500/8 hover:text-white"
					>
						Logout
					</button>
				</div>
			</header>

			<!-- Page Content -->
			<main class="flex-1 overflow-auto p-4 sm:p-6">
				{@render children()}
			</main>
		</div>
	</div>
{/if}
