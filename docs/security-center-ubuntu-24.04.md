# Security Center: Ubuntu 24.04 Operations Guide

This guide covers the first deployable Security Center slice: persistent task
progress, security events, and Safe-mode Fail2ban management. The panel does
not install or enable Fail2ban during a normal panel update. An administrator
must explicitly start installation from **Security Center → Fail2ban**.

## Safe setup

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

For the lockout test, keep one provider console session open throughout. Use a
separate, disposable source address that is not in the management allowlist.
Confirm the ban expires automatically and never test against the only available
administrative path.
