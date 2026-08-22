#!/usr/bin/env bash

set -Eeuo pipefail

if [[ ! -r /etc/os-release ]]; then
  echo "This installer requires Ubuntu or Debian." >&2
  exit 1
fi

# shellcheck source=/dev/null
source /etc/os-release
case "${ID:-}:${ID_LIKE:-}" in
  ubuntu:*|debian:*|*:ubuntu*|*:debian*) ;;
  *)
    echo "Unsupported distribution: ${PRETTY_NAME:-unknown}. Use Ubuntu or Debian." >&2
    exit 1
    ;;
esac

target_user="${SUDO_USER:-$(id -un)}"
target_home="$(getent passwd "$target_user" | cut -d: -f6)"
target_shell="$(getent passwd "$target_user" | cut -d: -f7)"
if [[ -z "$target_home" || ! -d "$target_home" ]]; then
  echo "Could not determine the home directory for $target_user." >&2
  exit 1
fi

if [[ "$(id -u)" -eq 0 ]]; then
  sudo_cmd=()
else
  command -v sudo >/dev/null 2>&1 || {
    echo "sudo is required when this script is not run as root." >&2
    exit 1
  }
  sudo_cmd=(sudo)
fi

run_as_target() {
  if [[ "$(id -un)" == "$target_user" ]]; then
    "$@"
  else
    sudo -H -u "$target_user" "$@"
  fi
}

echo "Installing Ubuntu build and language prerequisites..."
export DEBIAN_FRONTEND=noninteractive
"${sudo_cmd[@]}" apt-get update
"${sudo_cmd[@]}" apt-get install -y --no-install-recommends \
  autoconf \
  automake \
  build-essential \
  ca-certificates \
  clang \
  cmake \
  curl \
  file \
  git \
  jq \
  libssl-dev \
  libsodium-dev \
  meson \
  ninja-build \
  pipx \
  pkg-config \
  protobuf-compiler \
  python3 \
  python3-dev \
  python3-pip \
  python3-venv \
  rsync \
  shellcheck \
  sudo \
  tar \
  unzip \
  xz-utils \
  zip

echo "Configuring pipx for $target_user..."
run_as_target python3 -m pipx ensurepath

echo "Installing or updating Rust and Cargo for $target_user..."
if [[ ! -x "$target_home/.cargo/bin/rustup" ]]; then
  rustup_installer="$(mktemp)"
  curl --proto '=https' --tlsv1.2 -fsS https://sh.rustup.rs -o "$rustup_installer"
  run_as_target sh "$rustup_installer" -y --profile minimal --default-toolchain stable
  rm -f "$rustup_installer"
else
  run_as_target "$target_home/.cargo/bin/rustup" update stable
  run_as_target "$target_home/.cargo/bin/rustup" default stable
fi

case "$(uname -m)" in
  x86_64) go_arch="amd64" ;;
  aarch64|arm64) go_arch="arm64" ;;
  *)
    echo "Unsupported Go architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

echo "Installing the current stable Go toolchain system-wide..."
go_metadata="$(curl -fsSL 'https://go.dev/dl/?mode=json')"
go_version="$(jq -r 'map(select(.stable == true))[0].version' <<<"$go_metadata")"
go_filename="${go_version}.linux-${go_arch}.tar.gz"
go_checksum="$(jq -r --arg filename "$go_filename" \
  'map(select(.stable == true))[0].files[] | select(.filename == $filename) | .sha256' \
  <<<"$go_metadata")"

if [[ -z "$go_version" || -z "$go_checksum" || "$go_checksum" == "null" ]]; then
  echo "Could not resolve the current Go download." >&2
  exit 1
fi

installed_go_version=""
if [[ -x /usr/local/go/bin/go ]]; then
  installed_go_version="$(/usr/local/go/bin/go version | awk '{print $3}')"
fi

if [[ "$installed_go_version" != "$go_version" ]]; then
  go_temp_dir="$(mktemp -d)"
  curl -fsSL "https://go.dev/dl/$go_filename" -o "$go_temp_dir/$go_filename"
  echo "$go_checksum  $go_temp_dir/$go_filename" | sha256sum --check --status

  if [[ -d /usr/local/go ]]; then
    go_backup="/usr/local/go.backup.$(date +%s)"
    echo "Moving the previous Go installation to $go_backup"
    "${sudo_cmd[@]}" mv /usr/local/go "$go_backup"
  fi

  "${sudo_cmd[@]}" tar -C /usr/local -xzf "$go_temp_dir/$go_filename"
  rm -f "$go_temp_dir/$go_filename"
  rmdir "$go_temp_dir"
fi

printf '%s\n' 'export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"' | \
  "${sudo_cmd[@]}" tee /etc/profile.d/benchmark-go-path.sh >/dev/null
"${sudo_cmd[@]}" chmod 0644 /etc/profile.d/benchmark-go-path.sh

echo "Installing NVM and the current Node.js LTS for $target_user..."
nvm_version="$(curl -fsSL https://api.github.com/repos/nvm-sh/nvm/releases/latest | jq -r '.tag_name')"
if [[ -z "$nvm_version" || "$nvm_version" == "null" ]]; then
  echo "Could not resolve the current NVM release." >&2
  exit 1
fi

nvm_profile="$target_home/.bashrc"
if [[ "$target_shell" == */zsh ]]; then
  nvm_profile="$target_home/.zshrc"
fi

nvm_installer="$(mktemp)"
curl -fsSL \
  "https://raw.githubusercontent.com/nvm-sh/nvm/${nvm_version}/install.sh" \
  -o "$nvm_installer"
chmod a+r "$nvm_installer"
run_as_target env \
  NVM_DIR="$target_home/.nvm" \
  PROFILE="$nvm_profile" \
  bash "$nvm_installer"
rm -f "$nvm_installer"

run_as_target env NVM_DIR="$target_home/.nvm" bash -c '
  source "$NVM_DIR/nvm.sh"
  nvm install --lts
  nvm alias default "lts/*"
  nvm use default
'

echo
echo "Installed versions:"
python3 --version
run_as_target python3 -m pipx --version
run_as_target "$target_home/.cargo/bin/rustc" --version
run_as_target "$target_home/.cargo/bin/cargo" --version
/usr/local/go/bin/go version
run_as_target env NVM_DIR="$target_home/.nvm" bash -c '
  source "$NVM_DIR/nvm.sh"
  node --version
  npm --version
'

echo
echo "Done. Start a new shell before installing the benchmark tools."
