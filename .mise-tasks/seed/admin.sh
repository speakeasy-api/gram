#!/usr/bin/env bash

#MISE description="Seed fictional admin-list organizations in the local development DB only"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Opt-in and Postgres-only: do not call the ordinary or production demo seed.
# The database must already be running; avoid waking unrelated services.
cd server
exec go run . admin-seed "$@"
