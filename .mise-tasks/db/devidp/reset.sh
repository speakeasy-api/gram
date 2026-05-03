#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Drop and recreate the dev-idp database public schema, then re-apply"

set -e

echo "Dropping and recreating public schema in gram_devidp..."

docker compose exec -T gram-db psql -U "${DB_USER}" -d gram_devidp \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

echo "Schema reset. Applying dev-idp schema..."

mise run db:devidp:apply
