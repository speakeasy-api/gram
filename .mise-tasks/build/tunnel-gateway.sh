#!/usr/bin/env bash

#MISE description="Build the tunnel gateway binary"
#MISE dir="{{ config_root }}"
#MISE depends=["go:tidy"]

#USAGE flag "--readonly" help="Build with -mod=readonly"

set -e

args=()
if [ "${usage_readonly:-false}" = "true" ]; then
    args+=("-mod=readonly")
fi

mkdir -p server/bin

CGO_ENABLED=0 go \
    build \
    "${args[@]}" \
    -trimpath \
    -o server/bin/tunnel-gateway \
    ./tunnel/cmd/tunnel-gateway
