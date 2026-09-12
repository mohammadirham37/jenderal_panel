// Domain dictionary: see ../index.ts for how these merge into the global lookup.
// Dashboard page strings plus root-layout chrome (`nav.*`). Keys that already
// exist in core.ts (dash.title, dash.cpu, nav.dashboard, …) are NOT redefined
// here — the components reuse them directly.

const en = {
	'nav.loading_workspace': 'Loading your workspace...',
	'nav.server_workspace': 'Server workspace',
	'nav.logout': 'Logout',
	'nav.administrator': 'Administrator',
	'nav.control_center': 'Control center',
	'nav.open_nav': 'Open navigation',
	'nav.close_nav': 'Close navigation',
	'nav.collapse_sidebar': 'Collapse sidebar',
	'nav.expand_sidebar': 'Expand sidebar',
	'nav.primary_nav': 'Primary navigation',
	'dash.timezone': 'Timezone',
	'dash.chart.now': 'now'
} as const;

const id: Record<keyof typeof en, string> = {
	'nav.loading_workspace': 'Memuat ruang kerja Anda...',
	'nav.server_workspace': 'Ruang kerja server',
	'nav.logout': 'Keluar',
	'nav.administrator': 'Administrator',
	'nav.control_center': 'Pusat kendali',
	'nav.open_nav': 'Buka navigasi',
	'nav.close_nav': 'Tutup navigasi',
	'nav.collapse_sidebar': 'Ciutkan bilah sisi',
	'nav.expand_sidebar': 'Luaskan bilah sisi',
	'nav.primary_nav': 'Navigasi utama',
	'dash.timezone': 'Zona waktu',
	'dash.chart.now': 'sekarang'
};

export const dict = { en, id };
