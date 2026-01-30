#!/usr/bin/env bash

#MISE description="Build the plog command"
#MISE dir="{{ config_root }}/plog"
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
    -o bin/plog \
    ./cmd/plog
