#!/usr/bin/env bash

#MISE description="Start the dev-idp operator dashboard"
#MISE hide=true

set -e

if [ -z "${GRAM_DEVIDP_EXTERNAL_URL:-}" ]; then
  echo "GRAM_DEVIDP_EXTERNAL_URL is not set — uncomment in mise.toml or set in mise.local.toml (run \`mise run zero:devidp\` to opt in)." >&2
  exit 1
fi

# This server keeps about 85 MB of live heap.
# shellcheck source=.mise-tasks/start/_vite-memory.sh
source "${BASH_SOURCE[0]%/*}/_vite-memory.sh"
vite_memory_limits 640

exec aube run --filter ./dev-idp-dashboard dev
