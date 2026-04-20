#!/usr/bin/env bash

#MISE description="Run golangci-lint on the server codebase"
#MISE dir="{{ config_root }}/server"

#USAGE flag "--long" help="Enable more detailed reporting"

set -e

args=(--show-stats=false --output.text.print-issued-lines=false)
if [ "${usage_long:-false}" = "true" ]; then
    args=()
fi

if [ ! -x ./bin/gcl ] || [ -n "$(find ../glint .custom-gcl.yml ../go.mod ../go.sum -newer ./bin/gcl -type f 2>/dev/null)" ]; then
    golangci-lint custom --destination ./bin --name gcl
fi

exec ./bin/gcl run --max-issues-per-linter=0 "${args[@]}" ./...
