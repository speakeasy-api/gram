#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"

<<<<<<< HEAD
# Check if air should be disabled
if [ "${GRAM_DISABLE_AIR:-0}" = "1" ]; then
    # Run directly without air
    mise run -q build:server && ./bin/gram worker "$@"
else
    # Use air for hot reload - args after -- are passed to the binary
    air -- worker "$@"
fi
=======
GIT_SHA=$(git rev-parse HEAD)
go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go worker
>>>>>>> main
