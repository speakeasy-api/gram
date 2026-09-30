#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Admin API server"
#MISE hide=true

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

go run main.go admin "${CONFIG_ARGS[@]}" "$@"
