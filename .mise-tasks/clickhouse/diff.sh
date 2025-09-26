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
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl \
  --to file://clickhouse/schema.sql \
  --dev-url "docker://clickhouse/clickhouse-server/25.8.3/dev"