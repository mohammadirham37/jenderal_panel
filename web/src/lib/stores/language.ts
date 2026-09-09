import { writable } from 'svelte/store';

export type Language = 'en' | 'id';

const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('jenderal_lang') as Language : null;
export const language = writable<Language>(stored || 'en');

language.subscribe(val => {
    if (typeof localStorage !== 'undefined') {
        localStorage.setItem('jenderal_lang', val);
    }
});

// Translation keys
const translations: Record<Language, Record<string, string>> = {
    en: {
        'nav.dashboard': 'Dashboard',
        'nav.websites': 'Websites',
        'nav.server': 'Server',
        'nav.services': 'Services',
        'nav.nginx': 'Nginx',
        'nav.php': 'PHP',
        'nav.ssl': 'SSL Certificates',
        'nav.deploy': 'Deployments',
        'nav.cron': 'Cron Jobs',
        'nav.queue': 'Queue Workers',
        'nav.nodejs': 'Node.js',
        'nav.databases': 'Databases',
        'nav.docker': 'Docker',
        'nav.backups': 'Backups',
		'nav.security_center': 'Security Center',
        'nav.firewall': 'Firewall',
        'nav.processes': 'Processes',
        'nav.users': 'Users',
        'nav.alerts': 'Alerts',
        'nav.notifications': 'Notifications',
        'nav.terminal': 'Terminal',
        'nav.update': 'Update',
        'nav.audit': 'Audit Logs',
        'nav.settings': 'Settings',
        'nav.group.overview': 'Overview',
        'nav.group.web': 'Web & Apps',
        'nav.group.infrastructure': 'Infrastructure',
        'nav.group.security': 'Security',
        'nav.group.operations': 'Operations',
        'nav.group.system': 'System',
        'settings.language': 'Language',
        'settings.language.en': 'English',
        'settings.language.id': 'Bahasa Indonesia',
    },
    id: {
        'nav.dashboard': 'Dasbor',
        'nav.websites': 'Situs Web',
        'nav.server': 'Server',
        'nav.services': 'Layanan',
        'nav.nginx': 'Nginx',
        'nav.php': 'PHP',
        'nav.ssl': 'Sertifikat SSL',
        'nav.deploy': 'Deployment',
        'nav.cron': 'Tugas Cron',
        'nav.queue': 'Antrean Worker',
        'nav.nodejs': 'Node.js',
        'nav.databases': 'Database',
        'nav.docker': 'Docker',
        'nav.backups': 'Cadangan',
		'nav.security_center': 'Pusat Keamanan',
        'nav.firewall': 'Firewall',
        'nav.processes': 'Proses',
        'nav.users': 'Pengguna',
        'nav.alerts': 'Peringatan',
        'nav.notifications': 'Notifikasi',
        'nav.terminal': 'Terminal',
        'nav.update': 'Pembaruan',
        'nav.audit': 'Log Audit',
        'nav.settings': 'Pengaturan',
        'nav.group.overview': 'Ringkasan',
        'nav.group.web': 'Web & Aplikasi',
        'nav.group.infrastructure': 'Infrastruktur',
        'nav.group.security': 'Keamanan',
        'nav.group.operations': 'Operasi',
        'nav.group.system': 'Sistem',
        'settings.language': 'Bahasa',
        'settings.language.en': 'English',
        'settings.language.id': 'Bahasa Indonesia',
    }
};

export function t(key: string): string {
    let lang: Language = 'en';
    language.subscribe(v => lang = v)();
    return translations[lang][key] || key;
}

// Reactive translation - use in components with $language
export function translate(lang: Language, key: string): string {
    return translations[lang]?.[key] || key;
}
