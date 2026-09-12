// Domain dictionary: see ../index.ts for how these merge into the global lookup.
// Owns the `common.` key prefix: shared components (TaskProgress, LogViewer,
// TerminalConsole, Toaster, ThemeToggle) and global libs (api.ts).

const en = {
	'common.loading': 'Loading…',
	'common.refresh': 'Refresh',
	'common.clear': 'Clear',
	'common.send': 'Send',
	'common.dismiss': 'Dismiss',
	'common.dismissNotification': 'Dismiss notification',
	// Task progress (TaskProgress.svelte)
	'common.backgroundTask': 'Background task',
	'common.reconnecting': 'Reconnecting',
	'common.running': 'Running',
	'common.completed': 'Completed',
	'common.failed': 'Failed',
	'common.waitingToReconnect': 'Waiting to reconnect...',
	'common.waitingForOutput': 'Waiting for output...',
	'common.unableToRefreshTask': 'Unable to refresh task progress',
	'common.connectionInterrupted': 'Connection interrupted: {error}. Retrying...',
	// Log viewer (LogViewer.svelte)
	'common.failedToLoadLogs': 'Failed to load logs',
	'common.wsError': 'WebSocket connection error',
	'common.nLines': '{n} lines',
	'common.stream': 'Stream',
	'common.stopStream': 'Stop Stream',
	// Terminal console (TerminalConsole.svelte)
	'common.connecting': 'Connecting...',
	'common.connected': 'Connected',
	'common.disconnected': 'Disconnected',
	'common.notConnected': 'Not connected',
	'common.reconnect': 'Reconnect',
	'common.typeCommand': 'Type a command…',
	'common.terminal.connectedBanner': '--- Connected to persistent shell ---',
	'common.terminal.disconnectedBanner': '--- Disconnected ---',
	'common.terminal.errorBanner': '--- Connection error ---',
	'common.terminal.exitCode': '[exit {code}]',
	'common.terminal.hints':
		'Shell state (cd, export) persists while connected · ↑/↓ history · Ctrl+L clear · paste multi-line to run it as one command',
	// Theme toggle (ThemeToggle.svelte)
	'common.switchToLight': 'Switch to light mode',
	'common.switchToDark': 'Switch to dark mode',
	'common.lightMode': 'Light mode',
	'common.darkMode': 'Dark mode',
	// Global API errors (api.ts)
	'common.api.timeout':
		'Server did not respond within {seconds} seconds. Restart it over SSH: sudo /opt/jenderal/jenderal restart'
} as const;

const id: Record<keyof typeof en, string> = {
	'common.loading': 'Memuat…',
	'common.refresh': 'Muat ulang',
	'common.clear': 'Bersihkan',
	'common.send': 'Kirim',
	'common.dismiss': 'Tutup',
	'common.dismissNotification': 'Tutup notifikasi',
	// Task progress (TaskProgress.svelte)
	'common.backgroundTask': 'Tugas latar belakang',
	'common.reconnecting': 'Menyambung ulang…',
	'common.running': 'Berjalan',
	'common.completed': 'Selesai',
	'common.failed': 'Gagal',
	'common.waitingToReconnect': 'Menunggu menyambung ulang...',
	'common.waitingForOutput': 'Menunggu output...',
	'common.unableToRefreshTask': 'Tidak dapat memperbarui progres tugas',
	'common.connectionInterrupted': 'Koneksi terputus: {error}. Menyambung ulang...',
	// Log viewer (LogViewer.svelte)
	'common.failedToLoadLogs': 'Gagal memuat log',
	'common.wsError': 'Kesalahan koneksi WebSocket',
	'common.nLines': '{n} baris',
	'common.stream': 'Streaming',
	'common.stopStream': 'Hentikan streaming',
	// Terminal console (TerminalConsole.svelte)
	'common.connecting': 'Menghubungkan…',
	'common.connected': 'Terhubung',
	'common.disconnected': 'Terputus',
	'common.notConnected': 'Tidak terhubung',
	'common.reconnect': 'Sambungkan ulang',
	'common.typeCommand': 'Ketik perintah…',
	'common.terminal.connectedBanner': '--- Terhubung ke shell persisten ---',
	'common.terminal.disconnectedBanner': '--- Terputus ---',
	'common.terminal.errorBanner': '--- Kesalahan koneksi ---',
	'common.terminal.exitCode': '[keluar {code}]',
	'common.terminal.hints':
		'Status shell (cd, export) tetap tersimpan selama terhubung · riwayat ↑/↓ · Ctrl+L bersihkan · tempel multi-baris untuk dijalankan sebagai satu perintah',
	// Theme toggle (ThemeToggle.svelte)
	'common.switchToLight': 'Ganti ke mode terang',
	'common.switchToDark': 'Ganti ke mode gelap',
	'common.lightMode': 'Mode terang',
	'common.darkMode': 'Mode gelap',
	// Global API errors (api.ts)
	'common.api.timeout':
		'Server tidak merespons dalam {seconds} detik. Jalankan ulang lewat SSH: sudo /opt/jenderal/jenderal restart'
};

export const dict = { en, id };
