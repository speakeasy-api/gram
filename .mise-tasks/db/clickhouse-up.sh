#!/usr/bin/env bash

#MISE description="Apply pending clickhouse migrations"
#MISE dir="{{ config_root }}/server"

set -e

echo "Applying ClickHouse migrations..."

# Apply migrations using clickhouse-client
for migration_file in clickhouse/migrations/*.up.sql; do
    if [ -f "$migration_file" ]; then
        echo "Applying migration: $(basename "$migration_file")"
        docker exec gram-clickhouse-1 clickhouse-client \
            --user "$CLICKHOUSE_USER" \
            --password "$CLICKHOUSE_PASSWORD" \
            --host "$CLICKHOUSE_HOST" \
            --port "$CLICKHOUSE_NATIVE_PORT" \
            --database "$CLICKHOUSE_DB" \
            --query "$(cat "$migration_file")"
    fi
done

echo "ClickHouse migrations completed successfully!"
