#!/usr/bin/env bash

#MISE description="Apply pending clickhouse migrations"
#MISE dir="{{ config_root }}/server"

set -e

echo "Applying ClickHouse migrations..."

args=()

if [ "${usage_dry:-false}" = "true" ]; then
  args+=("--dry-run")
fi

atlas login

exec atlas migrate apply \
  --dir file://clickhouse/migrations \
  --config file://atlas.hcl \
  -u "${GRAM_CLICKHOUSE_URL}" \
  "${args[@]}"

echo "ClickHouse migrations completed successfully!"
