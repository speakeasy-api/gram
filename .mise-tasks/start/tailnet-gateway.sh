#!/usr/bin/env bash

#MISE description="Start the local tailnet gateway (needs a real tailnet and a provisioned ingress)"
#MISE dir="{{ config_root }}/server"
#MISE hide=true

set -euo pipefail

if [[ -z "${GRAM_DATABASE_URL:-}" ]]; then
  echo "GRAM_DATABASE_URL is not set." >&2
  exit 2
fi

# By default the gateway forwards to the local private listener
# (network-ingress-server), which serves a certificate from the local mkcert CA.
upstream_url="${GRAM_TAILNET_GATEWAY_UPSTREAM_URL:-https://localhost:${GRAM_NETINGRESS_PORT}}"
upstream_ca_file="${GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE:-$(mkcert -CAROOT)/rootCA.pem}"

# Stands in for the projected service-account token. Minting on every start
# keeps it well inside its lifetime for as long as the stack stays up.
go run main.go netingress-local-token

ordinal="${GRAM_TAILNET_GATEWAY_ORDINAL:-0}"

exec env \
  GRAM_TAILNET_GATEWAY_UPSTREAM_URL="${upstream_url}" \
  GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE="${upstream_ca_file}" \
  GRAM_TAILNET_GATEWAY_ORDINAL="${ordinal}" \
  GRAM_TAILNET_GATEWAY_POD_NAME="${GRAM_TAILNET_GATEWAY_POD_NAME:-gram-tailnet-gateway-${ordinal}}" \
  GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS="${GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS:-127.0.0.1:${GRAM_TAILNET_GATEWAY_CONTROL_PORT:-8093}}" \
  go run main.go tailnet-gateway "$@"
