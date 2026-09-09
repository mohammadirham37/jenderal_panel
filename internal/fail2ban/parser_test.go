package fail2ban

import (
	"strings"
	"testing"
)

func TestParseJailStatusAcceptsSpacingVariants(t *testing.T) {
	output := `Status for the jail: sshd
|- Filter
|  |- Currently failed: 2
|  |- Total failed:     17
|  ` + "`" + `- File list: /var/log/auth.log
` + "`" + `- Actions
   |- Currently banned:   1
   |- Total banned: 4
   ` + "`" + `- Banned IP list: 203.0.113.7`
	jail, err := ParseJailStatus("sshd", output)
	if err != nil {
		t.Fatal(err)
	}
	if jail.CurrentlyFailed != 2 || jail.TotalFailed != 17 || jail.CurrentlyBanned != 1 || jail.TotalBanned != 4 {
		t.Fatalf("jail = %#v", jail)
	}
	if len(jail.BannedIPs) != 1 || jail.BannedIPs[0] != "203.0.113.7" {
		t.Fatalf("banned IPs = %#v", jail.BannedIPs)
	}
}

func TestParseJailStatusRejectsMalformedBanCount(t *testing.T) {
	_, err := ParseJailStatus("sshd", "Currently banned: many\nTotal banned: 2")
	if err == nil || !strings.Contains(err.Error(), "currently banned") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseGlobalStatusFindsOnlyAllowlistedJails(t *testing.T) {
	jails := ParseActiveJails("Status\n |- Number of jail: 3\n `- Jail list: sshd, nginx-limit-req, ../../bad")
	if len(jails) != 2 || jails[0] != "nginx-limit-req" || jails[1] != "sshd" {
		t.Fatalf("jails = %#v", jails)
	}
}
