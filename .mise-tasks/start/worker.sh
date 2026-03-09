#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"

# Check if air should be disabled
if [ "${GRAM_DISABLE_AIR:-0}" = "1" ]; then
    # Run directly without air
    mise run -q build:server && ./bin/gram worker "$@"
else
    # Use air for hot reload - args after -- are passed to the binary
    air -- worker "$@"
fi
