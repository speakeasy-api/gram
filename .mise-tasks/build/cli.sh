#!/usr/bin/env bash

#MISE description="Build the gram cli"
#MISE dir="{{ config_root }}/cli"
#MISE depends=["go:tidy"]

#USAGE flag "--readonly" help="Build with -mod=readonly"

set -e

args=()
if [ "${usage_readonly:-false}" = "true" ]; then
    args+=("-mod=readonly")
fi

git_sha=$(git rev-parse HEAD)
version=$(node -p "require('./package.json').version")

CGO_ENABLED=0 go \
    build \
    "${args[@]}" \
    -trimpath \
    -ldflags="-s -w -X github.com/speakeasy-api/gram/cli/internal/app.GitSHA=${git_sha} -X github.com/speakeasy-api/gram/cli/internal/app.Version=${version}" \
    -o bin/gram \
    ./main.go