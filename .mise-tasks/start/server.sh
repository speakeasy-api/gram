#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the API server"
#MISE sources=["server/**/*.go"]

GIT_SHA=$(git rev-parse HEAD)

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go start "${CONFIG_ARGS[@]}" "$@"