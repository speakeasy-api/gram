#!/usr/bin/env bash

#MISE description="Adversarial UI states judged by Jev, against this worktree's stack (paid: a few cents per run)"
#MISE dir="{{ config_root }}/e2e"
#MISE depends=["ensure-stack"]

set -euo pipefail

# Not part of `mise run test`: it drives a real browser against a real stack
# and every case pays for a Jev judgement. Nightly and on demand in CI
# (.github/workflows/ui-fuzz.yml); run it here while adding screens, mutations
# or questions. Extra arguments go to `playwright test`, so `--grep empty`
# runs one mutation.

if [ -z "${OPENROUTER_DEV_KEY:-}" ] || [ "${OPENROUTER_DEV_KEY}" = "unset" ]; then
    echo "OPENROUTER_DEV_KEY is not set; run \`mise run zero:openrouter\` first." >&2
    exit 1
fi

aube install -F @gram/e2e

if [ -n "${CI:-}" ]; then
    aube exec playwright install --with-deps chromium
else
    aube exec playwright install chromium
fi

# The pure parts first: a broken question set or verdict rule should fail
# before anything is paid for.
node --test ui-fuzz/*.test.ts

exec aube exec playwright test --config ui-fuzz.config.ts "$@"
