import type { Language } from '$lib/stores/language';

// Long-form guide content is kept in its own module (selected by the $language
// store) instead of the flat translation dictionary, so documentation paragraphs
// do not bloat language.ts.

export type GuideBlock =
	| { type: 'p'; text: string }
	| { type: 'list'; items: string[] }
	| { type: 'steps'; items: string[] }
	| { type: 'tip'; text: string }
	| { type: 'warning'; text: string }
	| { type: 'table'; headers: string[]; rows: string[][] }
	| { type: 'terms'; items: { term: string; def: string }[] };

export interface GuideSubsection {
	id: string;
	title: string;
	blocks: GuideBlock[];
}

export interface GuideSection {
	id: string;
	title: string;
	blocks: GuideBlock[];
	subsections?: GuideSubsection[];
}

export interface GuideDoc {
	title: string;
	subtitle: string;
	sections: GuideSection[];
}

const en: GuideDoc = {
	title: 'User Guide',
	subtitle: 'Everything you need to run a server with Jenderal Panel, step by step.',
	sections: [
		{
			id: 'intro',
			title: '1. About This Guide',
			blocks: [
				{
					type: 'p',
					text: 'Jenderal Panel is a control panel for Ubuntu servers (22.04 and 24.04). It puts the everyday jobs of a server administrator behind a friendly web page: hosting websites, running databases, installing SSL certificates, making backups, and keeping the server safe.'
				},
				{ type: 'p', text: 'This guide walks through every page of the panel in the same order you will usually need them — from creating your first website to scheduling backups. Each section explains what the page is for and the exact steps to use it.' },
				{
					type: 'list',
					items: [
						'New to the panel? Read sections 1–4 in order.',
						'Looking for one specific feature? Use the table of contents on the left to jump straight to it.',
						'Words shown like `php.ini` are names of files, buttons, or technical terms — they are all explained in the Glossary at the end.'
					]
				},
				{
					type: 'tip',
					text: 'This guide follows the language you picked in Settings → Language. Switch there between English and Bahasa Indonesia at any time — the whole guide changes with it.'
				}
			]
		},
		{
			id: 'basics',
			title: '2. Getting Around the Panel',
			blocks: [
				{ type: 'p', text: 'After logging in you land on the Dashboard. Everything else lives in the left sidebar, grouped so similar pages sit together:' },
				{
					type: 'table',
					headers: ['Menu group', 'What is inside'],
					rows: [
						['Overview', 'Dashboard (server summary) and this User Guide.'],
						['Web & Apps', 'Websites (sites and apps), PHP versions, Node.js applications.'],
						['Infrastructure', 'Server settings, Services, Nginx, Databases, Docker.'],
						['Security', 'Security Center, Firewall, Users, Alerts, Notifications.'],
						['Operations', 'Backups, Processes, Terminal.'],
						['System', 'Update (upgrade the panel), Audit Logs, Settings.']
					]
				},
				{ type: 'p', text: 'The sun/moon button in the top bar switches between dark and light theme. Your choice is remembered on this browser.' },
				{ type: 'p', text: 'Not every menu appears for every account. The panel uses roles and permissions: an account with a limited role only sees the pages it is allowed to open. Only administrators see everything, including the Terminal page.' },
				{
					type: 'tip',
					text: 'The sidebar can be narrowed to icons only with the arrow button at its top — handy on smaller laptop screens.'
				}
			]
		},
		{
			id: 'dashboard',
			title: '3. Dashboard',
			blocks: [
				{ type: 'p', text: 'The Dashboard is the health screen of your server. It updates itself live — if the badge says Reconnecting, the page is trying to reach the server again and will recover on its own.' },
				{
					type: 'list',
					items: [
						'Resources — live gauges for CPU, memory, disk, swap and load, plus network upload/download speed.',
						'Resource history — small charts of the last 60 samples so you can spot spikes that happened while you were away.',
						'System information — operating system, uptime, CPU cores and disk partitions.',
						'At a glance — quick counts of websites, databases, backups and SSL certificates, including certificates that will expire soon.',
						'Recent alerts — the latest warnings produced by your alert rules (see the Monitoring section).'
					]
				},
				{
					type: 'tip',
					text: 'If a service is stopped or a certificate is close to expiry, the Dashboard tells you here first. Make it a habit to glance at it after logging in.'
				}
			]
		},
		{
			id: 'first-website',
			title: '4. Creating Your First Website',
			blocks: [
				{ type: 'p', text: 'Websites is the heart of the panel. One entry here equals one domain hosted on the server, and every site gets its own Linux user, folder and settings.' },
				{
					type: 'steps',
					items: [
						'Point your domain to the server first: create a DNS A record (or AAAA for IPv6) that sends your domain to the server IP address. SSL installation needs this.',
						'Open Websites and fill in the Create Website form: your domain (for example `contoh.com`), the template, and — for PHP sites — the PHP version.',
						'Pick a template. Static HTML for plain files, Native PHP for classic PHP projects, CodeIgniter 3/4, Laravel (choose Blade, Inertia or Livewire), Laravel Octane on FrankenPHP, WordPress, or an app runtime: Node.js, Go, Python, Deno or Bun.',
						'Some templates let you choose a project variant (empty project or starter kit) and whether the panel should only write the Nginx config or also install the framework for you.',
						'Press Create. The site now goes through stages — pending, installing, configuring, validating — and you can watch the provisioning log live on its card.',
						'When the badge turns active, open `https://your-domain` in a browser to see the result.'
					]
				},
				{
					type: 'warning',
					text: 'If provisioning fails, the card shows a Retry button — fix the reported problem (often DNS not pointing yet) and retry. A suspended site is offline but kept; use Suspend when you want to pause a site without deleting its files.'
				},
				{
					type: 'tip',
					text: 'Use the search box and the status filter chips above the site list to quickly find a site when you host many of them.'
				}
			]
		},
		{
			id: 'manage-site',
			title: '5. Managing a Website',
			blocks: [
				{ type: 'p', text: 'Press Manage on a site card to open its detail page. Everything specific to that one site lives here, organised in tabs:' },
				{
					type: 'table',
					headers: ['Tab', 'What you do there'],
					rows: [
						['Overview', 'Site status, owner, health check, per-site runtimes.'],
						['Deployment', 'Deploy code from Git or an uploaded archive.'],
						['SSL', 'Install and renew HTTPS certificates.'],
						['Commands', 'One-click command presets and the .env editor.'],
						['PHP Settings', 'Per-site PHP limits (hidden for static sites).'],
						['App', 'Start/stop and build app runtimes (Node.js, Go, Python, Deno, Bun).'],
						['WP Toolkit', 'WordPress core, plugin and theme updates.'],
						['Cron Jobs', 'Scheduled tasks for this site.'],
						['Files', 'Full file manager with editor and upload.'],
						['Terminal', 'Shell inside this site folder.'],
						['Logs', 'Access, error and Octane logs.'],
						['Config', 'Nginx vhost editor and template.'],
						['Domains', 'Alias and subdomain domains.'],
						['Queue', 'Laravel queue workers (Laravel sites only).']
					]
				},
				{
					type: 'tip',
					text: 'The open tab is remembered in the web address (`?tab=...`), so you can bookmark a specific tab of a site.'
				}
			],
			subsections: [
				{
					id: 'deploy',
					title: 'Deploying your code',
					blocks: [
						{ type: 'p', text: 'The Deployment tab gets your code onto the server. Two ways:' },
						{
							type: 'steps',
							items: [
								'Git: choose your provider (GitHub, GitLab, Bitbucket or a custom URL), press Generate deploy key, and add the shown public key to your repository as a deploy key. Then fill in the repository and branch and press Deploy Now.',
								'Archive: upload a `.zip` or `.tar.gz` of your project with Upload & Extract. The panel extracts it into the site folder.'
							]
						},
						{ type: 'p', text: 'Every deploy is stored in the history list with its log, so you can always check what was deployed and when. Prefer doing it yourself? Connect with the site Terminal over SSH and pull manually.' }
					]
				},
				{
					id: 'app',
					title: 'App runtimes (Node.js, Go, Python, Deno, Bun)',
					blocks: [
						{ type: 'p', text: 'Sites that are not plain PHP run as a small background service (a systemd unit) listening on a private port, with Nginx forwarding visitor traffic to it. The App tab shows the service status, its port, and Start / Stop / Restart buttons.' },
						{
							type: 'list',
							items: [
								'Node.js — runs through a per-site NVM version; pick the version when creating the site.',
								'Go — either built from source on the server, or a prebuilt binary you upload.',
								'Python — the panel creates a virtualenv (venv) for the site and installs your `requirements.txt`.',
								'Deno and Bun — installed pinned on the server and used to run your entry file.'
							]
						},
						{ type: 'p', text: 'The start command is editable. After changing code, press Build & restart so the new code goes live. If the app fails to start, the provisioning or service log tells you why.' }
					]
				},
				{
					id: 'ssl',
					title: 'SSL certificates (HTTPS)',
					blocks: [
						{ type: 'p', text: 'The SSL tab issues free Let\'s Encrypt certificates. The usual choice is Let\'s Encrypt (HTTP), which proves you own the domain over HTTP — the domain must already point to this server.' },
						{
							type: 'steps',
							items: [
								'Make sure DNS for the domain (and any alias) already points to the server.',
								'On the SSL tab choose Let\'s Encrypt, select the domains, and press issue.',
								'The certificate is installed and set to renew automatically. Each certificate shows its expiry date, colour-coded, with an auto-renew switch you can turn off.'
							]
						},
						{ type: 'p', text: 'Other options: Wildcard (`*.contoh.com`) uses DNS validation and needs a Cloudflare API token; Custom lets you paste a certificate and key you got elsewhere.' },
						{
							type: 'warning',
							text: 'If issuing fails, in nine out of ten cases DNS is not pointing to the server yet, or the change has not spread. Wait for the DNS to resolve and try again.'
						}
					]
				},
				{
					id: 'files',
					title: 'File manager',
					blocks: [
						{ type: 'p', text: 'The Files tab is a complete file manager for the site folder — no FTP needed. Browse with the breadcrumb path, create folders and files, rename, delete, download, and upload with the button or by dragging files in (uploads show a progress queue).' },
						{ type: 'p', text: 'Click a text file to edit it in the built-in editor. Press `Ctrl+S` to save; the editor warns before closing with unsaved changes.' }
					]
				},
				{
					id: 'cron',
					title: 'Cron jobs',
					blocks: [
						{ type: 'p', text: 'Cron jobs run commands automatically on a schedule — for example `php artisan schedule:run` every minute on Laravel, or a nightly cleanup script. They are managed per site in the Cron Jobs tab.' },
						{
							type: 'steps',
							items: [
								'Press create, write the command, and pick a schedule preset (every minute, hourly, daily, weekly, monthly) or type a custom cron expression.',
								'Save. Use the toggle to pause a job without deleting it.',
								'Edit or delete entries at any time from the list.'
							]
						}
					]
				},
				{
					id: 'php',
					title: 'PHP settings and queue workers',
					blocks: [
						{ type: 'p', text: 'PHP Settings overrides limits for this site only: memory limit, upload size, execution time and more — useful when one site needs a bigger upload limit than the rest of the server.' },
						{ type: 'p', text: 'On Laravel sites, the Queue tab manages queue workers as proper services: choose how many workers to run, press create, then start/stop/restart them. Status and the last 100 log lines are shown inline, so a stuck queue is easy to diagnose.' }
					]
				},
				{
					id: 'wp',
					title: 'WordPress Toolkit',
					blocks: [
						{ type: 'p', text: 'For WordPress sites this tab shows the core version and whether core, plugin or theme updates are waiting. Every action is a one-click task run safely as the site user:' },
						{
							type: 'list',
							items: [
								'Update core — updates WordPress itself.',
								'Update all plugins / Update all themes — brings extensions up to date.',
								'Update database — runs the database migration after a core update.',
								'Flush cache — clears the WordPress cache.'
							]
						},
						{
							type: 'tip',
							text: 'Take a backup (see the Backups section) before running big WordPress updates — if an update breaks the site you can restore in minutes.'
						}
					]
				},
				{
					id: 'misc',
					title: 'The remaining tabs',
					blocks: [
						{
							type: 'list',
							items: [
								'Commands — one-click presets for git, Composer, Artisan and npm/yarn/pnpm, with confirmation before anything risky runs. Also the `.env` editor for Laravel, in key/value or raw mode.',
								'Config — edit the Nginx vhost by hand, or pick a template: Auto, Generic PHP, Laravel, CodeIgniter 3/4 or Static. App-based sites use a reverse-proxy config automatically.',
								'Domains — add alias or subdomain domains that share this site; newly added ones usually need their own SSL certificate.',
								'Logs — access and error logs (plus Octane log on Laravel Octane sites).',
								'Terminal — a shell that starts inside this site folder as this site\'s user.',
								'Overview — configure a health-check URL with the expected HTTP status and press check now to test; administrators can also transfer the site to another panel user.'
							]
						}
					]
				}
			]
		},
		{
			id: 'php',
			title: '6. PHP Versions',
			blocks: [
				{ type: 'p', text: 'The PHP page manages the PHP versions installed on the server itself (8.1 to 8.4). A version must be installed here before a website can use it.' },
				{
					type: 'list',
					items: [
						'Install — downloads and sets up that PHP version as a background task with progress.',
						'Restart — restarts the FPM service after config changes.',
						'Uninstall — removes the version (sites still using it will stop working).',
						'php.ini editor — Simple mode exposes the common limits (memory, upload size, execution time, error display); Advanced mode is a raw editor of the whole file for the unusual settings.'
					]
				},
				{
					type: 'tip',
					text: 'Per-site overrides live on the site itself (PHP Settings tab). Only change the global php.ini when the setting should apply to every PHP site.'
				}
			]
		},
		{
			id: 'nodejs',
			title: '7. Node.js Applications',
			blocks: [
				{ type: 'p', text: 'The Node.js page lists which websites have a Node.js runtime installed and lets you create applications on them: pick the site, the package manager (npm, yarn or pnpm), the build command, start arguments and the port your app listens on.' },
				{
					type: 'list',
					items: [
						'Each app gets start / stop / restart controls, and long installs run as tasks with progress.',
						'Every site keeps its own Node version (through NVM), so one site can run Node 20 while another runs Node 22.',
						'If the server still has an old global Node.js, a removal card offers to clean it up — only remove it when no app depends on it.'
					]
				}
			]
		},
		{
			id: 'nginx',
			title: '8. Nginx',
			blocks: [
				{ type: 'p', text: 'Nginx is the web server that receives every visitor and forwards them to your sites. The Nginx page shows its status (installed, running, version, config test) with Start, Stop, Restart and Reload buttons.' },
				{
					type: 'list',
					items: [
						'Global config — Simple mode for the common knobs (worker processes, max upload size, keepalive, gzip); Manual mode for the raw file.',
						'Per-site table — enable/disable a site, edit its vhost directly, or delete it.',
						'Logs — read the access and error logs without opening a terminal.'
					]
				},
				{
					type: 'warning',
					text: 'Always press Test Config after editing Nginx settings. A broken config can stop all sites at once; the test catches syntax mistakes before the reload applies them.'
				}
			]
		},
		{
			id: 'databases',
			title: '9. Databases',
			blocks: [
				{ type: 'p', text: 'The Databases page manages MySQL, PostgreSQL and Redis. Each engine gets a card to install it (background task) and start/stop/restart it.' },
				{
					type: 'steps',
					items: [
						'Create a database: press create, choose the engine and a name (and character set if you need a specific one).',
						'Create a database user: generate a strong password with the built-in generator (choose length and symbols), then grant that user privileges on the database.',
						'Give the database name, user and password to your application — for Laravel that is the `.env` file on the site\'s Commands tab.'
					]
				},
				{
					type: 'list',
					items: [
						'Export — download a `.sql` or compressed `.sql.gz` dump of any database.',
						'Restore — upload a dump file to refill a database (the panel asks for confirmation, because it overwrites the current contents).',
						'User tools — copy a password, reset it, or delete a user.'
					]
				},
				{
					type: 'tip',
					text: 'Need to look inside the data? The web database manager (opened per user from the databases page) is a phpMyAdmin-style browser: browse and search rows, view table structure, run SQL in the console, and export CSV.'
				}
			]
		},
		{
			id: 'docker',
			title: '10. Docker',
			blocks: [
				{ type: 'p', text: 'The Docker page runs container workloads next to your normal sites. Install the Docker daemon from here if it is not present, then control it with Start / Stop / Restart.' },
				{
					type: 'table',
					headers: ['Tab', 'What you do there'],
					rows: [
						['Containers', 'List containers with their state, start/stop/restart/remove them, and read the last 100 log lines.'],
						['Images', 'Pull an image by name and delete images you no longer need.'],
						['Volumes', 'Create and delete persistent data volumes.'],
						['Networks', 'Create and delete networks (the default ones are protected).'],
						['Compose', 'Point at a `docker-compose.yml` path on the server and run compose up or down.']
					]
				}
			]
		},
		{
			id: 'server',
			title: '11. Server Settings',
			blocks: [
				{ type: 'p', text: 'The Server page holds the machine-level settings: system information, the hostname, the timezone (searchable list), disk partitions and network interfaces.' },
				{
					type: 'steps',
					items: [
						'Changing the SSH port: enter the new port. The panel works in two phases — it applies the new port but keeps the old one accepting, and only closes the old port after you confirm the new one works (finalize).',
						'Reboot: available at the bottom with a confirmation dialog. Running sites come back automatically when the server is up again.'
					]
				},
				{
					type: 'warning',
					text: 'The panel refuses SSH ports that would lock you out — including its own port. Still, change the SSH port only when you can reach the server another way if something goes wrong.'
				}
			]
		},
		{
			id: 'services',
			title: '12. Services',
			blocks: [
				{ type: 'p', text: 'The Services page lists the managed services (Nginx, PHP-FPM versions, database engines, and more) with their status: running, stopped or not installed, plus whether they start on boot.' },
				{ type: 'p', text: 'Each row has Start, Stop and Restart buttons. If the Dashboard reports a stopped service, this is where you bring it back.' },
				{
					type: 'tip',
					text: 'The Developer dependencies card installs or updates Composer — the PHP dependency tool many frameworks need during deployment.'
				}
			]
		},
		{
			id: 'security',
			title: '13. Security Center',
			blocks: [
				{ type: 'p', text: 'The Security Center gathers everything that keeps the server safe into one place, organised in tabs.' },
				{
					type: 'list',
					items: [
						'Overview — a security report: firewall state, AppArmor, SSH settings, Nginx config validity, pending OS security updates. Every finding carries a severity and a hint on how to fix it.',
						'Setup (Safe Setup) — one guided hardening run: choose which websites to protect, the management IP ranges, enable Fail2ban, install the malware scanner with a daily schedule, and switch on Traffic Guard. It first shows a review (dry run), then applies everything as a resumable task.',
						'Fail2ban — see which addresses are currently banned and unban one if it was a mistake.',
						'Malware — update signatures, run a quick scan or scan all websites, and manage quarantined files (restore or delete).',
						'Traffic — per-site rate limiting. Sites start in Observe mode (only watching); after checking the report you confirm to enforce limits. Works with sites behind Cloudflare.',
						'Events — the feed of security events, so you can see what happened and when.'
					]
				},
				{
					type: 'tip',
					text: 'Run Safe Setup once on a fresh server — it applies sensible defaults for Fail2ban, malware scanning and traffic watching in a single pass.'
				}
			]
		},
		{
			id: 'firewall',
			title: '14. Firewall',
			blocks: [
				{ type: 'p', text: 'The Firewall page controls UFW — the gate in front of your server. When it is enabled, only ports with an ALLOW rule are reachable from the internet.' },
				{
					type: 'list',
					items: [
						'Add rule — port, protocol (TCP/UDP), action (allow or deny), an optional source IP to limit who may connect, and a comment so future-you remembers why it exists.',
						'Delete rule — removes it from the table. Deleting the SSH rule asks extra questions, because that is the port you connect through.'
					]
				},
				{
					type: 'warning',
					text: 'Disabling the firewall or deleting the SSH allow-rule can cut your access to the server. The panel warns before both — read the warning and make sure you have another way in (for example the provider console) before continuing.'
				}
			]
		},
		{
			id: 'users',
			title: '15. Users & Roles',
			blocks: [
				{ type: 'p', text: 'The Users page manages who can log in to the panel. Each user has a username, email, password and a role that decides which menus they see. Optionally a user also gets a Linux SSH account with its own home directory.' },
				{
					type: 'list',
					items: [
						'Create user — fill the form and choose the role; tick the SSH option to also create the Linux account.',
						'SSH keys — open a user\'s key manager to add or remove public keys for SSH/SFTP login.',
						'Edit — change role or the SSH account later.',
						'Delete — removes the panel user and, with it, the Linux account and its home directory. The panel warns first, and websites owned by the user can be transferred first.'
					]
				},
				{
					type: 'tip',
					text: 'Give colleagues a limited role instead of sharing your admin password — every action is recorded in the Audit Logs with the user\'s name.'
				}
			]
		},
		{
			id: 'backups',
			title: '16. Backups',
			blocks: [
				{ type: 'p', text: 'Backups protect you from the bad day. The Backups page shows summary cards (completed backups, total size, free disk, last backup) and everything else around backup lives here.' },
				{
					type: 'steps',
					items: [
						'Press Backup now and choose the type: a single website, a database, the panel configuration, or everything (full).',
						'Pick the target if you host several sites or databases.',
						'The backup runs as a task; when it finishes it appears in the Backups tab with its size and status.'
					]
				},
				{
					type: 'list',
					items: [
						'Restore — choose the backup and the part to restore. Restoring overwrites the current data, so the panel asks for confirmation.',
						'Download — grab the backup file to keep a copy outside the server.',
						'Schedules tab — automate it: type, target, how often (preset or custom cron), and the retention rule (keep N days, or keep the last N backups).',
						'Prune now — runs the retention cleanup immediately, deleting backups that are no longer needed.'
					]
				},
				{
					type: 'tip',
					text: 'Backups kept on the same server die with the server. In Settings → remote storage, connect an S3-compatible bucket (AWS S3, Wasabi, Cloudflare R2, MinIO…) or an rclone remote — every new backup is then copied off-site automatically and marked with an off-site badge.'
				}
			]
		},
		{
			id: 'monitoring',
			title: '17. Monitoring: Processes, Alerts, Notifications',
			blocks: [
				{ type: 'p', text: 'Three pages together tell you what the server is doing and warn you before things break.' },
				{
					type: 'list',
					items: [
						'Processes — a live table of everything running, sortable by CPU or memory, refreshing every 5 seconds when enabled. Kill ends a runaway process; you choose the signal (SIGTERM asks nicely, SIGKILL forces).',
						'Alerts — create rules such as "CPU above 90% for 5 minutes" or "certificate expiring in 14 days". Matching events appear in the alert history, marked resolved once the value recovers.',
						'Notifications — deliver alerts to humans: email (SMTP), Telegram, Discord or a generic webhook. Each channel has a Send test button, and secrets are masked in the list.'
					]
				},
				{
					type: 'tip',
					text: 'A good starter set: disk above 85%, any service down, and SSL expiring within 14 days — delivered to a Telegram channel you actually read.'
				}
			]
		},
		{
			id: 'terminal',
			title: '18. Terminal',
			blocks: [
				{ type: 'p', text: 'The Terminal page is a full shell on the server, inside the browser. It is administrator-only, because it can touch anything on the machine.' },
				{ type: 'p', text: 'Regular users with site access get a safer variant: the Terminal tab on a website starts the shell inside that site\'s folder, as that site\'s user — enough for `composer`, `artisan`, `npm` and friends without exposing the rest of the server.' }
			]
		},
		{
			id: 'settings',
			title: '19. Settings',
			blocks: [
				{ type: 'p', text: 'The Settings page collects the panel\'s own configuration:' },
				{
					type: 'list',
					items: [
						'Language — switch the whole panel (including this guide) between English and Bahasa Indonesia.',
						'Panel domain — serve the panel over HTTPS on your own domain instead of the IP:8443 address, with an automatic Let\'s Encrypt certificate. Removing it falls back to IP:8443.',
						'Two-factor authentication — strengthen your login with a TOTP app (Google Authenticator, Aegis, 1Password…): scan the QR code, enter the 6-digit code, done.',
						'SSH keys — if your account has an SSH account, manage your public keys here (password login is then disabled).',
						'API tokens — create named tokens for scripts that talk to the panel API. The token is shown once — copy it immediately. The list shows when each token was last used.',
						'Remote backup storage — connect S3 or rclone as the off-site target used by the backup module.',
						'All other settings — every known setting key appears as an editable row with its last-updated time; saving sends only what you changed.'
					]
				}
			]
		},
		{
			id: 'update',
			title: '20. Updating the Panel',
			blocks: [
				{ type: 'p', text: 'The Update page keeps the panel itself current. It shows your version against the latest published version, with an Update available or Up to date badge and a Check again button.' },
				{
					type: 'steps',
					items: [
						'Press Update now and confirm. The panel pulls the latest source, rebuilds, swaps the binary and restarts itself.',
						'The whole run takes roughly two to five minutes; sites keep running, but the panel is briefly unreachable.',
						'When finished the page reloads onto the new version.'
					]
				},
				{
					type: 'warning',
					text: 'Always update through this page — never pull code or run builds by hand on the server, or the next update may conflict with your changes.'
				}
			]
		},
		{
			id: 'audit',
			title: '21. Audit Logs',
			blocks: [
				{ type: 'p', text: 'Every important action in the panel is written down: who did it, what, when, and with what data. The Audit Logs page is that record.' },
				{
					type: 'list',
					items: [
						'Filter by date with the quick ranges (today, yesterday, last 7 or 30 days) or a custom from/to.',
						'Search free text, or narrow to one module with the module filter.',
						'Click a row to expand the details of what exactly was changed.'
					]
				},
				{
					type: 'tip',
					text: 'Something broke and nobody admits touching it? Filter the audit log to the hour before the problem and check the recent entries.'
				}
			]
		},
		{
			id: 'faq',
			title: '22. FAQ & Troubleshooting',
			blocks: [
				{
					type: 'terms',
					items: [
						{ term: 'I lost the panel address after setting a panel domain', def: 'Without a panel domain the panel answers on `https://SERVER-IP:8443`. Removing the panel domain in Settings returns you to that address. If HTTPS on the domain fails, the IP:8443 address keeps working.' },
						{ term: 'A new site stays in pending or fails provisioning', def: 'Open the site card and read the provisioning log. The usual causes: DNS not pointing to the server yet, or a missing dependency shown on the create form (with an install link). Fix the cause and press Retry.' },
						{ term: 'SSL issuing fails', def: 'Almost always DNS: the domain must point to this server before Let\'s Encrypt will issue. Wildcard certificates additionally need a Cloudflare API token. Wait for DNS to spread and try again.' },
						{ term: 'My Node/Python/Go app returns 502', def: 'The app service is not running or listens on a different port than configured. Open the site\'s App tab, check the port, press Restart, and read the service log.' },
						{ term: 'A website shows the default page instead of my app', def: 'The Nginx template may not match the site type. On the site\'s Config tab pick the matching template (Auto handles this for app sites), then test the Nginx config on the Nginx page.' },
						{ term: 'Uploads fail for large files', def: 'Three limits can bite: PHP `upload_max_filesize` (site PHP Settings tab), Nginx `client_max_body_size` (Nginx global config), and the app\'s own body limit. Raise the ones that apply.' },
						{ term: 'A service is stopped', def: 'Dashboard shows which one. Go to Services, press Start, and check its logs if it refuses to stay up. The Security Center report also flags services that fail repeatedly.' },
						{ term: 'I locked myself out with the firewall', def: 'Use your hosting provider\'s console/VNC access to reach the server, re-enable SSH access, and re-add the allow rule. The panel warns before every rule that can cause this.' },
						{ term: 'Where did my backup go?', def: 'Check the Backups tab filters (type and status) and the retention rule of your schedule — pruning deletes backups beyond retention. Backups with an off-site badge also exist in your S3/rclone storage.' },
						{ term: 'Two-factor codes are rejected', def: 'Phone time drift breaks TOTP codes. Enable automatic time on the phone, or disable 2FA from Settings (with a confirmation) and set it up again.' }
					]
				}
			]
		},
		{
			id: 'glossary',
			title: '23. Glossary',
			blocks: [
				{
					type: 'terms',
					items: [
						{ term: 'Document root', def: 'The folder of a website that Nginx serves to visitors, for example the site\'s `public` directory.' },
						{ term: 'Vhost (virtual host)', def: 'The Nginx configuration block of one domain — it tells Nginx which folder (or app port) answers for that domain.' },
						{ term: 'Reverse proxy', def: 'A config where Nginx accepts the visitor and forwards the request to an app running on a private port (used by Node.js, Go, Python, Deno and Bun sites).' },
						{ term: 'Let\'s Encrypt', def: 'A free certificate authority. Its certificates last 90 days and are renewed automatically by the panel.' },
						{ term: 'HTTP-01 / DNS-01', def: 'Two ways to prove domain ownership for a certificate: serving a special file over HTTP, or adding a DNS record (needed for wildcards).' },
						{ term: 'Cron', def: 'The scheduler that runs commands at fixed times; a cron expression like `*/5 * * * *` means every 5 minutes.' },
						{ term: 'systemd unit', def: 'A background service managed by Linux (shown on the Services page and as App services for non-PHP sites).' },
						{ term: 'Queue worker', def: 'A background process that executes queued jobs (emails, exports…) for frameworks like Laravel, so web requests stay fast.' },
						{ term: 'NVM', def: 'Node Version Manager — lets each website use its own Node.js version.' },
						{ term: 'venv', def: 'A Python virtual environment — a per-site folder of Python packages so sites do not conflict.' },
						{ term: 'Deploy key', def: 'A read-only SSH key that gives a Git provider access to exactly one repository, used for deployments.' },
						{ term: 'UFW', def: 'Uncomplicated Firewall — the firewall the panel manages; without an ALLOW rule a port is closed to the internet.' },
						{ term: 'Fail2ban', def: 'A guard that automatically blocks IP addresses that repeatedly try to break in (for example brute-forcing SSH).' },
						{ term: 'Retention', def: 'How long backups are kept (N days, or the last N backups) before pruning removes the old ones.' },
						{ term: 'TOTP', def: 'Time-based one-time password — the 6-digit codes from an authenticator app used for two-factor login.' }
					]
				}
			]
		}
	]
};

