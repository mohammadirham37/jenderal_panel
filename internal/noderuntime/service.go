// Package noderuntime manages isolated NVM and Node runtimes for website users.
package noderuntime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

const (
	nvmVersion       = "v0.40.7"
	nvmCommit        = "f0b0c6bb0b281ceeb106c8cf9ab8fde141215092"
	nvmArchiveSHA256 = "2a9578d1e31d2e8fc45984ca1ab33e56dce470b9c986ef3ce265fa57a2be3083"
)

const (
	detectTimeout         = 15 * time.Second
	installTimeout        = 15 * time.Minute
	detectCommandTimeout  = "10s"
	installCommandTimeout = "14m"
)

const (
	NVMStateMissing    = "missing"
	NVMStateCorrupt    = "corrupt"
	NVMStateMismatched = "mismatched"
	NVMStateReady      = "ready"
)

var webUserPattern = regexp.MustCompile(`^web_[a-z0-9](?:[a-z0-9_]{0,30}[a-z0-9])?$`)

// Status describes the requested runtime and its NVM installation.
type Status struct {
	Installed   bool   `json:"installed"`
	NodeVersion string `json:"node_version"`
	NPMVersion  string `json:"npm_version"`
	NVMVersion  string `json:"nvm_version"`
	NVMState    string `json:"nvm_state"`
}

type Service struct {
	exec executor.CommandExecutor
}

func New(exec executor.CommandExecutor) *Service { return &Service{exec: exec} }

func ValidateVersion(version string) error {
	switch version {
	case "20", "22", "24":
		return nil
	default:
		return fmt.Errorf("unsupported Node version %q", version)
	}
}

func Home(user string) (string, error) {
	if !webUserPattern.MatchString(user) {
		return "", fmt.Errorf("invalid website user %q", user)
	}
	return "/home/" + user, nil
}

// ExecArgs returns argv to pass after the "-u" name in RunSudo. It uses an
// empty, explicit environment and nvm-exec, so it never depends on a login
// shell or the user's current default alias.
func ExecArgs(user, version, command string, args ...string) ([]string, error) {
	home, err := Home(user)
	if err != nil {
		return nil, err
	}
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	if command == "" || strings.IndexByte(command, 0) >= 0 {
		return nil, errors.New("command must not be empty or contain NUL")
	}
	result := []string{
		user, "--", "/usr/bin/env", "-i",
		"HOME=" + home, "USER=" + user, "LOGNAME=" + user,
		"NVM_DIR=" + home + "/.nvm", "NODE_VERSION=" + version,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		home + "/.nvm/nvm-exec", command,
	}
	return append(result, args...), nil
}

func runtimeShellArgs(user, version, commandTimeout, script string) ([]string, error) {
	home, err := Home(user)
	if err != nil {
		return nil, err
	}
	if err := ValidateVersion(version); err != nil {
		return nil, err
	}
	return []string{
		user, "--", "/usr/bin/env", "-i",
		"HOME=" + home, "USER=" + user, "LOGNAME=" + user,
		"NVM_DIR=" + home + "/.nvm", "NODE_VERSION=" + version,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"/usr/bin/timeout", "--signal=TERM", "--kill-after=5s", commandTimeout,
		"/bin/bash", "-c", script, "--", user, version, home,
	}, nil
}

func (s *Service) Detect(ctx context.Context, user, version string) (Status, error) {
	args, err := runtimeShellArgs(user, version, detectCommandTimeout, detectScript)
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, detectTimeout)
	defer cancel()
	result, err := s.exec.RunSudo(ctx, "-u", args...)
	if err != nil {
		return Status{}, fmt.Errorf("detect Node runtime: %w", err)
	}
	if result == nil {
		return Status{}, errors.New("detect Node runtime: executor returned no result")
	}
	if result.ExitCode != 0 {
		return Status{}, commandError("detect Node runtime", result)
	}
	return parseStatus(result.Stdout, version)
}

