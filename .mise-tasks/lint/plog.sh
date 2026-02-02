#!/usr/bin/env bash

#MISE description="Run golangci-lint on the plog package"
#MISE dir="{{ config_root }}/plog"

#USAGE flag "--long" help="Enable more detailed reporting"

set -e

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

exec golangci-lint run --max-issues-per-linter=0 "${args[@]}" ./...
