#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Apply pending migrations"

#USAGE flag "--dry" help="Enable dry run mode"

set -e

args=()

if [ "${usage_dry:-false}" = "true" ]; then
  args+=("--dry-run")
fi

# run clickhouse migrations
echo "Running ClickHouse migrations..."
mise run clickhouse:migrate

exec atlas migrate apply \
  --config file://atlas.hcl \
  -u "${GRAM_DATABASE_URL}" \
  "${args[@]}"

