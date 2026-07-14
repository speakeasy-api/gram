#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

docker compose up -d || exit 1

# Use psql to wait for the databases to be ready
until docker compose exec -T gram-db psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; do
    echo "Waiting for databases to be ready..."
    sleep 1
done

# ClickHouse takes longer than Postgres to accept queries. Migrations run
# immediately after infra starts, so without waiting here the first ClickHouse
# migration can fail with a connection EOF.
until docker compose exec -T clickhouse clickhouse-client --user "$CLICKHOUSE_USERNAME" --password "$CLICKHOUSE_PASSWORD" -q "SELECT 1" > /dev/null 2>&1; do
    echo "Waiting for ClickHouse to be ready..."
    sleep 1
done
