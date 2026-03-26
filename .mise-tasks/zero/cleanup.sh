#!/usr/bin/env bash

#MISE description="Restart Temporal to clear SQLite locks"
#MISE hide=true

set -e

# Restart Temporal container to clear any SQLite locks from previous runs
# The health check doesn't catch SQLite lock issues, so we restart unconditionally
if docker compose ps gram-temporal --status running -q 2>/dev/null | grep -q .; then
    echo "Restarting Temporal container..."
    docker compose restart gram-temporal
    until docker compose exec gram-temporal temporal operator cluster health 2>/dev/null; do
        echo "Waiting for Temporal to be healthy..."
        sleep 2
    done
fi
