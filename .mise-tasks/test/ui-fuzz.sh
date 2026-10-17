#!/usr/bin/env bash

#MISE description="Adversarial UI states on every static dashboard route, judged by Jev, against this worktree's stack (paid: a dollar or two per full run)"
#MISE dir="{{ config_root }}/e2e"
#MISE depends=["ensure-stack"]

#USAGE flag "--skip-record" help="Reuse the latest recording of route dependencies instead of visiting every route first"
#USAGE arg "[playwright-args]..." help="Passed to the fuzz step's `playwright test`, e.g. --grep mcp"

set -euo pipefail

# Not part of `mise run test`: it drives a real browser against a real stack
# and every case pays for a Jev judgement. Nightly and on demand in CI
# (.github/workflows/ui-fuzz.yml); run it here while adding mutators or
# questions.
#
# Three steps share one run id: record which rpc calls each static route
# makes (free), fuzz each of those dependencies (paid), then merge the
# per-worker records into cases.jsonl and print the summary.

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

export UI_FUZZ_RUN_ID="${UI_FUZZ_RUN_ID:-$(date -u +%Y%m%dT%H%M%S)-$(git rev-parse --short=8 HEAD)}"
echo "ui-fuzz run ${UI_FUZZ_RUN_ID}"

if [ "${usage_skip_record:-false}" != "true" ]; then
    # A redirecting route fails its record test but is still written down,
    # so the fuzz step can skip it with a reason. Keep going.
    aube exec playwright test --config ui-fuzz.config.ts ui-fuzz-record.spec.ts || true
fi

status=0
# shellcheck disable=SC2086
aube exec playwright test --config ui-fuzz.config.ts ui-fuzz.spec.ts ${usage_playwright_args:-} || status=$?

node ui-fuzz/summarize.ts
exit "$status"
