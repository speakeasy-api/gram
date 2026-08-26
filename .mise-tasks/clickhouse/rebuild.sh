#!/usr/bin/env bash

#MISE description="Restart the shared ClickHouse server to reload mounted configuration"

set -e

echo "⚠️  Restarting shared ClickHouse interrupts every worktree." >&2
docker compose -f compose.shared.yml -p gram-shared up -d --force-recreate \
    --wait --wait-timeout 30 clickhouse
echo "Shared ClickHouse restarted and healthy."
