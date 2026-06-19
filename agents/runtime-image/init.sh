#!/usr/bin/env bash

set -euo pipefail

workdir=/var/lib/gram-assistant/work
sandbox_src=/usr/share/gram/sandbox
mkdir -p "$workdir"

# Size-capped tmpfs + read-only bind-mounted deps where mount(2) is permitted;
# symlink the deps where it is not (the platform supplies the writable workdir).
if mount -t tmpfs -o size=256m,mode=0755 tmpfs "$workdir" 2>/dev/null; then
  for dep in browser.ts package.json node_modules; do
    src="$sandbox_src/$dep"
    dst="$workdir/$dep"
    [ -e "$src" ] || continue
    if [ -d "$src" ]; then mkdir -p "$dst"; else : >"$dst"; fi
    mount --bind "$src" "$dst"
    mount -o remount,bind,ro "$dst"
  done
else
  for dep in browser.ts package.json node_modules; do
    [ -e "$sandbox_src/$dep" ] || continue
    ln -sfn "$sandbox_src/$dep" "$workdir/$dep"
  done
fi

/usr/local/bin/lightpanda-supervise &

runner_port="${GRAM_ASSISTANT_RUNTIME_GUEST_PORT:-8081}"
runner_addr="${GRAM_RUNNER_ADDR:-0.0.0.0:${runner_port}}"

exec /usr/local/bin/gram-assistant-runner serve --addr "$runner_addr"
