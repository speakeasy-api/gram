#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

docker compose up -d || exit 1

# Use psql to wait for the databases to be ready
until docker compose exec gram-db psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" > /dev/null 2>&1; do
    echo "Waiting for databases to be ready..."
    sleep 1
done
