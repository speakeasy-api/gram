#!/usr/bin/env bash

#MISE description="Authorization end-to-end suite (API contract matrix and dashboard journeys) against this worktree's running stack"
#MISE dir="{{ config_root }}/e2e"
#MISE depends=["ensure-stack"]

set -e

# Needs a booted, seeded stack: `./zero --agent` on a fresh checkout. The
# `ensure-stack` dependency wakes a paused one. Not part of `mise run test`: it
# drives a real server and browser. In CI it has its own workflow
# (.github/workflows/rbac-e2e.yml).
#
# Extra arguments go to `playwright test`, for example:
#   mise run test:rbac-e2e --project=contract
#   mise run test:rbac-e2e --grep "projects"

aube install -F @gram/e2e

# RBAC_E2E_CHROMIUM_PATH points the suite at a browser already on the machine,
# for environments where Playwright's download host is unreachable.
if [ -n "${RBAC_E2E_CHROMIUM_PATH:-}" ]; then
    :
elif [ -n "${CI:-}" ]; then
    aube exec playwright install --with-deps chromium
else
    aube exec playwright install chromium
fi

exec aube exec playwright test --config rbac.config.ts "$@"
