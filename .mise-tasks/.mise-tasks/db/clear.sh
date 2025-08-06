#!/usr/bin/env bash

#MISE description="Clear all data from the development database"
#MISE hide=true

set -eo pipefail

echo "Truncating projects and deployment_statuses tables..."

docker compose exec gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -c "TRUNCATE projects, deployment_statuses CASCADE;"

echo "Tables truncated successfully!"