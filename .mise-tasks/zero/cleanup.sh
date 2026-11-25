#!/usr/bin/env bash

#MISE description="Restart Temporal to clear SQLite locks"
#MISE hide=true

set -e

# Restart only this worktree's Temporal server to clear SQLite locks.
local_compose=(docker compose)
if "${local_compose[@]}" ps gram-temporal --status running -q 2>/dev/null | grep -q .; then
    echo "Restarting Temporal container..."
    "${local_compose[@]}" restart gram-temporal
    until "${local_compose[@]}" exec -T gram-temporal temporal operator cluster health 2>/dev/null; do
        echo "Waiting for Temporal to be healthy..."
        sleep 2
    done
fi
