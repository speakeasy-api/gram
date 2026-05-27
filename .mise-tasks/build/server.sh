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
# Allow CI to compute the runtime image hash once and pass it through so the
# server binary and the runtime image push agree on a single canonical value
# regardless of intermediate filesystem state on a runner.
runtime_image_hash=${GRAM_ASSISTANT_RUNTIME_IMAGE_HASH:-$(mise run hash:assistant-runtime-image)}

if [ -z "${runtime_image_hash}" ]; then
    echo "build:server: runtime image hash is empty" >&2
    exit 1
fi

CGO_ENABLED=0 go \
    build \
    "${args[@]}" \
    -trimpath \
    -ldflags="-s -w -X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${git_sha} -X github.com/speakeasy-api/gram/server/cmd/gram.AssistantRuntimeImageHash=${runtime_image_hash} -X goa.design/clue/health.Version=${git_sha}" \
    -o bin/gram \
    ./main.go