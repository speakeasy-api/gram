#!/usr/bin/env bash
# Cursor Cloud install: tools, deps, local secrets, and a warm server compile.
# Do not start dockerd here — the Build sandbox cannot run it. Image pulls
# happen in start.sh after the daemon is up. Keep the snapshot small: Cloud
# VMs are ~21GB and cannot hold Presidio and leftover package caches.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
export DEBIAN_FRONTEND=noninteractive

if ! command -v dockerd >/dev/null 2>&1 || ! command -v fuse-overlayfs >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install -y docker.io docker-compose-v2 fuse-overlayfs iptables
fi
sudo mkdir -p /etc/docker
printf '%s\n' '{' '  "storage-driver": "fuse-overlayfs",' '  "iptables": true' '}' | sudo tee /etc/docker/daemon.json >/dev/null
if [ -x /usr/sbin/iptables-legacy ]; then
  sudo update-alternatives --set iptables /usr/sbin/iptables-legacy
  sudo update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy
fi
if ! id -nG "$USER" | grep -qw docker; then
  sudo usermod -aG docker "$USER" || true
fi
if ! command -v mise >/dev/null 2>&1; then
  curl -fsSL https://mise.run | sh
fi
export PATH="$HOME/.local/bin:$PATH"
cd /workspace
mise trust
mise install
mise run install

if [ ! -f mise.local.toml ]; then
  printf '%s\n' '# Local mise configuration' '' '[tools]' '' '[env]' >mise.local.toml
fi
if ! grep -q 'USE_RECOMMENDED_SKILLS' mise.local.toml; then
  mise set --file mise.local.toml USE_RECOMMENDED_SKILLS=false
fi

# Keys and TLS belong in the snapshot so start does not regenerate them.
# Skip assistant-runtime image builds — that OCI image is huge and unused
# unless a task actually starts local assistants.
mise run zero:devidp
mise run zero:encryption
mise run zero:tunnel-identity
mise run zero:tls
mise run zero:assistants --skip-image

mise run build:server-cache

# Drop rebuildable caches so the snapshot leaves room for core compose images
# at start. Keep ~/go/pkg/mod and the aube virtual-store.
mise exec -- go clean -cache || true
if mise exec -- uv --version >/dev/null 2>&1; then
  mise exec -- uv cache clean || true
fi
rm -rf "${HOME}/.cache/aube/packuments-full-v1" "${HOME}/.cache/uv"
