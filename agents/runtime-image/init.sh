#!/usr/bin/env bash

set -euo pipefail

mount -t proc proc /proc || true
mount -t sysfs sys /sys || true
mount -t devtmpfs dev /dev || true
mkdir -p /dev/pts /run /tmp
mount -t devpts devpts /dev/pts || true

# Provision the assistant workdir from the prebuilt ext4 template. The rootfs
# is per-VM (FC) or per-machine (Fly) so we can mount the template directly —
# any mutations stay isolated to this VM and the loop mount enforces the hard
# size cap baked into the template at image build time.
workdir_image=/usr/share/gram/workdir-template.ext4
workdir_mount=/var/lib/gram-assistant/work
mkdir -p "$workdir_mount"
mount -o loop "$workdir_image" "$workdir_mount"

ip link set lo up || true
cat >/etc/resolv.conf <<'EOF'
nameserver 1.1.1.1
nameserver 8.8.8.8
EOF

cmdline="$(cat /proc/cmdline 2>/dev/null || true)"
cmdline_value() {
  key="$1"
  printf '%s\n' "$cmdline" | tr ' ' '\n' | sed -n "s/^${key}=//p" | head -n 1
}

server_hostname="$(cmdline_value gram_server_hostname)"
server_ip="$(cmdline_value gram_server_ip)"
if [ -n "$server_hostname" ] && [ -n "$server_ip" ]; then
  printf '%s %s\n' "$server_ip" "$server_hostname" >> /etc/hosts
fi

/usr/local/bin/lightpanda-supervise &

runner_port="${GRAM_ASSISTANT_RUNTIME_GUEST_PORT:-$(cmdline_value gram_assistant_runtime_guest_port)}"
runner_port="${runner_port:-8081}"
runner_addr="${GRAM_RUNNER_ADDR:-$(cmdline_value gram_runner_addr)}"
runner_addr="${runner_addr:-0.0.0.0:${runner_port}}"

exec /usr/local/bin/gram-assistant-runner serve --addr "$runner_addr"
