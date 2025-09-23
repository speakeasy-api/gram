#!/usr/bin/env bash

#MISE description="Rollback clickhouse migrations"
#MISE dir="{{ config_root }}/server"

set -e

echo "Rolling back ClickHouse migrations..."

# Apply down migrations using clickhouse-client
for migration_file in $(ls -r clickhouse/migrations/*.down.sql 2>/dev/null || true); do
    if [ -f "$migration_file" ]; then
        echo "Rolling back migration: $(basename "$migration_file")"
        clickhouse client \
            --user "$CLICKHOUSE_USERNAME" \
            --password "$CLICKHOUSE_PASSWORD" \
            --host "$CLICKHOUSE_HOST" \
            --port "$CLICKHOUSE_NATIVE_PORT" \
            --database "$CLICKHOUSE_DATABASE" \
            --query "$(cat "$migration_file")"
    fi
done

echo "ClickHouse migrations rolled back successfully!"
