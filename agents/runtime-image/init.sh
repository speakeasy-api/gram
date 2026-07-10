#!/usr/bin/env bash

set -euo pipefail

workdir=/var/lib/gram-assistant/work
sandbox_src=/usr/share/gram/sandbox
mkdir -p "$workdir"

# Size-capped tmpfs + read-only bind-mounted deps where mount(2) is permitted;
# symlink the deps where it is not or when GKE supplies a persistent workspace.
# Never cover the generic-ephemeral PVC with the legacy tmpfs mount.
if [[ -z "${GRAM_ASSISTANT_RUNTIME_GKE_WORKSPACE_PATH:-}" ]] && mount -t tmpfs -o size=256m,mode=0755 tmpfs "$workdir" 2>/dev/null; then
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
