#!/usr/bin/env bash

set -euo pipefail

mount -t proc proc /proc || true
mount -t sysfs sys /sys || true
mount -t devtmpfs dev /dev || true
mkdir -p /dev/pts /run /tmp
mount -t devpts devpts /dev/pts || true

ip link set lo up || true
cat >/etc/resolv.conf <<'EOF'
nameserver 1.1.1.1
nameserver 8.8.8.8
EOF

cmdline="$(cat /proc/cmdline 2>/dev/null || true)"
server_hostname="$(printf '%s\n' "$cmdline" | tr ' ' '\n' | sed -n 's/^gram_server_hostname=//p' | head -n 1)"
server_ip="$(printf '%s\n' "$cmdline" | tr ' ' '\n' | sed -n 's/^gram_server_ip=//p' | head -n 1)"
if [ -n "$server_hostname" ] && [ -n "$server_ip" ]; then
  printf '%s %s\n' "$server_ip" "$server_hostname" >> /etc/hosts
fi

/usr/local/bin/lightpanda-supervise &

exec /usr/local/bin/gram-assistant-runner serve --addr 0.0.0.0:8081
