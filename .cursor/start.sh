set -euo pipefail
unset DOCKER_HOST || true
export PATH="$HOME/.local/bin:$PATH"
cd /workspace

if ! docker info >/dev/null 2>&1; then
  if [ -S /var/run/docker.sock ]; then
    sudo chmod 666 /var/run/docker.sock || true
  fi
fi
if ! docker info >/dev/null 2>&1; then
  sudo mkdir -p /etc/docker
  printf '%s\n' '{' '  "storage-driver": "fuse-overlayfs",' '  "iptables": true' '}' | sudo tee /etc/docker/daemon.json >/dev/null
  if [ -x /usr/sbin/iptables-legacy ]; then
    sudo update-alternatives --set iptables /usr/sbin/iptables-legacy
    sudo update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy
  fi
  sudo setsid dockerd >/tmp/dockerd.log 2>&1 < /dev/null &
  ready=0
  for _ in $(seq 1 60); do
    if docker info >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  sudo chmod 666 /var/run/docker.sock || true
  if [ "$ready" != 1 ]; then
    echo "dockerd failed to start" >&2
    tail -n 80 /tmp/dockerd.log >&2 || true
    exit 1
  fi
fi

eval "$(mise activate bash)"

if [ -z "${ATLAS_TOKEN:-}" ]; then
  echo "ATLAS_TOKEN is required to log in to Atlas Pro" >&2
  exit 1
fi
atlas login --token "${ATLAS_TOKEN}"
atlas whoami >/dev/null