#!/usr/bin/env bash

#MISE description="Apply pending clickhouse migrations"
#MISE dir="{{ config_root }}/server"
#USAGE flag "--dry" help="Enable dry run mode"
#USAGE flag "--url <clickhouse-url>" help="The URL to ClickHouse server"
#USAGE flag "--engine <engine>" {
#USAGE   env "CLICKHOUSE_MIGRATION_ENGINE"
#USAGE   choices "atlas" "golang-migrate"
#USAGE   default "atlas"
#USAGE   help "Which engine to use to apply migrations against ClickHouse"
#USAGE }

set -e

echo "Applying ClickHouse migrations..."

migration_engine="${usage_engine:?Error: migration engine is required}"

if [ "$migration_engine" = "golang-migrate" ]; then
  echo "Using golang-migrate engine"

  if [ "${usage_dry:-false}" = "true" ]; then
    echo "Dry run mode is not supported with golang-migrate"
    exit 1
  fi

  ch_url=${usage_url:-${GRAM_CLICKHOUSE_GOMIGRATE_URL:?Error: --url or GRAM_CLICKHOUSE_GOMIGRATE_URL is required}}

  exec migrate \
    -path clickhouse/local/golang_migrate \
    -database "${ch_url}" \
    up
else
  echo "Using atlas engine"

  ch_url=${usage_url:-${GRAM_CLICKHOUSE_URL:?Error: --url or GRAM_CLICKHOUSE_URL is required}}

  args=()

  if [ "${usage_dry:-false}" = "true" ]; then
    args+=("--dry-run")
  fi

  atlas whoami &>/dev/null || atlas login

  exec atlas migrate apply \
    --dir file://clickhouse/migrations \
    --config file://atlas.hcl \
    -u "${ch_url}" \
    "${args[@]}"
fi

echo "ClickHouse migrations completed successfully!"
