#!/usr/bin/env bash

#MISE description="Tail Lima VM kernel+syslog to see tap/firecracker events during local assistant runtime dev"
#MISE hide=true

set -e

INSTANCE="${GRAM_ASSISTANT_RUNTIME_LIMA_INSTANCE:-${GRAM_ASSISTANT_LIMA_INSTANCE:-gram-firecracker}}"

if ! command -v limactl >/dev/null 2>&1; then
  echo "limactl not found; skipping lima log tail" >&2
  exec sleep infinity
fi

if ! limactl list --quiet 2>/dev/null | grep -qx "$INSTANCE"; then
  echo "lima instance '$INSTANCE' not found; skipping log tail" >&2
  exec sleep infinity
fi

if [[ "$(limactl list --format '{{.Status}}' "$INSTANCE" 2>/dev/null)" != "Running" ]]; then
  echo "lima instance '$INSTANCE' is not running; start it with: limactl start $INSTANCE" >&2
  exec sleep infinity
fi

echo "Streaming lima journal for $INSTANCE (tap/firecracker activity shows up here)"
exec limactl shell "$INSTANCE" -- sudo journalctl -f -n 100 --no-hostname
