#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

profile=()
if [[ "$GRAM_ENABLE_OTEL_TRACES" == "1" || "$GRAM_ENABLE_OTEL_METRICS" == "1" ]]; then
    profile=(--profile observability)
    echo "💡 Starting Grafana on port $GRAFANA_PORT..."
fi

docker compose "${profile[@]}" up -d || exit 1

# Use psql to wait for the databases to be ready
until docker compose exec gram-db psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; do
    echo "Waiting for databases to be ready..."
    sleep 1
done
