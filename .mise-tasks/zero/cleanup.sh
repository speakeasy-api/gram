#!/usr/bin/env bash

#MISE description="Restart Temporal to clear SQLite locks"
#MISE hide=true

set -e

# shellcheck source=/dev/null
. "${MISE_PROJECT_ROOT:-$(git rev-parse --show-toplevel)}/local/lib/compose.sh"

# Restart Temporal container to clear any SQLite locks from previous runs
# The health check doesn't catch SQLite lock issues, so we restart unconditionally
if compose ps gram-temporal --status running -q 2>/dev/null | grep -q .; then
    echo "Restarting Temporal container..."
    compose restart gram-temporal
    until compose exec -T gram-temporal temporal operator cluster health 2>/dev/null; do
        echo "Waiting for Temporal to be healthy..."
        sleep 2
    done
fi
