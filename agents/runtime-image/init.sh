#!/usr/bin/env bash

set -euo pipefail

workdir=/var/lib/gram-assistant/work
sandbox_src=/usr/share/gram/sandbox
mkdir -p "$workdir"

# The platform may mount an extra CA bundle (e.g. the mkcert root CA during
# local development) so the runner and its sandbox children trust the Gram
# server's TLS certificate. Build a combined bundle in /tmp rather than
# mutating the system trust store, which must stay pristine (and may sit on a
# read-only root filesystem).
extra_ca=/usr/local/share/gram/extra-ca.pem
if [ -f "$extra_ca" ]; then
  ca_bundle=/tmp/gram-ca-bundle.pem
  cat /etc/ssl/certs/ca-certificates.crt "$extra_ca" > "$ca_bundle"
  export SSL_CERT_FILE="$ca_bundle"
  export NODE_EXTRA_CA_CERTS="$extra_ca"
fi

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
