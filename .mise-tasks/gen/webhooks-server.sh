#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Generate server/internal/outbox/events/catalog_gen.go and catalog_gen.yaml"

#USAGE flag "-c --check" help="Verify catalog_gen.go and catalog_gen.yaml are up to date without writing"

set -e

# `go run` builds a throwaway binary and stamps VCS info, which has crashed
# CI with `signal: bus error` while shelling out to git. Codegen binaries
# are never shipped, so the stamp buys nothing.
export GOFLAGS="-buildvcs=false ${GOFLAGS:-}"

args=()
if [[ "${usage_check:-}" == "true" ]]; then
    args=(--check)
fi
exec go run ./cmd/gen-webhooks "${args[@]}"
