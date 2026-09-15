#!/usr/bin/env bash

#MISE description="Restart Temporal to clear SQLite locks"
#MISE hide=true

set -e

# Restart the shared Temporal container to clear SQLite locks from previous
# runs. This affects every worktree, so zero keeps it interactive-only.
shared_compose=(docker compose -f compose.shared.yml -p gram-shared)
if "${shared_compose[@]}" ps gram-temporal --status running -q 2>/dev/null | grep -q .; then
    echo "Restarting shared Temporal container..."
    "${shared_compose[@]}" restart gram-temporal
    until "${shared_compose[@]}" exec -T gram-temporal temporal operator cluster health 2>/dev/null; do
        echo "Waiting for Temporal to be healthy..."
        sleep 2
    done
fi
