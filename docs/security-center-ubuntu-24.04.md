# Security Center: Ubuntu 24.04 Operations Guide

This guide covers persistent task progress, security events, Safe-mode
Fail2ban, ClamAV website malware scanning, and HTTP-layer Traffic Guard. A
normal panel update does not install or enable either security package. An
administrator must explicitly start each installation from Security Center.

## Safe setup

The **Setup** tab provides an eight-part review flow for assessment, management
CIDRs, Fail2ban, malware, Traffic Guard Observe, proxy guidance, notifications,
and final Apply. It stores the review hash, task ID, and every completed step in
SQLite. If a later step fails or the panel restarts, use **Resume from
checkpoint**; already completed steps are not repeated.

1. Keep the VPS provider console open before testing SSH protection.
2. Open **Security Center → Fail2ban**.
3. Install Fail2ban and wait for the persistent task to complete.
4. Enter the public IP or CIDR used to administer the VPS. Loopback is always
   ignored automatically, but it does not protect a remote administrator.
5. Keep the Simple preset unless there is a measured reason to change it:
   five retries, a ten-minute observation window, and a fifteen-minute ban.
6. Select **Validate & Apply**. The panel validates a copy of the Fail2ban tree,
   atomically promotes only `/etc/fail2ban/jail.d/jenderal-panel.local`, reloads
   the service, and confirms the requested jail is active.

All Safe-mode bans are temporary. The panel does not alter `sshd_config`, the
SSH port, authentication methods, or UFW rules as part of Fail2ban setup.

The Overview posture check is read-only. It reports explicit `unknown` states
when an optional command is unavailable and provides guidance for UFW,
AppArmor, effective SSH settings, Nginx configuration, and Ubuntu security
updates. Its Good/Needs Attention/Critical label is an explainable summary, not
a security guarantee. Findings are reconciled every five minutes and resolved
findings produce at most one recovery notification when the original event was
notified.

## Task recovery behavior

The browser stores the active Security task ID. A full browser refresh resumes
polling the same task and displays its retained, bounded output. Task state is
also stored in SQLite. If the panel process restarts while work is running, the
interrupted task is restored as failed with the message `panel restarted before
task completed`; its existing output remains available and the operation can be
retried explicitly. The panel never guesses that interrupted privileged work
completed.

## Status checks

Run these from a provider console or trusted SSH session:

```bash
sudo systemctl status fail2ban --no-pager
sudo fail2ban-client status
sudo fail2ban-client status sshd
sudo fail2ban-client -t
sudo sed -n '1,160p' /etc/fail2ban/jail.d/jenderal-panel.local
```

The `sshd` jail should appear in the active jail list after a successful Safe
configuration. If the panel reports that a filter or source is unavailable,
resolve that prerequisite and retry; do not create an unvalidated replacement
file under the panel-owned filename.

## Lockout recovery

To release one address from the SSH jail:

```bash
sudo fail2ban-client set sshd unbanip <address>
```

To return Fail2ban to a stopped state without changing UFW:

```bash
sudo systemctl stop fail2ban
sudo systemctl status fail2ban --no-pager
sudo ufw status verbose
```

Stopping Fail2ban does not disable UFW and does not remove administrator-owned
firewall rules. Use the panel's Start action after correcting configuration.
If a candidate fails validation, reload, or health confirmation, the panel
restores the previous panel-owned file and attempts a recovery reload.

## Malware scanner setup

1. Open **Security Center → Malware** and choose **Install low-memory** for a
   small VPS. Daemon mode is offered only when the host has at least 2 GiB RAM.
2. Wait for the persistent installation task, then select **Update
   signatures**. Confirm that the signature age and updater service are green.
3. Run a Website Scan against a disposable test website before enabling the
   daily schedule. The Simple schedule is one Quick Scan per day at the VPS
   local time and only examines files changed since the prior successful Quick
   Scan.
4. Review every finding. Restore re-scans the retained sample and refuses an
   existing destination. False-positive allowlisting is scoped to the exact
   website, canonical path, and SHA-256. Permanent deletion always requires a
   separate confirmation.

The scanner does not follow symlinks or cross filesystem devices. It runs with
low CPU/I/O priority, a one-hour limit, a 100 MiB per-file limit, an archive
depth of 16, and a maximum of 100,000 candidate files. Samples are moved to
`/var/lib/jenderal/quarantine` with opaque names and are never removed by scan
history cleanup.

Useful provider-console checks:

```bash
sudo clamscan --version
sudo systemctl status clamav-freshclam --no-pager
sudo systemctl status clamav-daemon --no-pager
sudo ls -ld /var/lib/jenderal/quarantine
sudo find /var/lib/jenderal/quarantine -maxdepth 1 -type f -printf '%m %u:%g %f\n'
```

Use only the standard harmless EICAR antivirus test file from the official
EICAR site inside a disposable website. After detection, verify the original
URL no longer serves the file and the quarantine directory is outside every
Nginx document root. A restore attempt must remain blocked while ClamAV still
detects the sample.

