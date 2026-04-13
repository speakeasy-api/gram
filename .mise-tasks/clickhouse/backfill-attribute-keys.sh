#!/usr/bin/env bash
#MISE description="Backfill the attribute_keys table from existing telemetry_logs"
#MISE depends=["clickhouse:migrate"]
#USAGE flag "--dry" help="Print the query without executing"

set -e

QUERY="INSERT INTO attribute_keys
SELECT
    gram_project_id,
    arrayJoin(JSONAllPaths(attributes)) AS attribute_key,
    min(time_unix_nano) AS first_seen_unix_nano,
    max(time_unix_nano) AS last_seen_unix_nano
FROM telemetry_logs
GROUP BY gram_project_id, attribute_key"

if [ "${usage_dry:-false}" = "true" ]; then
  echo "Dry run — query that would be executed:"
  echo "$QUERY"
  exit 0
fi

echo "Backfilling attribute_keys from telemetry_logs..."

clickhouse client \
  --host "${CLICKHOUSE_HOST}" \
  --port "${CLICKHOUSE_NATIVE_PORT}" \
  --user "${CLICKHOUSE_USERNAME}" \
  --password "${CLICKHOUSE_PASSWORD}" \
  --database "${CLICKHOUSE_DATABASE}" \
  --secure \
  --query "$QUERY"

echo "Backfill complete."