func parseStatus(output, requestedMajor string) (Status, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			return Status{}, fmt.Errorf("malformed runtime status line %q", scanner.Text())
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return Status{}, fmt.Errorf("read runtime status: %w", err)
	}
	state := values["nvm_state"]
	switch state {
	case NVMStateMissing, NVMStateCorrupt, NVMStateMismatched, NVMStateReady:
	default:
		return Status{}, fmt.Errorf("invalid NVM state %q", state)
	}
	status := Status{NVMState: state, NVMVersion: values["nvm_version"]}
	if values["installed"] == "true" {
		status.Installed = true
		status.NodeVersion = values["node_version"]
		status.NPMVersion = values["npm_version"]
		if !strings.HasPrefix(status.NodeVersion, "v"+requestedMajor+".") {
			return Status{}, fmt.Errorf("detected Node %q does not match requested major %s", status.NodeVersion, requestedMajor)
		}
		if status.NPMVersion == "" {
			return Status{}, errors.New("detected installed runtime without npm version")
		}
	} else if values["installed"] != "" && values["installed"] != "false" {
		return Status{}, fmt.Errorf("invalid installed status %q", values["installed"])
	}
	return status, nil
}

func (s *Service) Install(ctx context.Context, user, version string, log func(string)) error {
	args, err := runtimeShellArgs(user, version, installCommandTimeout, installScript)
	if err != nil {
		return err
	}
	if log != nil {
		log(fmt.Sprintf("Preparing NVM and Node %s for %s", version, user))
	}
	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	result, err := s.exec.RunSudo(ctx, "-u", args...)
	if err != nil {
		return fmt.Errorf("install Node runtime: %w", err)
	}
	if result == nil {
		return errors.New("install Node runtime: executor returned no result")
	}
	if log != nil {
		for _, output := range []string{result.Stdout, result.Stderr} {
			scanner := bufio.NewScanner(strings.NewReader(output))
			for scanner.Scan() {
				if line := strings.TrimSpace(scanner.Text()); line != "" {
					log(line)
				}
			}
		}
	}
	if result.ExitCode != 0 {
		return commandError("install Node runtime", result)
	}
	return nil
}

func commandError(action string, result *executor.Result) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		detail = "command failed"
	}
	return fmt.Errorf("%s (exit %d): %s", action, result.ExitCode, detail)
}

const detectScript = `set -eu
user=$1
version=$2
expected_home=$3
expected_commit=` + nvmCommit + `

if [ "$HOME" != "$expected_home" ] || [ "$NVM_DIR" != "$expected_home/.nvm" ]; then echo 'invalid runtime home' >&2; exit 64; fi
if [ -L "$HOME" ] || [ -L "$NVM_DIR" ] || [ -L "$NVM_DIR/versions" ] || [ -L "$NVM_DIR/versions/node" ] || [ -L "$NVM_DIR/versions/node/v$version" ]; then
  printf 'nvm_state=corrupt\n'
  exit 0
fi
if [ ! -e "$NVM_DIR" ]; then
  printf 'nvm_state=missing\n'
  exit 0
fi
if [ ! -d "$NVM_DIR" ] || [ ! -f "$NVM_DIR/nvm.sh" ] || [ ! -x "$NVM_DIR/nvm-exec" ]; then
  printf 'nvm_state=corrupt\n'
  exit 0
fi
actual_commit=''
if [ -f "$NVM_DIR/.jenderal-nvm-commit" ]; then IFS= read -r actual_commit < "$NVM_DIR/.jenderal-nvm-commit" || true; fi
if [ "$actual_commit" != "$expected_commit" ]; then
  printf 'nvm_state=mismatched\nnvm_version=%s\n' "$actual_commit"
  exit 0
fi
printf 'nvm_state=ready\nnvm_version=` + nvmVersion + `\n'
node_version=$(NODE_VERSION="$version" "$NVM_DIR/nvm-exec" node --version 2>/dev/null) || { printf 'installed=false\n'; exit 0; }
npm_version=$(NODE_VERSION="$version" "$NVM_DIR/nvm-exec" npm --version 2>/dev/null) || { printf 'installed=false\n'; exit 0; }
case "$node_version" in "v$version."*) ;; *) printf 'installed=false\n'; exit 0;; esac
printf 'installed=true\nnode_version=%s\nnpm_version=%s\n' "$node_version" "$npm_version"
`

