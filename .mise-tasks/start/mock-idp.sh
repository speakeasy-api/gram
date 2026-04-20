#!/usr/bin/env bash

#MISE description="Start the mock Speakeasy IDP server for local development auth"

set -e

if [[ -n "$USE_LOCAL_SPEAKEASY_REGISTRY_AUTH" ]]; then
  echo "USE_LOCAL_SPEAKEASY_REGISTRY_AUTH is set, skipping mock-idp."
  # Sleep forever so madprocs doesn't restart the process.
  exec sleep infinity
fi

exec go run ./mock-speakeasy-idp/main
