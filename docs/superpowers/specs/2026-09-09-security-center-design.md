# Security Center Design

**Date:** 2026-09-09

## Goal

Add an approachable network and server security center to Jenderal-Panel for
Ubuntu 24.04. The first release integrates mature operating-system tools rather
than implementing a firewall, intrusion prevention engine, or malware scanner
inside the panel.

The default policy is **Safe**:

- Fail2ban may automatically create temporary bans for confirmed brute-force
  behavior.
- HTTP traffic protection starts in observation and dry-run mode.
- Malware is quarantined, never deleted automatically.
- Every privileged action is validated, recoverable, and audited.

## Scope

The first release contains:

- A new `/security` Security Center and guided setup wizard.
- Fail2ban installation, health, safe configuration, jail visibility, and
  temporary ban management.
- ClamAV signature status, manual and scheduled website scans, quarantine, and
  safe restore.
- HTTP traffic aggregation, anomaly events, Nginx request/connection limiting,
  and explicit trusted-proxy handling.
- Read-only posture checks for UFW, AppArmor, security services, SSL, and
  security updates.
- A unified security-event lifecycle connected to the existing audit, alert,
  notification, and persistent-task systems.

The work is split into four independently verifiable subprojects:

1. Security Center foundation and Fail2ban.
2. ClamAV scanning and quarantine.
3. Traffic Guard, Nginx limiting, and trusted proxies.
4. Posture checks, integrated notifications, Ubuntu 24.04 verification, and UX
   hardening.

## Architecture

The panel acts as an orchestrator around system packages:

```text
Security Center
 |-- Fail2ban adapter
 |-- ClamAV adapter
 |-- Traffic Guard adapter
 |-- Posture checker
 `-- Security event service
          |-- persistent tasks
          |-- audit log
          |-- alert history
          `-- notification delivery
```

The backend packages are separated by responsibility:

- `internal/security` owns the overview, setup state, posture checks, event
  lifecycle, and shared policy.
- `internal/fail2ban` owns package/service discovery, panel-managed jail
  configuration, status parsing, bans, and unbans.
- `internal/malware` owns ClamAV discovery, signatures, scans, findings,
  quarantine, and restore.
- `internal/trafficguard` owns access-log aggregation, baselines, anomaly
  evaluation, trusted proxies, and panel-managed Nginx limit configuration.

All external commands use a fixed command runner with structured arguments and
bounded execution. API input cannot supply arbitrary shell text.

Installation, scanning, configuration changes, and repairs use the existing
persistent task infrastructure. Their progress and final logs therefore remain
visible after a page refresh, logout/login, or frontend reconnection.

## Installation and Ownership

Security components are not silently installed or enabled during a panel
update. The user explicitly runs the setup wizard, reviews the proposed
packages and configuration changes, and then applies them.

Panel-owned configuration is isolated from administrator-owned configuration:

- Fail2ban uses `/etc/fail2ban/jail.d/jenderal-panel.local` and any required
  filters use clearly named Jenderal-Panel files.
- Nginx traffic directives are kept in panel-owned snippets and included only
  for websites that enable Traffic Guard.
- ClamAV distribution configuration remains authoritative; panel overrides are
  added only where required for bounded scanning or optional on-access mode.

Before promotion, a candidate configuration is written to a temporary file and
validated with the component's native validator. Promotion is atomic where the
filesystem allows it. The prior valid content is restored if validation,
reload, or post-reload health checking fails. A repair action reconciles only
panel-owned files and does not overwrite unrelated manual configuration.

Repeated installation, setup, update, and repair operations are idempotent.

## Security Center UX

The `/security` overview displays:

- A condition of Good, Needs Attention, or Critical, always accompanied by the
  reasons that produced it. It is not presented as a security guarantee.
- Fail2ban, Malware Scanner, Traffic Guard, UFW, AppArmor, ClamAV signatures,
  and notification-channel status.
- Active incidents and recommended actions.
- Last posture check, last scan, and current persistent tasks.

The guided setup performs these steps:

1. Inspect Ubuntu version, CPU, memory, disk, Nginx, logs, kernel capability,
   SSH port, UFW, installed packages, and notification channels.