const installScript = `set -eu
umask 077
user=$1
version=$2
expected_home=$3
expected_commit=` + nvmCommit + `
expected_sha256=` + nvmArchiveSHA256 + `
archive_url=https://github.com/nvm-sh/nvm/archive/$expected_commit.tar.gz

if [ "$HOME" != "$expected_home" ] || [ "$NVM_DIR" != "$expected_home/.nvm" ]; then echo 'invalid runtime home' >&2; exit 64; fi
for path in "$HOME" "$NVM_DIR" "$NVM_DIR/versions" "$NVM_DIR/versions/node" "$NVM_DIR/versions/node/v$version"; do
  if [ -L "$path" ]; then echo "Refusing symlinked runtime path: $path" >&2; exit 65; fi
done

stage=''
install_ok=false
rollback_armed=false
previous_default=''
previous_default_exists=false
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$rollback_armed" = true ] && [ "$install_ok" != true ]; then
    . "$NVM_DIR/nvm.sh"
    if [ "$previous_default_exists" = true ]; then nvm alias default "$previous_default" >/dev/null 2>&1 || true; else nvm unalias default >/dev/null 2>&1 || true; fi
  fi
  if [ -n "$stage" ] && [ -d "$stage" ]; then rm -rf -- "$stage"; fi
  exit "$status"
}
trap cleanup EXIT HUP INT TERM

install_nvm() {
  stage=$(mktemp -d "$HOME/.nvm-stage.XXXXXX")
  archive=$stage/nvm.tar.gz
  curl --fail --location --silent --show-error --connect-timeout 10 --max-time 120 --proto '=https' --proto-redir '=https' --tlsv1.2 "$archive_url" -o "$archive"
  actual_sha256=$(sha256sum "$archive" | awk '{print $1}')
  if [ "$actual_sha256" != "$expected_sha256" ]; then echo 'NVM archive checksum verification failed' >&2; exit 66; fi
  mkdir "$stage/unpacked"
  tar -xzf "$archive" --strip-components=1 -C "$stage/unpacked"
  if [ ! -f "$stage/unpacked/nvm.sh" ] || [ ! -x "$stage/unpacked/nvm-exec" ]; then echo 'NVM archive content verification failed' >&2; exit 67; fi
  printf '%s\n' "$expected_commit" > "$stage/unpacked/.jenderal-nvm-commit"
  mv "$stage/unpacked" "$NVM_DIR"
}

if [ ! -e "$NVM_DIR" ]; then
  install_nvm
elif [ ! -d "$NVM_DIR" ] || [ ! -f "$NVM_DIR/nvm.sh" ] || [ ! -x "$NVM_DIR/nvm-exec" ]; then
  actual_commit=''
  if [ -f "$NVM_DIR/.jenderal-nvm-commit" ]; then IFS= read -r actual_commit < "$NVM_DIR/.jenderal-nvm-commit" || true; fi
  if [ "$actual_commit" != "$expected_commit" ] || [ -e "$NVM_DIR/versions/node" ]; then
    echo 'Existing unknown or runtime-bearing NVM tree is corrupt; refusing replacement' >&2; exit 68
  fi
  quarantine="$HOME/.nvm-incomplete.$$.bak"
  if [ -e "$quarantine" ]; then echo 'NVM repair quarantine already exists' >&2; exit 68; fi
  mv "$NVM_DIR" "$quarantine"
  echo "Preserved incomplete panel-owned NVM tree at $quarantine"
  install_nvm
else
  actual_commit=''
  if [ -f "$NVM_DIR/.jenderal-nvm-commit" ]; then IFS= read -r actual_commit < "$NVM_DIR/.jenderal-nvm-commit" || true; fi
  if [ "$actual_commit" != "$expected_commit" ]; then echo 'Existing NVM installation has unexpected identity; refusing replacement' >&2; exit 69; fi
fi

echo "Installing Node $version"
. "$NVM_DIR/nvm.sh"
if [ -f "$NVM_DIR/alias/default" ]; then IFS= read -r previous_default < "$NVM_DIR/alias/default"; previous_default_exists=true; fi
rollback_armed=true
nvm install "$version"
node_version=$(NODE_VERSION="$version" "$NVM_DIR/nvm-exec" node --version)
npm_version=$(NODE_VERSION="$version" "$NVM_DIR/nvm-exec" npm --version)
case "$node_version" in "v$version."*) ;; *) echo "Installed Node verification failed: $node_version" >&2; exit 70;; esac
if [ -z "$npm_version" ]; then echo 'Installed npm verification failed' >&2; exit 71; fi
# Promotion is deliberately last: every failure above preserves the prior alias.
nvm alias default "$version"
install_ok=true
echo "Installed Node $node_version with npm $npm_version"
`
