#!/usr/bin/env bash

#MISE description="End-to-end test of the pause/resume worktree lifecycle (drives this worktree's stack)"
#MISE dir="{{ config_root }}/e2e"

set -e

# Deliberately does NOT depend on `ensure-stack`, unlike `mise run playwright`:
# the point of this suite is to hit a PAUSED stack, and a dependency that woke
# it first would test nothing. The suite calls `ensure-stack` itself, at the
# step where a booted stack is the precondition rather than the obstacle.
#
# Not part of `mise run test` either: it pauses and wakes real containers, takes
# minutes, and leaves the stack running. In CI it has a workflow of its own
# (.github/workflows/wake-stack-e2e.yml), which boots a stack first and runs
# only when the lifecycle files change.

# Unconditional rather than guarded on node_modules: a guard makes this a
# one-time install, so bumping the runner in e2e/package.json would leave the
# old one in place and the browser install below would then fetch a build that
# does not match it. An install that has nothing to do costs about a second.
aube install -F @gram/e2e

# Separate from the `playwright-cli` browser install: this is Playwright's own
# CLI, which fetches the build the pinned test runner expects. Idempotent, and
# a no-op once the browser is in the shared cache.
if [ -n "${CI:-}" ]; then
    # Chromium's shared libraries are not on a bare runner. `--with-deps` needs
    # root and only knows how to install them on Debian/Ubuntu, so it stays out
    # of the developer path, where the browser already works.
    aube exec playwright install --with-deps chromium
else
    aube exec playwright install chromium
fi

exec aube exec playwright test "$@"