const id: GuideDoc = {
	title: 'Panduan Pengguna',
	subtitle: 'Semua yang Anda perlukan untuk menjalankan server dengan Jenderal Panel, langkah demi langkah.',
	sections: [
		{
			id: 'intro',
			title: '1. Tentang Panduan Ini',
			blocks: [
				{
					type: 'p',
					text: 'Jenderal Panel adalah panel kontrol untuk server Ubuntu (22.04 dan 24.04). Ia menyatukan pekerjaan sehari-hari seorang administrator server ke dalam satu halaman web yang mudah: menghosting website, menjalankan database, memasang sertifikat SSL, membuat cadangan, dan menjaga keamanan server.'
				},
				{ type: 'p', text: 'Panduan ini membahas setiap halaman panel dengan urutan yang sama seperti saat Anda biasanya membutuhkannya — dari membuat website pertama sampai menjadwalkan cadangan. Setiap bagian menjelaskan fungsi halaman tersebut dan langkah-langkah tepat untuk memakainya.' },
				{
					type: 'list',
					items: [
						'Baru pertama kali memakai panel? Baca bagian 1–4 secara berurutan.',
						'Mencari satu fitur tertentu? Gunakan daftar isi di sebelah kiri untuk langsung melompat ke bagian itu.',
						'Kata yang ditampilkan seperti `php.ini` adalah nama file, tombol, atau istilah teknis — semuanya dijelaskan di Glosarium di bagian akhir.'
					]
				},
				{
					type: 'tip',
					text: 'Panduan ini mengikuti bahasa yang Anda pilih di Pengaturan → Bahasa. Ganti kapan saja antara Bahasa Indonesia dan English — seluruh isi panduan ikut berubah.'
				}
			]
		},
		{
			id: 'basics',
			title: '2. Mengenal Tampilan Panel',
			blocks: [
				{ type: 'p', text: 'Setelah login Anda tiba di Dasbor. Semua halaman lain ada di sidebar kiri, dikelompokkan agar halaman yang sejenis berkumpul bersama:' },
				{
					type: 'table',
					headers: ['Grup menu', 'Isi di dalamnya'],
					rows: [
						['Ringkasan', 'Dasbor (ringkasan server) dan Panduan ini.'],
						['Web & Aplikasi', 'Situs Web (website dan aplikasi), versi PHP, aplikasi Node.js.'],
						['Infrastruktur', 'Pengaturan Server, Layanan, Nginx, Database, Docker.'],
						['Keamanan', 'Pusat Keamanan, Firewall, Pengguna, Peringatan, Notifikasi.'],
						['Operasi', 'Cadangan, Proses, Terminal.'],
						['Sistem', 'Pembaruan (upgrade panel), Log Audit, Pengaturan.']
					]
				},
				{ type: 'p', text: 'Tombol matahari/bulan di bar atas mengganti tema gelap dan terang. Pilihan Anda diingat di browser ini.' },
				{ type: 'p', text: 'Tidak semua menu muncul untuk semua akun. Panel memakai peran dan izin (roles & permissions): akun dengan peran terbatas hanya melihat halaman yang boleh dibukanya. Hanya administrator yang melihat semuanya, termasuk halaman Terminal.' },
				{
					type: 'tip',
					text: 'Sidebar bisa disempitkan menjadi ikon saja dengan tombol panah di bagian atasnya — berguna di layar laptop yang kecil.'
				}
			]
		},
		{
			id: 'dashboard',
			title: '3. Dasbor',
			blocks: [
				{ type: 'p', text: 'Dasbor adalah layar kesehatan server Anda. Ia diperbarui secara langsung — jika badge tertulis Menyambung ulang, halaman sedang mencoba menghubungi server lagi dan akan pulih dengan sendirinya.' },
				{
					type: 'list',
					items: [
						'Sumber daya — indikator langsung untuk CPU, memori, disk, swap dan beban, plus kecepatan unggah/unduh jaringan.',
						'Riwayat sumber daya — grafik kecil dari 60 sampel terakhir sehingga lonjakan saat Anda pergi tetap terlihat.',
						'Informasi sistem — sistem operasi, uptime, jumlah inti CPU, dan partisi disk.',
						'Sekilas — hitungan cepat situs web, database, cadangan, dan sertifikat SSL, termasuk sertifikat yang akan segera kedaluwarsa.',
						'Peringatan terbaru — peringatan terakhir yang dihasilkan aturan alert Anda (lihat bagian Pemantauan).'
					]
				},
				{
					type: 'tip',
					text: 'Kalau ada layanan berhenti atau sertifikat hampir kedaluwarsa, Dasbor menunjukkannya lebih dulu. Biasakan melihatnya setiap kali login.'
				}
			]
		},
		{
			id: 'first-website',
			title: '4. Membuat Website Pertama',
			blocks: [
				{ type: 'p', text: 'Situs Web adalah jantung panel. Satu entri di sini berarti satu domain yang dihosting di server, dan setiap situs mendapatkan user Linux, folder, serta pengaturannya sendiri.' },
				{
					type: 'steps',
					items: [
						'Arahkan dulu domain Anda ke server: buat DNS record A (atau AAAA untuk IPv6) yang mengarahkan domain ke alamat IP server. Pemasangan SSL membutuhkan ini.',
						'Buka Situs Web dan isi formulir Create Website: domain Anda (misalnya `contoh.com`), template, dan — untuk situs PHP — versi PHP.',
						'Pilih template. Static HTML untuk file biasa, Native PHP untuk proyek PHP klasik, CodeIgniter 3/4, Laravel (pilih Blade, Inertia, atau Livewire), Laravel Octane di atas FrankenPHP, WordPress, atau runtime aplikasi: Node.js, Go, Python, Deno, atau Bun.',
						'Beberapa template menyediakan pilihan varian proyek (proyek kosong atau starter kit) dan apakah panel hanya menulis konfigurasi Nginx atau juga memasang framework-nya untuk Anda.',
						'Tekan Create. Situs akan melewati beberapa tahap — pending, installing, configuring, validating — dan Anda bisa memantau log provisioning langsung di kartunya.',
						'Ketika badge berubah menjadi active, buka `https://domain-anda` di browser untuk melihat hasilnya.'
					]
				},
				{
					type: 'warning',
					text: 'Jika provisioning gagal, kartu menampilkan tombol Retry — perbaiki masalah yang dilaporkan (seringkali DNS belum mengarah) lalu tekan Retry. Situs yang di-suspend offline tetapi datanya disimpan; gunakan Suspend untuk menghentikan sementara tanpa menghapus file.'
				},
				{
					type: 'tip',
					text: 'Gunakan kotak pencarian dan chip filter status di atas daftar situs untuk menemukan situs dengan cepat saat Anda menghosting banyak situs.'
				}
			]
		},
		{
			id: 'manage-site',
			title: '5. Mengelola Website',
			blocks: [
				{ type: 'p', text: 'Tekan Manage pada kartu situs untuk membuka halaman detailnya. Semua yang berkaitan dengan situs tersebut ada di sini, dibagi dalam tab-tab:' },
				{
					type: 'table',
					headers: ['Tab', 'Kegunaannya'],
					rows: [
						['Overview', 'Status situs, pemilik, health check, runtime per situs.'],
						['Deployment', 'Deploy kode dari Git atau arsip yang diunggah.'],
						['SSL', 'Memasang dan memperbarui sertifikat HTTPS.'],
						['Commands', 'Presets perintah sekali klik dan editor .env.'],
						['PHP Settings', 'Batas PHP per situs (tersembunyi untuk situs statis).'],
						['App', 'Start/stop dan build runtime aplikasi (Node.js, Go, Python, Deno, Bun).'],
						['WP Toolkit', 'Pembaruan WordPress core, plugin, dan tema.'],
						['Cron Jobs', 'Tugas terjadwal untuk situs ini.'],
						['Files', 'File manager lengkap dengan editor dan unggah.'],
						['Terminal', 'Shell di dalam folder situs ini.'],
						['Logs', 'Log access, error, dan Octane.'],
						['Config', 'Editor vhost Nginx dan pilihan template.'],
						['Domains', 'Domain alias dan subdomain.'],
						['Queue', 'Queue worker Laravel (hanya situs Laravel).']
					]
				},
				{
					type: 'tip',
					text: 'Tab yang terbuka diingat di alamat web (`?tab=...`), jadi Anda bisa membookmark tab tertentu dari sebuah situs.'
				}
			],
			subsections: [
				{
					id: 'deploy',
					title: 'Deploy kode Anda',
					blocks: [
						{ type: 'p', text: 'Tab Deployment membawa kode Anda ke server. Ada dua cara:' },
						{
							type: 'steps',
							items: [
								'Git: pilih penyedia Anda (GitHub, GitLab, Bitbucket, atau URL kustom), tekan Generate deploy key, lalu tambahkan public key yang muncul ke repositori Anda sebagai deploy key. Setelah itu isi repositori dan branch, lalu tekan Deploy Now.',
								'Arsip: unggah `.zip` atau `.tar.gz` proyek Anda lewat Upload & Extract. Panel mengekstraknya ke folder situs.'
							]
						},
						{ type: 'p', text: 'Setiap deploy tersimpan di daftar riwayat beserta lognya, jadi Anda selalu bisa memeriksa apa yang dideploy dan kapan. Lebih suka cara manual? Masuk lewat Terminal situs dan tarik kode sendiri.' }
					]
				},
				{
					id: 'app',
					title: 'Runtime aplikasi (Node.js, Go, Python, Deno, Bun)',
					blocks: [
						{ type: 'p', text: 'Situs yang bukan PHP biasa berjalan sebagai layanan kecil di latar belakang (unit systemd) yang mendengarkan di port privat, dengan Nginx meneruskan trafik pengunjung ke sana. Tab App menampilkan status layanan, port-nya, serta tombol Start / Stop / Restart.' },
						{
							type: 'list',
							items: [
								'Node.js — berjalan lewat versi NVM milik situs sendiri; pilih versinya saat membuat situs.',
								'Go — dibangun dari source di server, atau memakai binary jadi yang Anda unggah.',
								'Python — panel membuat virtualenv (venv) untuk situs dan memasang `requirements.txt` Anda.',
								'Deno dan Bun — dipasang dengan versi terkunci di server dan dipakai untuk menjalankan file entry Anda.'
							]
						},
						{ type: 'p', text: 'Perintah start bisa diedit. Setelah mengubah kode, tekan Build & restart agar kode baru aktif. Jika aplikasi gagal start, log provisioning atau log layanan memberi tahu penyebabnya.' }
					]
				},
				{
					id: 'ssl',
					title: 'Sertifikat SSL (HTTPS)',
					blocks: [
						{ type: 'p', text: 'Tab SSL menerbitkan sertifikat Let\'s Encrypt gratis. Pilihan paling umum adalah Let\'s Encrypt (HTTP), yang membuktikan kepemilikan domain lewat HTTP — domain harus sudah mengarah ke server ini.' },
						{
							type: 'steps',
							items: [
								'Pastikan DNS domain (dan alias-nya) sudah mengarah ke server.',
								'Di tab SSL pilih Let\'s Encrypt, pilih domainnya, lalu tekan terbitkan.',
								'Sertifikat terpasang dan diatur perpanjangan otomatis. Setiap sertifikat menampilkan tanggal kedaluwarsa dengan kode warna, lengkap dengan sakelar auto-renew yang bisa dimatikan.'
							]
						},
						{ type: 'p', text: 'Pilihan lainnya: Wildcard (`*.contoh.com`) memakai validasi DNS dan butuh API token Cloudflare; Custom memungkinkan Anda menempelkan sertifikat dan key dari tempat lain.' },
						{
							type: 'warning',
							text: 'Jika penerbitan gagal, sembilan dari sepuluh kasus penyebabnya DNS belum mengarah ke server, atau perubahannya belum menyebar. Tunggu DNS ter-resolve lalu coba lagi.'
						}
					]
				},
				{
					id: 'files',
					title: 'File manager',
					blocks: [
						{ type: 'p', text: 'Tab Files adalah file manager lengkap untuk folder situs — tanpa perlu FTP. Jelajahi lewat breadcrumb, buat folder dan file, ganti nama, hapus, unduh, dan unggah lewat tombol atau drag & drop (unggahan menampilkan antrean progres).' },
						{ type: 'p', text: 'Klik file teks untuk menyuntingnya di editor bawaan. Tekan `Ctrl+S` untuk menyimpan; editor memberi peringatan sebelum ditutup saat ada perubahan yang belum disimpan.' }
					]
				},
				{
					id: 'cron',
					title: 'Tugas cron',
					blocks: [
						{ type: 'p', text: 'Cron job menjalankan perintah secara otomatis sesuai jadwal — misalnya `php artisan schedule:run` setiap menit di Laravel, atau skrip pembersihan setiap malam. Pengelolaannya per situs di tab Cron Jobs.' },
						{
							type: 'steps',
							items: [
								'Tekan create, tulis perintahnya, lalu pilih preset jadwal (setiap menit, per jam, harian, mingguan, bulanan) atau ketik ekspresi cron kustom.',
								'Simpan. Gunakan sakelar untuk menghentikan sementara sebuah job tanpa menghapusnya.',
								'Edit atau hapus entri kapan saja dari daftarnya.'
							]
						}
					]
				},
				{
					id: 'php',
					title: 'Pengaturan PHP dan queue worker',
					blocks: [
						{ type: 'p', text: 'PHP Settings menimpa batas-batas untuk situs ini saja: memory limit, ukuran unggahan, waktu eksekusi, dan lainnya — berguna saat satu situs butuh batas unggah lebih besar daripada situs lain di server.' },
						{ type: 'p', text: 'Di situs Laravel, tab Queue mengelola queue worker sebagai layanan sungguhan: tentukan jumlah worker, tekan create, lalu start/stop/restart. Status dan 100 baris log terakhir ditampilkan langsung, sehingga antrean yang macet mudah didiagnosis.' }
					]
				},
				{
					id: 'wp',
					title: 'WordPress Toolkit',
					blocks: [
						{ type: 'p', text: 'Untuk situs WordPress, tab ini menampilkan versi core dan apakah pembaruan core, plugin, atau tema sedang menunggu. Setiap aksi adalah tugas sekali klik yang dijalankan aman sebagai user situs:' },
						{
							type: 'list',
							items: [
								'Update core — memperbarui WordPress itu sendiri.',
								'Update all plugins / Update all themes — memperbarui semua ekstensi.',
								'Update database — menjalankan migrasi database setelah update core.',
								'Flush cache — membersihkan cache WordPress.'
							]
						},
						{
							type: 'tip',
							text: 'Buat cadangan (lihat bagian Cadangan) sebelum menjalankan pembaruan WordPress besar — kalau ada yang rusak, pemulihan hanya butuh beberapa menit.'
						}
					]
				},
				{
					id: 'misc',
					title: 'Tab-tab lainnya',
					blocks: [
						{
							type: 'list',
							items: [
								'Commands — presets sekali klik untuk git, Composer, Artisan, dan npm/yarn/pnpm, dengan konfirmasi sebelum perintah berisiko dijalankan. Juga editor `.env` untuk Laravel, mode key/value atau mentah.',
								'Config — sunting vhost Nginx secara manual, atau pilih template: Auto, Generic PHP, Laravel, CodeIgniter 3/4, atau Static. Situs berbasis aplikasi otomatis memakai konfigurasi reverse-proxy.',
								'Domains — tambahkan domain alias atau subdomain yang berbagi situs ini; domain yang baru biasanya butuh sertifikat SSL sendiri.',
								'Logs — log access dan error (plus log Octane di situs Laravel Octane).',
								'Terminal — shell yang dimulai di dalam folder situs ini sebagai user situs tersebut.',
								'Overview — atur URL health check dengan status HTTP yang diharapkan lalu tekan periksa sekarang untuk menguji; administrator juga bisa memindahkan kepemilikan situs ke user panel lain.'
							]
						}
					]
				}
			]
		},
		{
			id: 'php',
			title: '6. Versi PHP',
			blocks: [
				{ type: 'p', text: 'Halaman PHP mengelola versi PHP yang terpasang di server (8.1 sampai 8.4). Sebuah versi harus dipasang di sini sebelum bisa dipakai oleh website.' },
				{
					type: 'list',
					items: [
						'Install — mengunduh dan memasang versi PHP tersebut sebagai tugas latar belakang dengan progres.',
						'Restart — me-restart layanan FPM setelah perubahan konfigurasi.',
						'Uninstall — menghapus versi tersebut (situs yang masih memakainya akan berhenti bekerja).',
						'Editor php.ini — mode Simple menampilkan batas-batas umum (memori, ukuran unggahan, waktu eksekusi, tampilan error); mode Advanced adalah editor mentah satu file penuh untuk pengaturan yang jarang.'
					]
				},
				{
					type: 'tip',
					text: 'Penimpaan per situs ada di situsnya sendiri (tab PHP Settings). Ubah php.ini global hanya jika pengaturan itu harus berlaku untuk semua situs PHP.'
				}
			]
		},
		{
			id: 'nodejs',
			title: '7. Aplikasi Node.js',
			blocks: [
				{ type: 'p', text: 'Halaman Node.js menampilkan situs web mana yang memiliki runtime Node.js terpasang dan memungkinkan Anda membuat aplikasi di atasnya: pilih situs, package manager (npm, yarn, atau pnpm), perintah build, argumen start, dan port tempat aplikasi mendengarkan.' },
				{
					type: 'list',
					items: [
						'Setiap aplikasi mendapat kontrol start / stop / restart, dan pemasangan yang lama berjalan sebagai task dengan progres.',
						'Setiap situs menyimpan versi Node sendiri (lewat NVM), jadi satu situs bisa di Node 20 sementara yang lain di Node 22.',
						'Jika server masih punya Node.js global lama, ada kartu pembuangannya — hapus hanya jika tidak ada aplikasi yang bergantung padanya.'
					]
				}
			]
		},
		{
			id: 'nginx',
			title: '8. Nginx',
			blocks: [
				{ type: 'p', text: 'Nginx adalah web server yang menerima setiap pengunjung lalu meneruskannya ke situs Anda. Halaman Nginx menampilkan statusnya (terpasang, berjalan, versi, uji konfigurasi) dengan tombol Start, Stop, Restart, dan Reload.' },
				{
					type: 'list',
					items: [
						'Konfigurasi global — mode Simple untuk pengaturan umum (worker processes, ukuran unggahan maksimum, keepalive, gzip); mode Manual untuk file mentah.',
						'Tabel per situs — aktifkan/nonaktifkan sebuah situs, sunting vhost-nya langsung, atau hapus.',
						'Log — membaca log access dan error tanpa membuka terminal.'
					]
				},
				{
					type: 'warning',
					text: 'Selalu tekan Test Config setelah menyunting pengaturan Nginx. Konfigurasi yang rusak bisa menghentikan semua situs sekaligus; pengujian menangkap salah ketik sebelum reload diterapkan.'
				}
			]
		},
		{
			id: 'databases',
			title: '9. Database',
			blocks: [
				{ type: 'p', text: 'Halaman Database mengelola MySQL, PostgreSQL, dan Redis. Setiap engine punya kartu untuk memasangnya (tugas latar belakang) serta menjalankan/menghentikan/me-restart-nya.' },
				{
					type: 'steps',
					items: [
						'Buat database: tekan create, pilih engine dan namanya (serta character set bila butuh yang khusus).',
						'Buat user database: hasilkan kata sandi kuat dengan generator bawaan (pilih panjang dan simbol), lalu berikan user itu hak akses ke database.',
						'Serahkan nama database, user, dan kata sandinya ke aplikasi Anda — di Laravel itu ada di file `.env` pada tab Commands situs.'
					]
				},
				{
					type: 'list',
					items: [
						'Export — unduh dump `.sql` atau `.sql.gz` terkompresi dari database mana pun.',
						'Restore — unggah file dump untuk mengisi ulang sebuah database (panel meminta konfirmasi karena isinya yang sekarang akan tertimpa).',
						'Alat user — salin kata sandi, reset kata sandi, atau hapus user.'
					]
				},
				{
					type: 'tip',
					text: 'Perlu melihat isi datanya? Pengelola database berbasis web (dibuka per user dari halaman database) bergaya phpMyAdmin: telusuri dan cari baris, lihat struktur tabel, jalankan SQL di konsol, dan ekspor CSV.'
				}
			]
		},
		{
			id: 'docker',
			title: '10. Docker',
			blocks: [
				{ type: 'p', text: 'Halaman Docker menjalankan beban kerja kontainer di samping situs biasa Anda. Pasang Docker daemon dari sini jika belum ada, lalu kendalikan dengan Start / Stop / Restart.' },
				{
					type: 'table',
					headers: ['Tab', 'Kegunaannya'],
					rows: [
						['Containers', 'Melihat daftar kontainer beserta statusnya, start/stop/restart/hapus, dan membaca 100 baris log terakhir.'],
						['Images', 'Menarik image dengan namanya dan menghapus image yang tak terpakai.'],
						['Volumes', 'Membuat dan menghapus volume data yang persisten.'],
						['Networks', 'Membuat dan menghapus jaringan (yang bawaan dilindungi).'],
						['Compose', 'Mengarah ke path `docker-compose.yml` di server lalu menjalankan compose up atau down.']
					]
				}
			]
		},
		{
			id: 'server',
			title: '11. Pengaturan Server',
			blocks: [
				{ type: 'p', text: 'Halaman Server menyimpan pengaturan level mesin: informasi sistem, hostname, zona waktu (daftar yang bisa dicari), partisi disk, dan antarmuka jaringan.' },
				{
					type: 'steps',
					items: [
						'Mengubah port SSH: masukkan port baru. Panel bekerja dua fase — port baru diterapkan tetapi port lama tetap menerima koneksi, dan port lama hanya ditutup setelah Anda mengonfirmasi port baru berfungsi (finalize).',
						'Reboot: tersedia di bagian bawah dengan dialog konfirmasi. Situs yang berjalan akan hidup kembali otomatis begitu server menyala.'
					]
				},
				{
					type: 'warning',
					text: 'Panel menolak port SSH yang bisa mengunci Anda di luar — termasuk port panel itu sendiri. Tetap saja, ubah port SSH hanya saat Anda punya jalur lain untuk masuk jika terjadi masalah.'
				}
			]
		},
		{
			id: 'services',
			title: '12. Layanan',
			blocks: [
				{ type: 'p', text: 'Halaman Layanan mendaftar layanan yang dikelola (Nginx, versi PHP-FPM, engine database, dan lainnya) beserta statusnya: berjalan, berhenti, atau tidak terpasang, plus apakah aktif saat boot.' },
				{ type: 'p', text: 'Setiap baris punya tombol Start, Stop, dan Restart. Kalau Dasbor melaporkan layanan berhenti, di sinilah Anda menyalakannya kembali.' },
				{
					type: 'tip',
					text: 'Kartu Developer dependencies memasang atau memperbarui Composer — alat dependensi PHP yang banyak dibutuhkan framework saat deployment.'
				}
			]
		},
		{
			id: 'security',
			title: '13. Pusat Keamanan',
			blocks: [
				{ type: 'p', text: 'Pusat Keamanan mengumpulkan semua yang menjaga keamanan server di satu tempat, dibagi dalam tab-tab.' },
				{
					type: 'list',
					items: [
						'Overview — laporan keamanan: status firewall, AppArmor, pengaturan SSH, validitas konfigurasi Nginx, pembaruan keamanan OS yang tertunda. Setiap temuan membawa tingkat keparahan dan petunjuk perbaikannya.',
						'Setup (Safe Setup) — satu proses penguatan terpandu: pilih situs yang dilindungi, rentang IP manajemen, aktifkan Fail2ban, pasang pemindai malware dengan jadwal harian, dan nyalakan Traffic Guard. Ia menampilkan tinjauan (dry run) lebih dulu, lalu menerapkan semuanya sebagai tugas yang bisa dilanjutkan bila terputus.',
						'Fail2ban — melihat alamat mana yang sedang diblokir dan membuka blokirnya bila salah sasaran.',
						'Malware — memperbarui signature, menjalankan scan cepat atau scan seluruh website, dan mengelola file terkarantina (pulihkan atau hapus).',
						'Traffic — pembatasan laju per situs. Situs dimulai dalam mode Observe (hanya mengamati); setelah laporannya diperiksa, Anda konfirmasi untuk menegakkan batasnya. Kompatibel dengan situs di balik Cloudflare.',
						'Events — aliran kejadian keamanan, agar terlihat apa yang terjadi dan kapan.'
					]
				},
				{
					type: 'tip',
					text: 'Jalankan Safe Setup sekali di server yang masih baru — ia menerapkan pengaturan bawaan yang masuk akal untuk Fail2ban, pemindaian malware, dan pengamatan trafik dalam satu langkah.'
				}
			]
		},
		{
			id: 'firewall',
			title: '14. Firewall',
			blocks: [
				{ type: 'p', text: 'Halaman Firewall mengendalikan UFW — gerbang di depan server Anda. Saat aktif, hanya port dengan aturan ALLOW yang bisa dijangkau dari internet.' },
				{
					type: 'list',
					items: [
						'Tambah aturan — port, protokol (TCP/UDP), aksi (allow atau deny), IP sumber opsional untuk membatasi siapa yang boleh tersambung, dan komentar agar Anda di masa depan ingat alasannya.',
						'Hapus aturan — menghapusnya dari tabel. Menghapus aturan SSH akan memunculkan peringatan ekstra, karena itulah port tempat Anda terhubung.'
					]
				},
				{
					type: 'warning',
					text: 'Menonaktifkan firewall atau menghapus aturan allow SSH bisa memutus akses Anda ke server. Panel memperingatkan sebelum keduanya — baca peringatannya dan pastikan Anda punya jalan masuk lain (misalnya konsol penyedia VPS) sebelum melanjutkan.'
				}
			]
		},
		{
			id: 'users',
			title: '15. Pengguna & Peran',
			blocks: [
				{ type: 'p', text: 'Halaman Pengguna mengelola siapa yang bisa login ke panel. Setiap user memiliki username, email, kata sandi, dan peran yang menentukan menu apa yang terlihat. Opsional, user juga bisa mendapatkan akun SSH Linux dengan folder rumahnya sendiri.' },
				{
					type: 'list',
					items: [
						'Buat user — isi formulir dan pilih perannya; centang opsi SSH untuk sekalian membuat akun Linux-nya.',
						'Kunci SSH — buka pengelola kunci seorang user untuk menambah atau menghapus public key untuk login SSH/SFTP.',
						'Edit — ubah peran atau akun SSH di kemudian hari.',
						'Hapus — menghapus user panel berserta akun Linux dan folder rumahnya. Panel memperingatkan lebih dulu, dan situs milik user tersebut bisa dipindahkan lebih dulu.'
					]
				},
				{
					type: 'tip',
					text: 'Beri rekan kerja peran terbatas daripada berbagi kata sandi admin Anda — setiap aksi tercatat di Log Audit beserta nama usernya.'
				}
			]
		},
		{
			id: 'backups',
			title: '16. Cadangan',
			blocks: [
				{ type: 'p', text: 'Cadangan menyelamatkan Anda di hari yang buruk. Halaman Cadangan menampilkan kartu ringkasan (jumlah cadangan selesai, total ukuran, sisa disk, cadangan terakhir) dan semua urusan cadangan lainnya ada di sini.' },
				{
					type: 'steps',
					items: [
						'Tekan Backup now lalu pilih jenisnya: satu situs web, sebuah database, konfigurasi panel, atau semuanya (full).',
						'Pilih targetnya bila Anda menghosting beberapa situs atau database.',
						'Cadangan berjalan sebagai tugas; setelah selesai ia muncul di tab Cadangan beserta ukuran dan statusnya.'
					]
				},
				{
					type: 'list',
					items: [
						'Restore — pilih cadangan dan bagian yang dipulihkan. Restore menimpa data yang sekarang, jadi panel meminta konfirmasi.',
						'Download — ambil file cadangannya untuk disimpan di luar server.',
						'Tab Schedules — otomatiskan: jenis, target, seberapa sering (preset atau cron kustom), dan aturan retensinya (simpan berapa hari, atau simpan N cadangan terakhir).',
						'Prune now — menjalankan pembersihan retensi seketika, menghapus cadangan yang sudah tidak diperlukan.'
					]
				},
				{
					type: 'tip',
					text: 'Cadangan yang disimpan di server yang sama ikut hilang bersama servernya. Di Pengaturan → penyimpanan remote, hubungkan bucket kompatibel S3 (AWS S3, Wasabi, Cloudflare R2, MinIO…) atau remote rclone — setiap cadangan baru otomatis disalin ke luar server dan ditandai badge off-site.'
				}
			]
		},
		{
			id: 'monitoring',
			title: '17. Pemantauan: Proses, Peringatan, Notifikasi',
			blocks: [
				{ type: 'p', text: 'Tiga halaman bersama-sama menunjukkan apa yang sedang dikerjakan server dan memperingatkan Anda sebelum semuanya rusak.' },
				{
					type: 'list',
					items: [
						'Proses — tabel langsung dari semua yang berjalan, bisa diurutkan berdasar CPU atau memori, menyegarkan tiap 5 detik bila diaktifkan. Kill menghentikan proses yang hilang kendali; Anda memilih sinyalnya (SIGTERM meminta dengan sopan, SIGKILL memaksa).',
						'Peringatan — buat aturan seperti "CPU di atas 90% selama 5 menit" atau "sertifikat kedaluwarsa dalam 14 hari". Kejadian yang cocok muncul di riwayat peringatan, dan ditandai pulih setelah nilainya kembali normal.',
						'Notifikasi — mengantarkan peringatan ke manusia: email (SMTP), Telegram, Discord, atau webhook umum. Setiap channel punya tombol kirim uji, dan data rahasia disamarkan di daftarnya.'
					]
				},
				{
					type: 'tip',
					text: 'Set awal yang bagus: disk di atas 85%, layanan mana pun mati, dan SSL kedaluwarsa dalam 14 hari — diantar ke channel Telegram yang benar-benar Anda baca.'
				}
			]
		},
		{
			id: 'terminal',
			title: '18. Terminal',
			blocks: [
				{ type: 'p', text: 'Halaman Terminal adalah shell penuh di server, dari dalam browser. Hanya administrator yang bisa memakainya, karena ia dapat menyentuh apa pun di mesin.' },
				{ type: 'p', text: 'User biasa yang punya akses situs mendapat versi yang lebih aman: tab Terminal di sebuah website memulai shell di dalam folder situs itu, sebagai user situs tersebut — cukup untuk `composer`, `artisan`, `npm` dan kawan-kawan tanpa membuka sisa server.' }
			]
		},
		{
			id: 'settings',
			title: '19. Pengaturan',
			blocks: [
				{ type: 'p', text: 'Halaman Pengaturan mengumpulkan konfigurasi panel itu sendiri:' },
				{
					type: 'list',
					items: [
						'Bahasa — mengganti seluruh panel (termasuk panduan ini) antara Bahasa Indonesia dan English.',
						'Domain panel — menyajikan panel lewat HTTPS di domain Anda sendiri alih-alih alamat IP:8443, dengan sertifikat Let\'s Encrypt otomatis. Menghapusnya mengembalikan akses ke IP:8443.',
						'Autentikasi dua faktor — memperkuat login Anda dengan aplikasi TOTP (Google Authenticator, Aegis, 1Password…): pindai kode QR, masukkan kode 6 digit, selesai.',
						'Kunci SSH — jika akun Anda punya akun SSH, kelola public key Anda di sini (login kata sandi lalu dimatikan).',
						'Token API — membuat token bernama untuk skrip yang berbicara dengan API panel. Token hanya ditampilkan sekali — salin segera. Daftarnya menampilkan kapan tiap token terakhir dipakai.',
						'Penyimpanan cadangan remote — menghubungkan S3 atau rclone sebagai target luar untuk modul cadangan.',
						'Pengaturan lainnya — setiap key pengaturan yang dikenal tampil sebagai baris yang bisa disunting dengan waktu terakhir diubah; menyimpan hanya mengirim yang berubah.'
					]
				}
			]
		},
		{
			id: 'update',
			title: '20. Memperbarui Panel',
			blocks: [
				{ type: 'p', text: 'Halaman Pembaruan menjaga panel itu sendiri tetap mutakhir. Ia menampilkan versi Anda dibanding versi terbaru yang dipublikasikan, dengan badge Pembaruan tersedia atau Sudah mutakhir serta tombol Periksa lagi.' },
				{
					type: 'steps',
					items: [
						'Tekan Update now dan konfirmasi. Panel menarik source terbaru, membangun ulang, menukar binary, lalu me-restart dirinya.',
						'Seluruh prosesnya memakan waktu sekitar dua sampai lima menit; situs tetap berjalan, tetapi panel sesaat tidak bisa dijangkau.',
						'Setelah selesai, halaman memuat ulang ke versi baru.'
					]
				},
				{
					type: 'warning',
					text: 'Selalu perbarui lewat halaman ini — jangan pernah menarik kode atau menjalankan build manual di server, karena pembaruan berikutnya bisa bentrok dengan perubahan Anda.'
				}
			]
		},
		{
			id: 'audit',
			title: '21. Log Audit',
			blocks: [
				{ type: 'p', text: 'Setiap aksi penting di panel dicatat: siapa yang melakukannya, apa, kapan, dan dengan data apa. Halaman Log Audit adalah catatan itu.' },
				{
					type: 'list',
					items: [
						'Filter menurut tanggal dengan rentang cepat (hari ini, kemarin, 7 atau 30 hari terakhir) atau dari-sampai kustom.',
						'Cari teks bebas, atau persempit ke satu modul dengan filter modul.',
						'Klik sebuah baris untuk membuka detail dari apa persisnya yang diubah.'
					]
				},
				{
					type: 'tip',
					text: 'Ada yang rusak dan tak ada yang mengaku menyentuhnya? Filter log audit pada satu jam sebelum masalah muncul, lalu periksa entri terbarunya.'
				}
			]
		},
		{
			id: 'faq',
			title: '22. FAQ & Pemecahan Masalah',
			blocks: [
				{
					type: 'terms',
					items: [
						{ term: 'Saya kehilangan alamat panel setelah mengatur domain panel', def: 'Tanpa domain panel, panel merespons di `https://IP-SERVER:8443`. Menghapus domain panel di Pengaturan mengembalikan Anda ke alamat itu. Bila HTTPS di domain gagal, alamat IP:8443 tetap berfungsi.' },
						{ term: 'Situs baru terus pending atau provisioning gagal', def: 'Buka kartu situs dan baca log provisioning. Penyebab paling umum: DNS belum mengarah ke server, atau ada dependensi yang kurang dan ditampilkan di formulir pembuatan (beserta tautan pasangnya). Perbaiki penyebabnya lalu tekan Retry.' },
						{ term: 'Penerbitan SSL gagal', def: 'Hampir selalu karena DNS: domain harus mengarah ke server ini sebelum Let\'s Encrypt mau menerbitkan. Sertifikat wildcard tambahan butuh API token Cloudflare. Tunggu DNS menyebar lalu coba lagi.' },
						{ term: 'Aplikasi Node/Python/Go saya mengembalikan 502', def: 'Layanan aplikasinya tidak berjalan atau mendengarkan di port yang berbeda dari yang dikonfigurasi. Buka tab App di situs tersebut, periksa portnya, tekan Restart, lalu baca log layanannya.' },
						{ term: 'Website menampilkan halaman bawaan, bukan aplikasi saya', def: 'Template Nginx mungkin tidak cocok dengan jenis situsnya. Di tab Config situs, pilih template yang sesuai (Auto menangani ini untuk situs aplikasi), lalu uji konfigurasi Nginx di halaman Nginx.' },
						{ term: 'Unggahan gagal untuk file besar', def: 'Tiga batas bisa menjadi penyebab: `upload_max_filesize` PHP (tab PHP Settings situs), `client_max_body_size` Nginx (konfigurasi global Nginx), dan batas body aplikasi itu sendiri. Naikkan yang berlaku.' },
						{ term: 'Sebuah layanan berhenti', def: 'Dasbor menunjukkan yang mana. Buka Layanan, tekan Start, dan periksa lognya bila menolak bertahan menyala. Laporan Pusat Keamanan juga menandai layanan yang berulang kali gagal.' },
						{ term: 'Saya terkunci gara-gara firewall', def: 'Gunakan akses konsol/VNC dari penyedia hosting untuk masuk ke server, buka kembali akses SSH, lalu tambahkan lagi aturan allow-nya. Panel memperingatkan sebelum setiap aturan yang bisa menyebabkan ini.' },
						{ term: 'Ke mana perginya cadangan saya?', def: 'Periksa filter di tab Cadangan (jenis dan status) serta aturan retensi jadwal Anda — pruning menghapus cadangan yang melampaui retensi. Cadangan ber-badge off-site juga ada di penyimpanan S3/rclone Anda.' },
						{ term: 'Kode dua faktor selalu ditolak', def: 'Waktu di ponsel yang melenceng membuat kode TOTP salah. Aktifkan waktu otomatis di ponsel, atau matikan 2FA dari Pengaturan (dengan konfirmasi) lalu pasang lagi.' }
					]
				}
			]
		},
		{
			id: 'glossary',
			title: '23. Glosarium',
			blocks: [
				{
					type: 'terms',
					items: [
						{ term: 'Document root', def: 'Folder situs web yang disajikan Nginx kepada pengunjung, misalnya direktori `public` milik situs.' },
						{ term: 'Vhost (virtual host)', def: 'Blok konfigurasi Nginx untuk satu domain — memberi tahu Nginx folder mana (atau port aplikasi mana) yang menjawab domain itu.' },
						{ term: 'Reverse proxy', def: 'Konfigurasi ketika Nginx menerima pengunjung lalu meneruskan permintaannya ke aplikasi di port privat (dipakai situs Node.js, Go, Python, Deno, dan Bun).' },
						{ term: 'Let\'s Encrypt', def: 'Otoritas sertifikat gratis. Sertifikatnya berumur 90 hari dan diperpanjang otomatis oleh panel.' },
						{ term: 'HTTP-01 / DNS-01', def: 'Dua cara membuktikan kepemilikan domain untuk sertifikat: menyajikan file khusus lewat HTTP, atau menambah DNS record (diperlukan untuk wildcard).' },
						{ term: 'Cron', def: 'Penjadwal yang menjalankan perintah pada waktu tetap; ekspresi cron seperti `*/5 * * * *` berarti setiap 5 menit.' },
						{ term: 'Unit systemd', def: 'Layanan latar belakang yang dikelola Linux (tampil di halaman Layanan dan sebagai layanan App untuk situs non-PHP).' },
						{ term: 'Queue worker', def: 'Proses latar belakang yang mengeksekusi pekerjaan antrean (email, ekspor…) untuk framework seperti Laravel, agar permintaan web tetap cepat.' },
						{ term: 'NVM', def: 'Node Version Manager — memungkinkan setiap website memakai versi Node.js miliknya sendiri.' },
						{ term: 'venv', def: 'Lingkungan virtual Python — folder paket Python milik satu situs agar situs-situs tidak saling bentrok.' },
						{ term: 'Deploy key', def: 'Kunci SSH hanya-baca yang memberi penyedia Git akses ke tepat satu repositori, dipakai untuk deployment.' },
						{ term: 'UFW', def: 'Uncomplicated Firewall — firewall yang dikelola panel; tanpa aturan ALLOW, sebuah port tertutup dari internet.' },
						{ term: 'Fail2ban', def: 'Penjaga yang otomatis memblokir alamat IP yang berulang kali mencoba membobol (misalnya membobol SSH dengan menebak kata sandi).' },
						{ term: 'Retensi', def: 'Berapa lama cadangan disimpan (N hari, atau N cadangan terakhir) sebelum pruning menghapus yang lama.' },
						{ term: 'TOTP', def: 'Kata sandi sekali pakai berbasis waktu — kode 6 digit dari aplikasi authenticator yang dipakai untuk login dua faktor.' }
					]
				}
			]
		}
	]
};

export const guideContent: Record<Language, GuideDoc> = { en, id };

/** Unique DOM id for a section heading. */
export function sectionAnchor(sectionId: string): string {
	return `guide-section-${sectionId}`;
}

/** Unique DOM id for a subsection heading. */
export function subAnchor(sectionId: string, subId: string): string {
	return `guide-section-${sectionId}-${subId}`;
}
