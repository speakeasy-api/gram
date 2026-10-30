#!/usr/bin/env bash

#MISE description="Replay the dashboard flow checks (e2e/flows) against this worktree's stack"
#MISE dir="{{ config_root }}/e2e"
#MISE depends=["ensure-stack"]

set -e

# Same install and browser handling as test:wake-stack: unconditional so a
# runner bump in e2e/package.json never leaves a mismatched browser behind.
aube install -F @gram/e2e

if [ -n "${CI:-}" ]; then
    aube exec playwright install --with-deps chromium
else
    aube exec playwright install chromium
fi

exec aube exec playwright test --config flows.config.ts "$@"
