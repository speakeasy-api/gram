#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Start up the API server"

CONFIG_ARGS=()
if [ -f "../config.local.toml" ]; then
    CONFIG_ARGS=(--config-file ../config.local.toml)
fi

# Use air for hot reload - args after -- are passed to the binary
air -- start "${CONFIG_ARGS[@]}" "$@"