2. Let the user confirm management IP/CIDR allowlists.
3. Install and configure Fail2ban.
4. Select a resource-appropriate malware scanning schedule.
5. Enable Traffic Guard in Observe Mode.
6. Configure Direct, Cloudflare, or Custom Proxy addressing per website.
7. Test one or more notification channels.
8. Present a final diff-style summary before applying the setup.

Each component provides two editing levels:

- **Simple** exposes the Safe preset, clear toggles, status, recommendations,
  and repair actions.
- **Advanced** exposes bounded component-specific settings and configuration
  previews, but never a shell command field.

## Fail2ban

### Discovery and setup

The adapter detects the installed Fail2ban version, service unit, firewall
backend, available filters, and usable log or journal sources. It enables a jail
only when the required filter and source are actually present. Unsupported
jails are shown as unavailable with a reason.

The Safe SSH profile uses:

- five failed attempts;
- a ten-minute observation window;
- a fifteen-minute temporary ban;
- no permanent bans;
- incremental ban duration only when the installed version supports it.

Nginx jails are offered only after their filters and log inputs pass discovery.
Custom Jenderal-Panel filters require positive and negative fixture tests before
they can be enabled.

### Lockout prevention

- Loopback is always ignored.
- The setup wizard recommends the directly observed administrator address but
  does not trust a forwarded header unless its proxy is configured as trusted.
- The user may add validated management IPs or CIDRs.
- Enabling the SSH jail without a management allowlist produces a prominent
  warning and recovery instructions.
- The configured SSH port is checked against UFW before Fail2ban activation.
- The panel does not change the SSH port, authentication method, or sshd
  configuration.
- Automatic bans always expire.

Before activation, the backend runs `fail2ban-client -t`. It then reloads the
service and confirms that the intended jails are active. Failure at any stage
restores the prior panel-managed configuration.

### Operations

The UI provides service controls, jail status, failure and ban counts, current
bans, ban reason, start and expected expiry, manual temporary ban, and unban.
All manual actions require validation and are written to the audit log.

## Malware Scanner

### Runtime selection

The default is scheduled and manual scanning of panel-managed website roots.
The resource check selects between:

- `clamscan` for low-memory VPS instances and infrequent scanning; or
- `clamdscan` when the daemon can be supported and repeated scans benefit from
  a resident signature database.

On-access scanning is an Advanced option. It is disabled by default because it
requires `clamd` and `clamonacc`, kernel capabilities, careful permission and
exclude rules, and can materially affect frequently accessed directories.

`freshclam` supplies signature updates. The panel displays signature version,
age, update-service health, and the last update error.

### Scan modes

- Quick Scan examines files created or changed since the last successful scan.
- Website Scan targets one or more selected websites.
- Full Website Scan covers every website root managed by the panel.
- Custom Scan accepts only validated paths within configured safe roots.

The Safe schedule runs one low-priority Quick Scan daily. Only one malware scan
may run at once. CPU/I/O priority, file size, archive depth, scan duration, and
file-count limits protect small VPS instances from resource exhaustion.

Scans do not follow symbolic links outside the selected root or cross into
external mounts. Sockets and device files are never scanned. Progress contains
the current phase, files examined, findings, elapsed time, and a bounded log.

### Quarantine

Detected files are treated as suspected malware and moved to a root-only
quarantine outside every Nginx document root. The storage key is generated by
the panel rather than derived from the original filename.

Each item records:

- SHA-256 hash and ClamAV signature;
- website and original canonical path;
- original owner, group, mode, size, and detection time;
- scan and security-event identifiers;
- quarantine, restored, deleted, or false-positive state.

Quarantine never deletes automatically and is excluded from retention cleanup.
Restore requires `security.quarantine`, explicit confirmation, path-containment
validation, and a fresh scan. It refuses to overwrite an existing destination.
Permanent deletion is a separate confirmed action. Allowlisting requires both
hash and scoped path; filename-only allowlisting is not supported.

## Traffic Guard

### Capability boundary

Traffic Guard detects and reduces HTTP-layer abuse reaching the VPS. It does
not claim to stop a volumetric DDoS attack that has saturated the VPS or its
network link. The UI directs users to their CDN, ISP, or hosting provider for
upstream volumetric protection.

The collector derives bounded aggregates from existing Nginx access logs:

- requests per second and minute;
- concurrently processed connections when supported;
- 401, 403, 404, 429, and 5xx rates;
- leading client IPs, domains, paths, and user agents;
- rate/connection-limit outcomes;
- Nginx failures and correlated CPU, memory, and load pressure.

The panel persists aggregates and security events, not a second unbounded copy
of raw access logs.

### Observe and enforcement lifecycle

Traffic Guard starts in Observe Mode for at least 24 hours. No request is
rejected in that period. A baseline is considered usable only after enough
valid samples exist; low-sample conditions use conservative fixed guidance and
are labeled as such.

The panel offers:

- Observe: detection and dry-run counters only.
- Balanced: conservative request and connection limiting.
- Strict: lower limits with a clear false-positive warning.
- Custom: validated Advanced values.

Nginx `limit_req_dry_run` and `limit_conn_dry_run` are used before enforcement.
The UI previews measured traffic that would have been limited. Enabling
enforcement requires confirmation and returns excess requests with HTTP 429.

Safe mode never creates an automatic UFW ban from a traffic anomaly. Optional
temporary IP bans are Advanced, disabled by default, time bounded, and subject
to the same trusted-client-address rules as Fail2ban. A recovery action returns
all panel-managed limits to Observe Mode.

### Trusted proxies

Client IP selection is configured per website:

- Direct uses the Nginx peer address.
- Cloudflare accepts `CF-Connecting-IP` only from current validated Cloudflare
  network CIDRs.
- Custom Proxy/CDN requires explicit CIDRs and an allowlisted supported header.

Forwarded address headers are never trusted from a peer outside the configured
CIDRs. The backend checks for the Nginx real-IP module, validates all CIDRs,
tests the candidate configuration with `nginx -t`, and confirms reload health.
Fail2ban, access logs, baselines, and rate limiting consume the same normalized
client address.

Cloudflare CIDRs are fetched from the official source on a bounded schedule.
Updates are syntax checked and must contain valid non-empty IPv4 and IPv6 sets.
An update failure retains the last valid set rather than emptying or partially
replacing it. The UI warns when a proxied website's origin still appears to be
directly reachable, but does not automatically close firewall access.

## Server Posture

The first release observes and explains posture; it does not silently harden
the machine. Checks include:

- UFW state and externally exposed ports;
- AppArmor availability, loaded state, and enforce/complain/unconfined counts;
- Fail2ban, ClamAV update, and Nginx service health;
- malware signature freshness;
- Nginx configuration and managed-site SSL health;
- available Ubuntu security updates;
- detectable risky SSH settings such as root login or password authentication.

Remediation is offered only when a safe, component-owned action exists. Risky
SSH settings initially produce guidance rather than automatic mutation.

## Security Events and Notifications

Fail2ban, Malware Scanner, Traffic Guard, and Posture checks emit a shared event
shape containing category, severity, component, resource, evidence summary,
recommended action, timestamps, and action metadata.

The lifecycle is:

- Open
- Acknowledged
- Resolved
- False Positive

Equivalent occurrences are folded into the same open event during a bounded
deduplication window. The event records occurrence counts and first/last seen
times. Critical events notify immediately. Repeated events respect a cooldown
so channels are not flooded. Recovery may generate one resolved notification.

Notification delivery uses the existing email, Telegram, Discord, and webhook
channels. Audit records cover installs, configuration changes, enforcement,
bans, unbans, quarantine, restore, deletion, allowlist changes, acknowledgement,
and resolution.

## Persistence and Retention

Additive migrations store:

- security settings and setup state;
- security events and repeated occurrences;
- malware scans and findings;
- quarantine metadata;
- traffic aggregates and baseline state;
- trusted-proxy configuration per website;
- the last successfully applied panel-managed configuration revision.

Default retention is:

- per-minute traffic aggregates: 24 hours;
- per-hour traffic aggregates: 30 days;
- security events and scan history: 90 days;
- quarantine items: no automatic deletion.

Advanced settings may choose bounded alternative retention periods. Cleanup is
incremental to avoid long database locks.

## Authorization and API Rules

New permissions are:

- `security.view` for overview, posture, event, scan-result, and status reads;
- `security.manage` for setup, component state, policy, ban, unban, and traffic
  enforcement;
