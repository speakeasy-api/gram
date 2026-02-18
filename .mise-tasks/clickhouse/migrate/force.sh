#!/usr/bin/env bash

#MISE description="Force a specific ClickHouse migration version (fixes dirty state)"
#MISE dir="{{ config_root }}/server"
#USAGE arg "<version>" help="The migration version to force (e.g., 20251217163326)"

set -e

if [ "${usage_version:-}" = "" ]; then
  echo "Usage: mise clickhouse:force <version>"
  echo "Example: mise clickhouse:force 20251217163326"
  exit 1
fi

echo "Forcing ClickHouse migration version to ${usage_version}..."

exec migrate \
  -source "file://clickhouse/local/golang_migrate" \
  -database "${GRAM_CLICKHOUSE_GOMIGRATE_URL}" \
  force "${usage_version}"
