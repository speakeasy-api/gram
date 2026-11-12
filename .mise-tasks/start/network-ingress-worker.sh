#!/usr/bin/env bash

#MISE description="Start the local private network reconciler for tailnet gateway connections"
#MISE dir="{{ config_root }}/server"
#MISE hide=true

set -euo pipefail

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
  CONFIG_ARGS=(--config-file ../config.local.toml)
fi

# Local private network artifacts are named local-<id>-... so they cannot
# collide with production's on a shared tailnet. A worktree set up before the
# id existed gets one here; a fresh mise process reads it back.
if [[ -z "${GRAM_NETWORK_INGRESS_DEV_ID:-}" ]]; then
  mise run zero:network-ingress
  GRAM_NETWORK_INGRESS_DEV_ID="$(mise exec -- printenv GRAM_NETWORK_INGRESS_DEV_ID)"
  export GRAM_NETWORK_INGRESS_DEV_ID
fi

# Local development has no Tailscale Kubernetes operator, so only the gateway
# provider is registered. One slot matches the single local gateway process;
# a second slot would never come online and leave connections degraded.
exec env \
  GRAM_NETWORK_INGRESS_WORKER_CONTROL_ADDRESS="127.0.0.1:${GRAM_NETWORK_INGRESS_WORKER_CONTROL_PORT}" \
  GRAM_NETWORK_INGRESS_PROVIDER_MUTATIONS_ENABLED=true \
  GRAM_NETWORK_INGRESS_OPERATOR_PROVIDER_ENABLED=false \
  GRAM_NETWORK_INGRESS_GATEWAY_ENABLED=true \
  GRAM_NETWORK_INGRESS_GATEWAY_SLOTS="${GRAM_NETWORK_INGRESS_GATEWAY_SLOTS:-1}" \
  go run main.go network-ingress-worker "${CONFIG_ARGS[@]}" "$@"
