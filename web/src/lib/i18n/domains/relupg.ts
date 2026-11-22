// Domain dictionary: see ../index.ts for how these merge into the global lookup.
// Fill both `en` and `id` — TypeScript enforces that `id` has exactly the keys of `en`.
// Owns key prefixes: `relupg.` (Ubuntu release upgrade card on /update).

const en = {
	'relupg.title': 'Upgrade Ubuntu 24.04 → 26.04',
	'relupg.desc':
		"Runs Canonical's do-release-upgrade without a terminal. The panel keeps every site's PHP version, PostgreSQL clusters and edited configs, and repairs its repositories afterwards. Takes 20–60 minutes; sites may be briefly unavailable while services restart.",
	'relupg.loading': 'Checking whether this server can be upgraded…',
	'relupg.loadFailed': 'Could not check the upgrade',
	'relupg.recheck': 'Check again',
	'relupg.checks': 'Readiness checks',
	'relupg.blocked': 'Fix the items marked as blocking before upgrading.',
	'relupg.start': 'Upgrade to Ubuntu 26.04',
	'relupg.confirm.title': 'Upgrade this server to Ubuntu 26.04?',
	'relupg.confirm.desc':
		'A release upgrade cannot be undone from the panel. If something goes wrong, the only full rollback is a snapshot of the server taken before starting.',
	'relupg.confirm.snapshot': 'I have a snapshot or full backup of this server from just now.',
	'relupg.confirm.type': 'Type 26.04 to confirm',
	'relupg.confirm.cancel': 'Cancel',
	'relupg.confirm.go': 'Start upgrade',
	'relupg.started': 'Upgrade started. It keeps running if you close this page.',
	'relupg.startFailed': 'Could not start the upgrade',
	'relupg.state.running': 'Upgrading…',
	'relupg.state.reboot_required': 'Finished — reboot required',
	'relupg.state.failed': 'Failed',
	'relupg.running.desc': 'The upgrade runs in its own service, so closing this page or restarting the panel does not stop it. Do not reboot until it finishes.',
	'relupg.done.desc': 'Ubuntu 26.04 is installed. Reboot to start the new kernel, then check your sites.',
	'relupg.failed.desc':
		'The upgrade did not finish. Read the log below. If packages were left half-installed, Repair finishes them; the full upgrader log is /var/log/dist-upgrade/main.log.',
	'relupg.warnings': 'Warnings after the upgrade',
	'relupg.log': 'Upgrade log',
	'relupg.reboot': 'Reboot now',
	'relupg.rebootConfirm': 'Reboot the server now? Sites are unavailable until it is back up.',
	'relupg.rebooting': 'Rebooting. The panel will be back in a minute or two.',
	'relupg.rebootFailed': 'Reboot failed',
	'relupg.repair': 'Repair packages',
	'relupg.repairFailed': 'Could not start the repair',
	'relupg.check.release': 'Ubuntu release',
	'relupg.check.available': 'Upgrade offered by Ubuntu',
	'relupg.check.panel_task': 'No upgrade running',
	'relupg.check.pending_updates': 'Packages up to date',
	'relupg.check.reboot_required': 'No pending reboot',
	'relupg.check.dpkg': 'Package system',
	'relupg.check.disk': 'Free disk space',
	'relupg.check.sources': 'Apt repositories',
	'relupg.check.foreign_sources': 'Third-party repositories',
	'relupg.check.php': 'PHP versions used by sites',
	'relupg.check.mysql_native_password': 'MySQL 8.0 → 8.4'
};

const id: Record<keyof typeof en, string> = {
	'relupg.title': 'Upgrade Ubuntu 24.04 → 26.04',
	'relupg.desc':
		'Menjalankan do-release-upgrade resmi Canonical tanpa terminal. Panel mempertahankan versi PHP setiap situs, cluster PostgreSQL, dan konfigurasi yang sudah diedit, lalu memperbaiki repositorinya sesudah upgrade. Butuh 20–60 menit; situs bisa sempat tidak bisa diakses saat layanan dimulai ulang.',
	'relupg.loading': 'Memeriksa apakah server ini bisa di-upgrade…',
	'relupg.loadFailed': 'Gagal memeriksa upgrade',
	'relupg.recheck': 'Periksa lagi',
	'relupg.checks': 'Pemeriksaan kesiapan',
	'relupg.blocked': 'Perbaiki item yang menghalangi sebelum melakukan upgrade.',
	'relupg.start': 'Upgrade ke Ubuntu 26.04',
	'relupg.confirm.title': 'Upgrade server ini ke Ubuntu 26.04?',
	'relupg.confirm.desc':
		'Upgrade rilis tidak bisa dibatalkan dari panel. Kalau terjadi masalah, satu-satunya cara kembali sepenuhnya adalah snapshot server yang dibuat sebelum mulai.',
	'relupg.confirm.snapshot': 'Saya sudah punya snapshot atau backup penuh server ini yang baru dibuat.',
	'relupg.confirm.type': 'Ketik 26.04 untuk konfirmasi',
	'relupg.confirm.cancel': 'Batal',
	'relupg.confirm.go': 'Mulai upgrade',
	'relupg.started': 'Upgrade dimulai. Proses tetap berjalan walaupun halaman ini ditutup.',
	'relupg.startFailed': 'Gagal memulai upgrade',
	'relupg.state.running': 'Sedang upgrade…',
	'relupg.state.reboot_required': 'Selesai — perlu reboot',
	'relupg.state.failed': 'Gagal',
	'relupg.running.desc': 'Upgrade berjalan sebagai layanan tersendiri, jadi menutup halaman ini atau me-restart panel tidak menghentikannya. Jangan reboot sebelum selesai.',
	'relupg.done.desc': 'Ubuntu 26.04 sudah terpasang. Reboot untuk memakai kernel baru, lalu periksa situs-situs Anda.',
	'relupg.failed.desc':
		'Upgrade tidak selesai. Baca log di bawah. Kalau ada paket yang setengah terpasang, tombol Perbaiki akan menyelesaikannya; log lengkap upgrader ada di /var/log/dist-upgrade/main.log.',
	'relupg.warnings': 'Peringatan setelah upgrade',
	'relupg.log': 'Log upgrade',
	'relupg.reboot': 'Reboot sekarang',
	'relupg.rebootConfirm': 'Reboot server sekarang? Situs tidak bisa diakses sampai server menyala kembali.',
	'relupg.rebooting': 'Server sedang reboot. Panel akan kembali dalam satu-dua menit.',
	'relupg.rebootFailed': 'Reboot gagal',
	'relupg.repair': 'Perbaiki paket',
	'relupg.repairFailed': 'Gagal memulai perbaikan',
	'relupg.check.release': 'Rilis Ubuntu',
	'relupg.check.available': 'Upgrade tersedia dari Ubuntu',
	'relupg.check.panel_task': 'Tidak ada upgrade berjalan',
	'relupg.check.pending_updates': 'Paket sudah terbaru',
	'relupg.check.reboot_required': 'Tidak ada reboot tertunda',
	'relupg.check.dpkg': 'Sistem paket',
	'relupg.check.disk': 'Ruang disk kosong',
	'relupg.check.sources': 'Repositori apt',
	'relupg.check.foreign_sources': 'Repositori pihak ketiga',
	'relupg.check.php': 'Versi PHP yang dipakai situs',
	'relupg.check.mysql_native_password': 'MySQL 8.0 → 8.4'
};

export const dict = { en, id };
