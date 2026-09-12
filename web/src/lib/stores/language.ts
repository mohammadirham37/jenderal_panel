import { writable } from 'svelte/store';
import { translations } from '$lib/i18n';

export type Language = 'en' | 'id';

const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('jenderal_lang') as Language : null;
export const language = writable<Language>(stored || 'en');

language.subscribe(val => {
    if (typeof localStorage !== 'undefined') {
        localStorage.setItem('jenderal_lang', val);
    }
});

// Translation dictionaries live in $lib/i18n/domains/* and are merged in
// $lib/i18n/index.ts. Keys are grouped by module prefix (e.g. 'bk.create').
// translate() falls back to English, then to the raw key.

export function t(key: string): string {
    let lang: Language = 'en';
    language.subscribe(v => lang = v)();
    return translate(lang, key);
}

// Reactive translation - use in components with $language
export function translate(lang: Language, key: string): string {
    return translations[lang]?.[key] ?? translations.en?.[key] ?? key;
}
