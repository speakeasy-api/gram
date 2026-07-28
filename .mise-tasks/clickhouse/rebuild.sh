#!/usr/bin/env bash
#MISE description="Rebuild the local ClickHouse image to pick up config changes (users.d, config.d)"

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../../local/lib/compose.sh
source "${REPO_ROOT}/local/lib/compose.sh"

# Builds never go through compose (podman's docker-compat socket has no
# BuildKit): rebuild the image directly, then recreate the container from it.
rebuild_clickhouse_image
compose up -d --force-recreate clickhouse

until curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${CLICKHOUSE_HTTP_PORT}/?user=${CLICKHOUSE_USERNAME}&password=${CLICKHOUSE_PASSWORD}&query=SELECT+1" | grep -q 200; do
    echo "Waiting for ClickHouse to be ready..."
    sleep 1
done

echo "ClickHouse rebuilt and ready."
