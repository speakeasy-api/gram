#!/usr/bin/env bash
#MISE description="Start the dev-idp operator dashboard"

set -e

if [ -z "${GRAM_DEVIDP_EXTERNAL_URL:-}" ]; then
  echo "GRAM_DEVIDP_EXTERNAL_URL is not set — uncomment in mise.toml or set in mise.local.toml (run \`mise run zero:devidp\` to opt in)." >&2
  exit 1
fi

exec pnpm --filter ./dev-idp-dashboard dev
