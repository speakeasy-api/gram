#!/usr/bin/env bash

#MISE description="Apply pending clickhouse migrations"
#MISE dir="{{ config_root }}/server"
#USAGE flag "--dry" help="Enable dry run mode"

set -e

echo "Applying ClickHouse migrations..."

migration_engine="${CLICKHOUSE_MIGRATION_ENGINE:-atlas}"

if [ "$migration_engine" = "golang-migrate" ]; then
  echo "Using golang-migrate engine..."

  if [ "${usage_dry:-false}" = "true" ]; then
    echo "Dry run mode is not supported with golang-migrate"
    exit 1
  fi

  exec migrate \
    -path clickhouse/migrations/golang_migrate \
    -database "${GRAM_CLICKHOUSE_MIGRATE_URL}" \
    up
else
  echo "Using atlas engine..."

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
fi

echo "ClickHouse migrations completed successfully!"
