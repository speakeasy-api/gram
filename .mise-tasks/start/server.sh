#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the API server"
#MISE hide=true

# Local private network artifacts are named dev-<id>-... so they cannot
# collide with production's on a shared tailnet. A worktree set up before the
# id existed gets one here; a fresh mise process reads it back.
if [[ -z "${GRAM_NETWORK_INGRESS_DEV_ID:-}" ]]; then
  mise run zero:network-ingress
  GRAM_NETWORK_INGRESS_DEV_ID="$(mise exec -- printenv GRAM_NETWORK_INGRESS_DEV_ID)"
  export GRAM_NETWORK_INGRESS_DEV_ID
fi

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

go run main.go start "${CONFIG_ARGS[@]}" "$@"
