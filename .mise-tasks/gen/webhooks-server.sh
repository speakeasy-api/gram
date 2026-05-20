#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Generate server/internal/outbox/events/catalog_gen.go and catalog_gen.yaml"

#USAGE flag "-c --check" help="Verify catalog_gen.go and catalog_gen.yaml are up to date without writing"

set -e
args=()
if [[ "${usage_check:-}" == "true" ]]; then
    args=(--check)
fi
exec go run ./cmd/gen-webhooks "${args[@]}"
