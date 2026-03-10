#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the API server"

GIT_SHA=$(git rev-parse HEAD)

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

<<<<<<< HEAD
# Check if air should be disabled
if [ "${GRAM_DISABLE_AIR:-0}" = "1" ]; then
    # Run directly without air
    mise run -q build:server && ./bin/gram start "${CONFIG_ARGS[@]}" "$@"
else
    # Use air for hot reload - args after -- are passed to the binary
    air -- start "${CONFIG_ARGS[@]}" "$@"
fi
=======
go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go start "${CONFIG_ARGS[@]}" "$@"
>>>>>>> main
