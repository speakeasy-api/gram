#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Drop and recreate the database schema, then re-run all migrations"

set -e

echo "Dropping and recreating public schema..."

psql "${GRAM_DATABASE_URL//&search_path=public/}" \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

echo "Schema reset. Running migrations..."

mise run db:migrate
