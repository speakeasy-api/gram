set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
export DEBIAN_FRONTEND=noninteractive
# docker-buildx provides BuildKit, which the assistant runtime image needs
# (its Dockerfile uses RUN --mount).
if ! command -v dockerd >/dev/null 2>&1 || ! command -v fuse-overlayfs >/dev/null 2>&1 || ! docker buildx version >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install -y docker.io docker-buildx docker-compose-v2 fuse-overlayfs iptables
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
# Resolve the repo root from this script so it works wherever the checkout
# lives (single-repo environments use /workspace; multi-repo ones may not).
cd "$(dirname "${BASH_SOURCE[0]}")/.."
mise trust
mise install