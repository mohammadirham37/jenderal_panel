package ssl

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/challenge/http01"
)

// webrootProvider answers HTTP-01 challenges by writing the key
// authorization under <root>/.well-known/acme-challenge/ for nginx to
// serve.
//
// lego's own webroot provider creates the directories 0755 and the file
// 0644, which the process umask then narrows. The panel service runs with
// UMask=0077, so they came out 0700/0600, owned by the panel account:
// nginx (www-data) could not read them, Let's Encrypt got a 404, and every
// certificate request on a fresh install failed. Modes are set explicitly
// here, which the umask does not affect.
type webrootProvider struct{ root string }

func newWebrootProvider(root string) *webrootProvider { return &webrootProvider{root: root} }

// Present writes the challenge file and makes it readable to nginx.
func (p *webrootProvider) Present(domain, token, keyAuth string) error {
	dir := filepath.Join(p.root, filepath.Dir(http01.ChallengePath(token)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create challenge directory: %w", err)
	}
	// Every directory from the webroot down must be traversable by nginx.
	for d := dir; ; d = filepath.Dir(d) {
		if err := ensureWorldReadable(d, 0o755); err != nil {
			return err
		}
		if d == filepath.Clean(p.root) || d == filepath.Dir(d) {
			break
		}
	}
	file := filepath.Join(p.root, http01.ChallengePath(token))
	if err := os.WriteFile(file, []byte(keyAuth), 0o644); err != nil {
		return fmt.Errorf("write challenge file: %w", err)
	}
	return ensureWorldReadable(file, 0o644)
}

// ensureWorldReadable sets mode on path unless others can already read (and,
// for directories, traverse) it, so a directory owned by someone else that
// is already open does not fail the challenge.
func ensureWorldReadable(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	need := os.FileMode(0o004)
	if info.IsDir() {
		need = 0o005
	}
	if info.Mode().Perm()&need == need {
		return nil
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("make %s readable to the web server: %w", path, err)
	}
	return nil
}

// CleanUp removes the challenge file.
func (p *webrootProvider) CleanUp(domain, token, keyAuth string) error {
	err := os.Remove(filepath.Join(p.root, http01.ChallengePath(token)))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove challenge file: %w", err)
	}
	return nil
}
