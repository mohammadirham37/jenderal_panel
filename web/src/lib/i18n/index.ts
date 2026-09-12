// Global translation registry. Every user-facing string is looked up here via
// $lib/stores/language (translate / t). Each domain file owns a key prefix —
// see the comment at the top of each file. English is the fallback when a key
// is missing from the selected language.
import { dict as core } from './domains/core';
import { dict as common } from './domains/common';
import { dict as dash } from './domains/dash';
import { dict as account } from './domains/account';
import { dict as infra } from './domains/infra';
import { dict as bksrv } from './domains/bksrv';
import { dict as ntfaud } from './domains/ntfaud';
import { dict as usr } from './domains/usr';
import { dict as dkngx } from './domains/dkngx';
import { dict as db } from './domains/db';
import { dict as wssections } from './domains/wssections';
import { dict as wsfiles } from './domains/wsfiles';
import { dict as wl } from './domains/wl';
import { dict as wssub } from './domains/wssub';
import { dict as ws } from './domains/ws';
import { dict as security } from './domains/security';

export const translations: Record<'en' | 'id', Record<string, string>> = {
	en: {
		...core.en,
		...common.en,
		...dash.en,
		...account.en,
		...infra.en,
		...bksrv.en,
		...ntfaud.en,
		...usr.en,
		...dkngx.en,
		...db.en,
		...wssections.en,
		...wsfiles.en,
		...wl.en,
		...wssub.en,
		...ws.en,
		...security.en
	},
	id: {
		...core.id,
		...common.id,
		...dash.id,
		...account.id,
		...infra.id,
		...bksrv.id,
		...ntfaud.id,
		...usr.id,
		...dkngx.id,
		...db.id,
		...wssections.id,
		...wsfiles.id,
		...wl.id,
		...wssub.id,
		...ws.id,
		...security.id
	}
};
