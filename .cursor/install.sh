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
mise run build:server-cache