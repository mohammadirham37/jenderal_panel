# Task 1 report: shared NVM runtime

## Delivered API

- `New(exec executor.CommandExecutor) *Service`
- `ValidateVersion(version string) error` accepts only Node majors 20, 22, and 24.
- `Home(user string) (string, error)` accepts bounded lowercase `web_*` Linux account names and returns `/home/<user>`.
- `ExecArgs(user, version, command string, args ...string) ([]string, error)` returns arguments for `RunSudo(ctx, "-u", args...)`. It uses `env -i`, explicit identity/home/NVM/Node/PATH values, and the user's `nvm-exec`. Command arguments remain separate argv values.
- `(*Service).Detect(...) (Status, error)` reports `installed`, `node_version`, `npm_version`, `nvm_version`, and `nvm_state` (JSON snake_case). Expected states are `missing`, `corrupt`, `mismatched`, and `ready`.
- `(*Service).Install(...) error` installs the pinned NVM distribution and requested Node major, streams non-empty stdout lines, verifies Node and npm, and only then promotes the default alias.

`ExecArgs` always supplies `NODE_VERSION`, so consumers do not depend on the mutable default alias. This is also the rollback-safe execution contract: a failed service activation can keep or restore configuration without changing how a selected runtime is resolved.

## Security and failure behavior

- User and version inputs are validated before executor calls.
- Shell programs are fixed constants; validated user, version, and expected home are passed positionally using `bash -c SCRIPT -- user version home`.
- The shell checks that explicit `HOME` and `NVM_DIR` match the validated values and rejects symlinks at the home/NVM/version path boundaries.
- NVM is fetched only over HTTPS with HTTPS-only redirects and bounded connection/transfer time.
- NVM `v0.40.7` is pinned to peeled commit `f0b0c6bb0b281ceeb106c8cf9ab8fde141215092`; its archive is checked against SHA-256 `2a9578d1e31d2e8fc45984ca1ab33e56dce470b9c986ef3ce265fa57a2be3083` before extraction.
- Staging is created under the validated user's home with `umask 077`, verified before an atomic move, and cleaned by a trap.
- Existing corrupt or differently identified NVM trees are preserved and rejected rather than overwritten.
- A prior default Node alias remains untouched unless NVM installation plus direct `nvm-exec` Node/npm verification succeeds.

## Upstream identity evidence

On 2026-09-09, `git ls-remote https://github.com/nvm-sh/nvm.git 'refs/tags/v0.40.7*'` returned annotated tag object `8869c20fa2ebdfce4021e6eabe572142d2dc67b8` and peeled commit `f0b0c6bb0b281ceeb106c8cf9ab8fde141215092`. GitHub's release page identifies v0.40.7 as an immutable release with verified signature. Downloading the commit archive and hashing it locally produced the pinned SHA-256 above.

## TDD and verification

The first focused run failed to compile because the new API did not exist, as expected. Tests cover validation, fixed/isolated execution argv, missing/corrupt/mismatched/ready detection, absent and installed runtimes, major mismatch rejection, successful installation/log forwarding, failed checksum reporting, and a real Bash positional-argument harness.

Fresh successful commands before commit:

```text
go test ./internal/noderuntime
ok github.com/mohammadirham37/jenderal_panel/internal/noderuntime

go vet ./internal/noderuntime
(exit 0)

git diff --check
(exit 0)
```

The repository-wide baseline initially failed at `cmd/jenderal/embed.go` because `web_build` was absent. This is unrelated to Task 1; focused package verification is clean.
