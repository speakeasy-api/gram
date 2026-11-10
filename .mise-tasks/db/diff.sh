#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Create a versioned migration"
#USAGE arg "<name>" help="The name of the migration"

set -e

if [ "${usage_name:-}" = "" ]; then
  echo "Usage: $0 --name <name>"
  exit 1
fi

exec atlas migrate diff "${usage_name:?}" \
  --config file://atlas.hcl \
  --to file://database/schema.sql \
  --dev-url "docker://pgvector/pgvector/0.8.7-pg18-bookworm@sha256:2358fcba361ed2233a5ed81b5fe4ca779ccb304120ce531a3bf51c0ed7e2bc11/dev?search_path=public"