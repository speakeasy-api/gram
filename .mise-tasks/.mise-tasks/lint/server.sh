#!/usr/bin/env bash

#MISE description="Run golangci-lint on the server codebase"
#MISE dir="{{ config_root }}/server"
#MISE sources=["server/**/*.go", ".golangci.yml"]
#MISE outputs=["server/**/*.go", ".golangci.yml"]

#USAGE flag "--long" help="Enable more detailed reporting"

set -e

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

exec golangci-lint run --max-issues-per-linter=0 "${args[@]}" ./...
