package noderuntime

import (
	"strings"
	"testing"
)

func TestPanelBuildRuntimeIsSeparateAndPinned(t *testing.T) {
	command := PanelNPMCommand("run", "build")
	for _, want := range []string{"/var/lib/jenderal/.nvm/nvm-exec", "HOME=/var/lib/jenderal", "NODE_VERSION=24", "'npm' 'run' 'build'"} {
		if !strings.Contains(command, want) {
			t.Fatalf("panel command missing %s", want)
		}
	}
	bootstrap := PanelBootstrapShell()
	for _, want := range []string{nvmCommit, nvmArchiveSHA256, "/usr/bin/timeout", "'jenderal' '24' '/var/lib/jenderal'"} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("bootstrap missing %s", want)
		}
	}
	if strings.Contains(bootstrap, "apt-get") || strings.Contains(command, "/usr/bin/npm") {
		t.Fatal("panel build depends on global Node")
	}
}
