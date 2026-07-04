#!/usr/bin/env bash

#MISE description="Build the speakeasy-hooks binary"
#MISE dir="{{ config_root }}/hooks"
#MISE depends=["go:tidy"]

#USAGE flag "--readonly" help="Build with -mod=readonly"

set -e

args=()
if [ "${usage_readonly:-false}" = "true" ]; then
    args+=("-mod=readonly")
fi

CGO_ENABLED=0 go \
    build \
    "${args[@]}" \
    -trimpath \
    -ldflags="-s -w" \
    -o bin/speakeasy-hooks \
    ./cmd/speakeasy-hooks
