#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Create a versioned migration"
#USAGE arg "<name>" help="The name of the migration"

set -e

if [ "${usage_name:-}" = "" ]; then
  echo "Usage: $0 <name>"
  exit 1
fi

echo "Generating atlas migrations..."
atlas migrate diff "${usage_name:?}" \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl \
  --to file://clickhouse/schema.sql \
  --dev-url "docker://clickhouse/clickhouse-server/26.2.19.43@sha256:c2f2605585899d5103a0447daadbc0005f362200d5f0fcca7f40db3ca0dd36dd/dev"


# We also generate golang-migrate migrations so to allow 
# any user to run clickhouse migrations without requiring an 
# atlas login
echo "Generating golang-migrate migrations..."
atlas migrate diff "${usage_name:?}" \
  --dir file://clickhouse/local/golang_migrate?format=golang-migrate \
  --config file://atlas.hcl \
  --to file://clickhouse/schema.sql \
  --dev-url "docker://clickhouse/clickhouse-server/26.2.19.43@sha256:c2f2605585899d5103a0447daadbc0005f362200d5f0fcca7f40db3ca0dd36dd/dev" \
  --format "{{ sql . \"  \" }}"

mise run clickhouse:gen-materialized-cols