- `security.quarantine` for quarantine download, restore, allowlist, and delete.

Endpoints live under `/api/v1/security`. Handlers use request DTOs rather than
binding database models directly. Domain, path, CIDR, header, jail, duration,
threshold, and identifier inputs are allowlisted or strictly parsed. Secrets,
malware file contents, and unbounded raw command output are not included in
normal API responses or application logs.

## Error Handling and Recovery

- Errors identify the component, phase, safe command label, exit code, bounded
  stderr, and recommended action without exposing credentials.
- Failed package or signature downloads have explicit timeouts and end in a
  retryable task state rather than an indefinitely running UI.
- A failed validator, reload, or health check triggers rollback and records a
  critical security event if the prior healthy state cannot be restored.
- The Fail2ban page provides console recovery commands and bulk temporary-ban
  inspection.
- Traffic Guard provides one action to return to Observe Mode.
- Quarantine restore is conflict safe and never overwrites an existing file.

## Verification

Automated tests cover:

- command argument construction and supported-version discovery;
- Fail2ban rendering, parsing, allowlists, jail availability, ban expiry,
  validation failure, and rollback;
- ClamAV output parsing, signature health, scan limits, path containment,
  symlink/mount handling, quarantine metadata, conflict-safe restore, and
  deletion authorization;
- access-log parsing, aggregation, baseline sufficiency, anomaly cooldown,
  Nginx limiting, dry-run/enforcement transitions, and rollback;
- direct, Cloudflare, and custom proxy selection, including forged forwarded
  headers, invalid CIDRs, failed CIDR refresh, and missing real-IP support;
- security-event deduplication, lifecycle, retention, notification cooldown,
  and audit coverage;
- persistent task visibility after frontend refresh and backend restart;
- Simple/Advanced UI behavior, setup summaries, confirmations, recovery, and
  actionable error states.

Integration verification runs on a disposable Ubuntu 24.04 VPS and includes:

- fresh install and repeated repair;
- live Fail2ban jail validation without losing SSH access;
- ClamAV signature update, standard harmless test detection, quarantine, and
  restore;
- normal traffic, legitimate bursts, abusive clients, proxy traffic, dry-run,
  and enforcement;
- low-resource and timeout behavior;
- notification delivery and event recovery;
- full Go tests, frontend tests, and production frontend build.

## Acceptance Criteria

- Safe setup can be completed without manually editing Linux configuration.
- Fail2ban preserves SSH access safeguards and all automatic bans expire.
- Malware findings are quarantined and are never automatically deleted.
- Traffic is not rejected before observation and explicit user confirmation.
- Untrusted forwarded headers cannot influence bans, limits, or event IPs.
- Configuration failure restores the last valid state whenever possible.
- Long-running progress and logs survive page refreshes and panel restarts.
- Privileged security actions are permission checked and audited.
- Install, update, repair, and repeated setup operations are idempotent.

## Deferred Work

Suricata or packet IDS, ModSecurity/WAF rule management, CrowdSec, container
image scanning, email gateway scanning, and volumetric DDoS mitigation are not
part of the first release. These need separate operational designs, resource
budgets, and failure policies. The shared event interface remains extensible so
they can be added later without replacing the first-release modules.

## References

- Fail2ban filter guidance: https://fail2ban.readthedocs.io/en/latest/filters.html
- ClamAV scanning: https://docs.clamav.net/manual/Usage/Scanning.html
- ClamAV on-access scanning: https://docs.clamav.net/manual/OnAccess.html
- Nginx request limiting: https://nginx.org/en/docs/http/ngx_http_limit_req_module.html
- Nginx connection limiting: https://nginx.org/en/docs/http/ngx_http_limit_conn_module.html
- Nginx real-IP module: https://nginx.org/en/docs/http/ngx_http_realip_module.html
- Ubuntu AppArmor: https://documentation.ubuntu.com/server/how-to/security/apparmor/index.html
- Cloudflare IP addresses: https://developers.cloudflare.com/fundamentals/concepts/cloudflare-ip-addresses/
- CISA DDoS guidance: https://www.cisa.gov/sites/default/files/publications/understanding-and-responding-to-ddos-attacks_508c.pdf
