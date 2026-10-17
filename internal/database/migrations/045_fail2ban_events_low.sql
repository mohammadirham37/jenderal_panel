-- Fail2ban bans were recorded as high-severity "intrusion" events, which kept
-- the Security Center in "Needs Attention" for attacks that were already
-- blocked. A ban is protection working, so active ones become low-severity
-- "intrusion_blocked" records that no longer drive the overall condition.
UPDATE security_events
SET severity = 'low',
    category = 'intrusion_blocked',
    recommended_action = 'Fail2ban blocked this address automatically. No action is needed unless it is a trusted address; then unban it and add it to the management networks.'
WHERE component = 'fail2ban'
  AND fingerprint LIKE 'fail2ban:%'
  AND status IN ('open', 'acknowledged');
