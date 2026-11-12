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

# Stands in for the projected service-account token. It is minted on every
# start and lasts 30 days, which outlasts a stack left running.
go run main.go netingress-local-token

ordinal="${GRAM_TAILNET_GATEWAY_ORDINAL:-0}"

# Local private network artifacts are named local-<id>-... so they cannot
# collide with production's on a shared tailnet. A worktree set up before the
# id existed gets one here; a fresh mise process reads it back.
if [[ -z "${GRAM_NETWORK_INGRESS_DEV_ID:-}" ]]; then
  mise run zero:network-ingress
  GRAM_NETWORK_INGRESS_DEV_ID="$(mise exec -- printenv GRAM_NETWORK_INGRESS_DEV_ID)"
  export GRAM_NETWORK_INGRESS_DEV_ID
fi

# Outside Kubernetes, tsnet keeps each node's TLS certificate in its state
# directory rather than the Postgres state store. The command's default is a
# new temporary directory per start, so every restart would request a fresh
# certificate and soon exhaust Let's Encrypt's weekly limit for the tailnet.
state_dir="${GRAM_TAILNET_GATEWAY_STATE_DIR:-$(dirname "$GRAM_TAILNET_GATEWAY_TOKEN_PATH")/tsnet}"
mkdir -p "$state_dir"

exec env \
  GRAM_TAILNET_GATEWAY_STATE_DIR="${state_dir}" \
  GRAM_TAILNET_GATEWAY_UPSTREAM_URL="${upstream_url}" \
  GRAM_TAILNET_GATEWAY_UPSTREAM_CA_FILE="${upstream_ca_file}" \
  GRAM_TAILNET_GATEWAY_ORDINAL="${ordinal}" \
  GRAM_TAILNET_GATEWAY_POD_NAME="${GRAM_TAILNET_GATEWAY_POD_NAME:-gram-tailnet-gateway-${ordinal}}" \
  GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS="${GRAM_TAILNET_GATEWAY_CONTROL_ADDRESS:-127.0.0.1:${GRAM_TAILNET_GATEWAY_CONTROL_PORT:-8093}}" \
  go run main.go tailnet-gateway "$@"
