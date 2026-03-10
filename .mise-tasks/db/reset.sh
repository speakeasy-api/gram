#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Drop and recreate the database schema, then re-run all migrations"

set -e

echo "Dropping and recreating public schema..."

docker compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

echo "Schema reset. Running migrations..."

mise run db:migrate
