#!/usr/bin/env bash

#MISE description="Start the local tailnet gateway (needs a real tailnet and a provisioned ingress)"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

if [[ -z "${GRAM_DATABASE_URL:-}" ]]; then
  echo "GRAM_DATABASE_URL is not set." >&2
  exit 2
fi
if [[ -z "${GRAM_TAILNET_GATEWAY_UPSTREAM_URL:-}" || -z "${GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE:-}" || -z "${GRAM_TAILNET_GATEWAY_TOKEN_PATH:-}" ]]; then
  echo "Set GRAM_TAILNET_GATEWAY_UPSTREAM_URL, GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE and GRAM_TAILNET_GATEWAY_TOKEN_PATH in mise.local.toml to point at a private listener." >&2
  exit 2
fi

ordinal="${GRAM_TAILNET_GATEWAY_ORDINAL:-0}"

exec env \
  GRAM_TAILNET_GATEWAY_ORDINAL="${ordinal}" \
  GRAM_TAILNET_GATEWAY_POD_NAME="${GRAM_TAILNET_GATEWAY_POD_NAME:-gram-tailnet-gateway-${ordinal}}" \
  GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS="${GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS:-127.0.0.1:${GRAM_TAILNET_GATEWAY_CONTROL_PORT:-8093}}" \
  go run ./server/cmd/gram tailnet-gateway
