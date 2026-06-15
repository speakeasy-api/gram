#!/usr/bin/env bash

set -euo pipefail

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
