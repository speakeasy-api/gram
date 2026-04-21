#!/usr/bin/env bash
#MISE description="Rebuild the local ClickHouse image to pick up config changes (users.d, config.d)"

set -e

docker compose up -d --build clickhouse

until curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${CLICKHOUSE_HTTP_PORT}/?user=${CLICKHOUSE_USERNAME}&password=${CLICKHOUSE_PASSWORD}&query=SELECT+1" | grep -q 200; do
    echo "Waiting for ClickHouse to be ready..."
    sleep 1
done

echo "ClickHouse rebuilt and ready."
