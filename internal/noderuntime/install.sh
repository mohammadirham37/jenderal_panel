#!/usr/bin/env bash
set -eu
umask 077
user=$1
version=$2
expected_home=$3
expected_commit=f0b0c6bb0b281ceeb106c8cf9ab8fde141215092
expected_sha256=2a9578d1e31d2e8fc45984ca1ab33e56dce470b9c986ef3ce265fa57a2be3083
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

