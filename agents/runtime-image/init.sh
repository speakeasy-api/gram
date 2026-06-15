#!/usr/bin/env bash

set -euo pipefail

# The assistant workdir is a RAM-backed tmpfs: it mounts instantly (no
# cold-disk read), is per-machine and ephemeral like /tmp, and caps the
# workspace at 256 MiB (against RAM — the VM has 1 GiB). The read-only sandbox
# helpers (browser.ts, node_modules, package.json) live on the rootfs and are
# bind-mounted in read-only at the paths the runner expects, so the agent's
# scratch writes go to RAM while the deps fault in lazily from the rootfs only
# when a tool actually uses them. This replaced a loop-mounted 256 MiB ext4
# image whose cold-boot mount blocked the runner ~35s on throttled disk I/O.
workdir_mount=/var/lib/gram-assistant/work
sandbox_src=/usr/share/gram/sandbox
mkdir -p "$workdir_mount"
mount -t tmpfs -o size=256m,mode=0755 tmpfs "$workdir_mount"
for dep in browser.ts package.json node_modules; do
  src="$sandbox_src/$dep"
  dst="$workdir_mount/$dep"
  [ -e "$src" ] || continue
  # The bind target must exist inside the tmpfs first.
  if [ -d "$src" ]; then mkdir -p "$dst"; else : >"$dst"; fi
  mount --bind "$src" "$dst"
  mount -o remount,bind,ro "$dst"
done

/usr/local/bin/lightpanda-supervise &

runner_port="${GRAM_ASSISTANT_RUNTIME_GUEST_PORT:-8081}"
runner_addr="${GRAM_RUNNER_ADDR:-0.0.0.0:${runner_port}}"

exec /usr/local/bin/gram-assistant-runner serve --addr "$runner_addr"
