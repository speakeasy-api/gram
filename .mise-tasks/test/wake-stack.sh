#!/usr/bin/env bash

#MISE description="End-to-end test of the pause/resume worktree lifecycle (drives this worktree's stack)"
#MISE dir="{{ config_root }}/e2e"

set -e

# Deliberately does NOT depend on `ensure-stack`, unlike `mise run playwright`:
# the point of this suite is to hit a PAUSED stack, and a dependency that woke
# it first would test nothing. The suite calls `ensure-stack` itself, at the
# step where a booted stack is the precondition rather than the obstacle.
#
# Not part of `mise run test` either. It pauses and wakes real containers, takes
# minutes, and leaves this worktree's stack running -- run it by hand when
# touching pause/wake/park.

if [ ! -d node_modules/@playwright/test ]; then
    echo "Installing the e2e suite's dependencies..." >&2
    aube install -F @gram/e2e
fi

# Separate from the `playwright-cli` browser install: this is Playwright's own
# CLI, which fetches the build the pinned test runner expects. Idempotent, and
# a no-op once the browser is in the shared cache.
aube exec playwright install chromium

exec aube exec playwright test "$@"
