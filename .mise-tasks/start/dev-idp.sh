#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start the dev-idp server (mock-speakeasy + workos + oauth2-1 + oauth2 modes)"

set -e

if [ -z "${GRAM_DEVIDP_DATABASE_URL:-}" ]; then
  echo "GRAM_DEVIDP_DATABASE_URL is not set — uncomment in mise.toml or set in mise.local.toml (run \`mise run zero:devidp\` to opt in)." >&2
  echo "Sleeping so madprocs doesn't restart this proc; ^C and re-enable when ready." >&2
  while true; do sleep 86400; done
fi

GIT_SHA=$(git rev-parse HEAD)

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

exec go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go dev-idp "${CONFIG_ARGS[@]}" "$@"
