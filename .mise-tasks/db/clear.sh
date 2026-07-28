#!/usr/bin/env bash

#MISE description="Clear all data from the development database"
#MISE hide=true

set -eo pipefail

# shellcheck source=../../local/lib/compose.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/local/lib/compose.sh"

echo "Truncating projects and deployment_statuses tables..."

compose exec -T gram-db psql -U "${DB_USER}" -d "${DB_NAME}" -c "TRUNCATE projects, deployment_statuses CASCADE;"

echo "Tables truncated successfully!"