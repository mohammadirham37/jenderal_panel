import { writable } from 'svelte/store';

/** Hostname of the server this panel manages; empty until fetched. */
export const serverHostname = writable<string>('');
