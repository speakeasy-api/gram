#!/usr/bin/env bash

#MISE description="Seed fictional paying customers with ~6 months of metered usage for the admin Customer usage page (local development only)"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Opt-in and local-only: writes fictional organizations to Postgres and their
# meter readings to ClickHouse. Both must already be running. Safe to rerun;
# usage is dated relative to today, so a rerun brings it up to date.
cd server
exec go run . customer-usage-seed "$@"