## Advanced on-access mode

On-access scanning is disabled by default. It requires `clamav-daemon`,
`clamonacc`, and kernel `CONFIG_FANOTIFY`. The panel first enables notify-only
mode. Prevention additionally requires
`CONFIG_FANOTIFY_ACCESS_PERMISSIONS` and an explicit warning confirmation.
The panel owns only its marked block in `/etc/clamav/clamd.conf` and the
`jenderal-clamonacc.service` unit; validation or health failure restores the
previous files.

```bash
grep FANOTIFY /boot/config-"$(uname -r)"
sudo clamconf -n
sudo systemctl status jenderal-clamonacc.service --no-pager
sudo journalctl -u jenderal-clamonacc.service -n 100 --no-pager
```

## Traffic Guard and trusted proxies

Traffic Guard reads each panel-managed website's Nginx access log with a
persisted inode/offset cursor and stores bounded minute/hour aggregates. It is
an HTTP-layer detector and origin rate limiter, not volumetric DDoS protection.
Use a CDN or hosting/network provider when traffic must be absorbed before it
reaches the VPS.

1. Open **Security Center → Traffic** and select a website.
2. Keep **Observe** mode for at least 24 hours. It enables Nginx dry-run limits
   and does not reject requests.
3. Select **Direct to VPS** unless the origin really is behind a proxy. For
   Cloudflare, refresh the official CIDRs before applying. For a custom proxy,
   enter only exact proxy CIDRs and an allowlisted forwarded-IP header.
4. Review request/status history and top client/path evidence. After 24 hours,
   choose Balanced or Strict, acknowledge the HTTP 429 impact, then select
   **Validate & Apply**.
5. If legitimate traffic is affected, select **Return to Observe**. This is the
   primary recovery action and does not alter UFW.

The panel owns `/etc/nginx/conf.d/jenderal-traffic-zones.conf`, optional bounded
custom-zone files, and `/etc/nginx/jenderal/security/sites/<website-id>.conf`.
Every change is atomically staged, checked with `nginx -t`, reloaded, and
health-confirmed. A failed candidate restores the exact previous managed file.
Cloudflare refresh requires HTTPS, validates both IPv4 and IPv6 lists, and
keeps the last valid snapshot after a failure.

Useful provider-console checks:

```bash
sudo nginx -t
sudo systemctl is-active nginx
sudo sed -n '1,160p' /etc/nginx/conf.d/jenderal-traffic-zones.conf
sudo find /etc/nginx/jenderal/security/sites -maxdepth 1 -type f -print
sudo grep -R "jenderal/security/sites" /etc/nginx/sites-enabled
```

When testing a trusted proxy, verify that an untrusted direct peer cannot spoof
the selected forwarded-IP header. Rotate a disposable website's access log,
restart the panel, and confirm aggregation continues without replaying old
bytes. Exercise enforcement from a disposable client and confirm excess
requests return 429 while normal traffic remains available.

## Disposable Ubuntu 24.04 verification record

Complete this section on a disposable VPS before declaring the live rollout
verified. Do not record public IPs, credentials, tokens, or complete auth logs.

| Evidence | Result |
| --- | --- |
| Panel commit | Pending live verification |
| Ubuntu release (`lsb_release -ds`) | Pending |
| Fail2ban version (`fail2ban-client --version`) | Pending |
| Firewall backend | Pending |
| Test timestamp (UTC) | Pending |
| Install task survives browser refresh | Pending |
| `sshd` jail active | Pending |
| Non-allowlisted test source temporarily banned | Pending |
| Panel unban succeeds | Pending |
| Interrupted task output restored as failed/retryable | Pending |
| ClamAV version and signature timestamp | Pending |
| Low-memory scan task survives browser refresh | Pending |
| EICAR test file moved outside document root | Pending |
| Quarantine file mode is `0600` and directory mode is `0700` | Pending |
| Quarantined sample is not reachable over HTTP | Pending |
| Restore refuses a still-detected or conflicting destination | Pending |
| Daily scheduler starts no more than once per local date | Pending |
| Optional on-access capability/rollback check | Pending |
| Traffic Guard Observe remains non-blocking for 24 hours | Pending |
| Access-log cursor survives rotation and panel restart | Pending |
| Direct client IP cannot be overridden by an untrusted header | Pending |
| Cloudflare/custom trusted proxy reports the expected client IP | Pending |
| Failed Cloudflare refresh retains the previous CIDRs | Pending |
| Balanced/Strict excess request receives HTTP 429 | Pending |
| Failed Nginx candidate restores the previous snippet | Pending |
| One-click Return to Observe restores non-blocking mode | Pending |
| Upstream volumetric DDoS protection documented/configured separately | Pending |

For the lockout test, keep one provider console session open throughout. Use a
separate, disposable source address that is not in the management allowlist.
Confirm the ban expires automatically and never test against the only available
administrative path.
