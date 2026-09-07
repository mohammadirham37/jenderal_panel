<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { isAuthenticated, user, logout, checkAuth } from '$lib/stores/auth';
	import '../app.css';

	let { children } = $props();

	let loading = $state(true);
	let sidebarOpen = $state(true);

	const navItems = [
		{ href: '/dashboard', label: 'Dashboard', icon: 'grid' },
		{ href: '/server', label: 'Server', icon: 'server' },
		{ href: '/services', label: 'Services', icon: 'layers' },
		{ href: '/nginx', label: 'Nginx', icon: 'globe' },
		{ href: '/firewall', label: 'Firewall', icon: 'shield' },
		{ href: '/processes', label: 'Processes', icon: 'activity' },
		{ href: '/users', label: 'Users', icon: 'users' },
		{ href: '/audit-logs', label: 'Audit Logs', icon: 'file-text' },
		{ href: '/settings', label: 'Settings', icon: 'settings' }
	];

	function isActive(href: string): boolean {
		return page.url.pathname === href || page.url.pathname.startsWith(href + '/');
	}

	async function handleLogout() {
		await logout();
		goto('/login');
	}

	onMount(async () => {
		const authed = await checkAuth();
		loading = false;
		if (!authed && page.url.pathname !== '/login') {
			goto('/login');
		}
	});

	// Redirect when auth state changes
	$effect(() => {
		if (!loading && !$isAuthenticated && page.url.pathname !== '/login') {
			goto('/login');
		}
	});
</script>

<svelte:head>
	<title>Jenderal Panel</title>
</svelte:head>

{#if loading}
	<div class="flex h-screen items-center justify-center bg-gray-900">
		<div class="text-gray-400 text-lg">Loading...</div>
	</div>
{:else if !$isAuthenticated}
	{@render children()}
{:else}
	<div class="flex h-screen bg-gray-900 text-gray-100">
		<!-- Sidebar -->
		<aside
			class="flex flex-col bg-gray-800 border-r border-gray-700 transition-all duration-200 {sidebarOpen
				? 'w-60'
				: 'w-16'}"
		>
			<div class="flex items-center gap-2 px-4 py-4 border-b border-gray-700">
				{#if sidebarOpen}
					<span class="text-lg font-bold text-white tracking-tight">Jenderal</span>
				{/if}
				<button
					onclick={() => (sidebarOpen = !sidebarOpen)}
					class="ml-auto p-1 rounded hover:bg-gray-700 text-gray-400 hover:text-white cursor-pointer"
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

			<nav class="flex-1 py-4 space-y-1 px-2">
				{#each navItems as item}
					<a
						href={item.href}
						class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors
						{isActive(item.href)
							? 'bg-blue-600 text-white'
							: 'text-gray-400 hover:bg-gray-700 hover:text-white'}"
					>
						<svg class="w-5 h-5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
							{#if item.icon === 'grid'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
							{:else if item.icon === 'server'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M21.75 17.25v-.228a4.5 4.5 0 00-.12-1.03l-2.268-9.64a3.375 3.375 0 00-3.285-2.602H7.923a3.375 3.375 0 00-3.285 2.602l-2.268 9.64a4.5 4.5 0 00-.12 1.03v.228m19.5 0a3 3 0 01-3 3H5.25a3 3 0 01-3-3m19.5 0a3 3 0 00-3-3H5.25a3 3 0 00-3 3m16.5 0h.008v.008h-.008v-.008zm-3 0h.008v.008h-.008v-.008z" />
							{:else if item.icon === 'layers'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M6.429 9.75L2.25 12l4.179 2.25m0-4.5l5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0l4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0l-5.571 3-5.571-3" />
							{:else if item.icon === 'globe'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
							{:else if item.icon === 'shield'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
							{:else if item.icon === 'activity'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M3 12h4l3-9 4 18 3-9h4" />
							{:else if item.icon === 'users'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 002.625.372 9.337 9.337 0 004.121-.952 4.125 4.125 0 00-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 018.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0111.964-3.07M12 6.375a3.375 3.375 0 11-6.75 0 3.375 3.375 0 016.75 0zm8.25 2.25a2.625 2.625 0 11-5.25 0 2.625 2.625 0 015.25 0z" />
							{:else if item.icon === 'file-text'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z" />
							{:else if item.icon === 'settings'}
								<path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 010 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 010-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281z" />
								<path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
							{/if}
						</svg>
						{#if sidebarOpen}
							<span>{item.label}</span>
						{/if}
					</a>
				{/each}
			</nav>

			<!-- User info at bottom -->
			{#if sidebarOpen && $user}
				<div class="border-t border-gray-700 px-4 py-3">
					<div class="text-sm text-gray-400 truncate">{$user.username}</div>
				</div>
			{/if}
		</aside>

		<!-- Main Content -->
		<div class="flex-1 flex flex-col overflow-hidden">
			<!-- Top Bar -->
			<header
				class="flex items-center justify-between px-6 py-3 bg-gray-800 border-b border-gray-700"
			>
				<h1 class="text-lg font-semibold text-white">Jenderal Panel</h1>
				<button
					onclick={handleLogout}
					class="px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-gray-300 hover:text-white rounded transition-colors cursor-pointer"
				>
					Logout
				</button>
			</header>

			<!-- Page Content -->
			<main class="flex-1 overflow-auto p-6">
				{@render children()}
			</main>
		</div>
	</div>
{/if}
