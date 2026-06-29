#!/usr/bin/env bash

#MISE description="Start the local tunnel gateway"
#MISE dir="{{ config_root }}"

set -euo pipefail

if [[ -z "${GRAM_DATABASE_URL:-}" ]]; then
  echo "GRAM_DATABASE_URL is not set." >&2
  exit 2
fi

exec env \
  TUNNEL_GATEWAY_ADDR="${TUNNEL_GATEWAY_ADDR:-:8090}" \
  TUNNEL_GATEWAY_ADVERTISE_ADDR="${TUNNEL_GATEWAY_ADVERTISE_ADDR:-127.0.0.1:8090}" \
  TUNNEL_REDIS_ADDR="${TUNNEL_REDIS_ADDR:-${GRAM_REDIS_CACHE_ADDR:-127.0.0.1:5445}}" \
  TUNNEL_REDIS_PASSWORD="${TUNNEL_REDIS_PASSWORD:-${GRAM_REDIS_CACHE_PASSWORD:-xi9XILbY}}" \
  TUNNEL_DATABASE_URL="${TUNNEL_DATABASE_URL:-${GRAM_DATABASE_URL}}" \
  go run ./tunnel/cmd/tunnel-gateway
