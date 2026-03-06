#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the API server"

GIT_SHA=$(git rev-parse HEAD)

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

# Export ldflags for air's build command
export AIR_BUILD_LDFLAGS="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}"

# Use air for hot reload - args after -- are passed to the binary
air -- start "${CONFIG_ARGS[@]}" "$@"
