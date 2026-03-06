#!/usr/bin/env bash

#MISE description="Build the gram server"
#MISE dir="{{ config_root }}/server"
#MISE depends=["go:tidy"]

#USAGE flag "--readonly" help="Build with -mod=readonly"

set -e

args=()
if [ "${usage_readonly:-false}" = "true" ]; then
    args+=("-mod=readonly")
fi

git_sha=$(git rev-parse HEAD)
CGO_ENABLED=0 go \
    build \
    "${args[@]}" \
    -trimpath \
    -ldflags="-s -w -X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=\"${git_sha}\" -X goa.design/clue/health.Version=\"${git_sha}\"" \
    -o bin/gram \
    ./main.go