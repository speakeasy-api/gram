#!/usr/bin/env bash

#MISE description="Start the local private network listener, attesting the tailnet gateway with a local HMAC key instead of Kubernetes"
#MISE dir="{{ config_root }}/server"
#MISE hide=true

set -euo pipefail

state_dir="$(dirname "$GRAM_NETINGRESS_LOCAL_TOKEN_KEY_FILE")"
cert_file="$state_dir/tls.pem"
key_file="$state_dir/tls-key.pem"

# The tailnet gateway pins this certificate's CA, so the listener gets its own
# mkcert certificate for loopback whatever the stack's TLS provider is.
if [[ ! -s "$cert_file" || ! -s "$key_file" ]]; then
  mkdir -p "$state_dir"
  mkcert -cert-file "$cert_file" -key-file "$key_file" localhost 127.0.0.1 ::1
fi

# The listener's readiness probe reviews the gateway's token, which proves the
# key and the token agree before the gateway relies on them.
go run main.go netingress-local-token

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
  CONFIG_ARGS=(--config-file ../config.local.toml)
fi

exec env \
  GRAM_NETINGRESS_ADDRESS="127.0.0.1:${GRAM_NETINGRESS_PORT}" \
  GRAM_NETINGRESS_TLS_CERT_FILE="$cert_file" \
  GRAM_NETINGRESS_TLS_KEY_FILE="$key_file" \
  GRAM_NETINGRESS_READINESS_TOKEN_FILE="$GRAM_TAILNET_GATEWAY_TOKEN_PATH" \
  go run main.go network-ingress-server \
  --control-address "127.0.0.1:${GRAM_NETINGRESS_CONTROL_PORT}" \
  "${CONFIG_ARGS[@]}" "$@"
