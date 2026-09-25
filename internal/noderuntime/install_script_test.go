package noderuntime

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The nvm tarball download has no internal timeout; install.sh must bound the
// `nvm install` step itself or a stalled nodejs.org connection hangs the whole
// panel self-update.
func TestInstallScriptBoundsNodeDownload(t *testing.T) {
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(source)

	syntax := exec.Command("bash", "-n", "install.sh")
	syntax.Dir = "."
	if out, err := syntax.CombinedOutput(); err != nil {
		t.Fatalf("install.sh is not valid bash: %v: %s", err, out)
	}

	if !strings.Contains(script, `timeout --signal=TERM --kill-after=5s 600 bash -c`) {
		t.Error("install.sh must bound the nvm install step with a timeout")
	}
	if !strings.Contains(script, `failed or timed out downloading from nodejs.org`) {
		t.Error("install.sh must explain that the Node download can be retried")
	}
}
