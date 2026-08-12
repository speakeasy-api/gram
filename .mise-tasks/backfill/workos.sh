#!/usr/bin/env bash

#MISE description="Run the local WorkOS backfill script"
#MISE dir="{{ config_root }}"

set -euo pipefail

exec go run ./server/cmd/workos-backfill "$@"
