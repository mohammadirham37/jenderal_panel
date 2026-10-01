// Domain dictionary: see ../index.ts for how these merge into the global lookup.
// Dashboard page strings plus root-layout chrome (`nav.*`). Keys that already
// exist in core.ts (dash.title, dash.cpu, nav.dashboard, …) are NOT redefined
// here — the components reuse them directly.

const en = {
	'nav.loading_workspace': 'Loading your workspace...',
	'nav.server_workspace': 'Server workspace',
	'nav.logout': 'Logout',
	'nav.logout_title': 'Log out?',
	'nav.logout_body': 'You will need to sign in again to access the workspace.',
	'nav.logout_yes': 'Yes, log out',
	'nav.cancel': 'Cancel',
	'nav.administrator': 'Administrator',
	'nav.control_center': 'Control center',
	'nav.open_nav': 'Open navigation',
	'nav.close_nav': 'Close navigation',
	'nav.collapse_sidebar': 'Collapse sidebar',
	'nav.expand_sidebar': 'Expand sidebar',
	'nav.primary_nav': 'Primary navigation',
	'nav.impersonating_as': 'Viewing the panel as {user}',
	'nav.impersonation_by': '· admin session by {admin}',
	'nav.switch_back': 'Switch back',
	'nav.switching_back': 'Switching back…',
	'nav.switch_back_ok': 'Welcome back, {admin}.',
	'nav.switch_back_failed': 'Could not switch back — the admin session may have ended. Log in again.',
	'dash.timezone': 'Timezone',
	'dash.chart.now': 'now'
} as const;

const id: Record<keyof typeof en, string> = {
	'nav.loading_workspace': 'Memuat ruang kerja Anda...',
	'nav.server_workspace': 'Ruang kerja server',
	'nav.logout': 'Keluar',
	'nav.logout_title': 'Keluar dari panel?',
	'nav.logout_body': 'Anda perlu masuk kembali untuk mengakses ruang kerja.',
	'nav.logout_yes': 'Ya, keluar',
	'nav.cancel': 'Batal',
	'nav.administrator': 'Administrator',
	'nav.control_center': 'Pusat kendali',
	'nav.open_nav': 'Buka navigasi',
	'nav.close_nav': 'Tutup navigasi',
	'nav.collapse_sidebar': 'Ciutkan bilah sisi',
	'nav.expand_sidebar': 'Luaskan bilah sisi',
	'nav.primary_nav': 'Navigasi utama',
	'nav.impersonating_as': 'Melihat panel sebagai {user}',
	'nav.impersonation_by': '· sesi admin oleh {admin}',
	'nav.switch_back': 'Kembali ke admin',
	'nav.switching_back': 'Kembali ke admin…',
	'nav.switch_back_ok': 'Selamat datang kembali, {admin}.',
	'nav.switch_back_failed': 'Tidak bisa kembali — sesi admin mungkin sudah berakhir. Silakan login lagi.',
	'dash.timezone': 'Zona waktu',
	'dash.chart.now': 'sekarang'
};

export const dict = { en, id };
