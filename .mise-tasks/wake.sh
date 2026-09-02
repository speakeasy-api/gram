#!/usr/bin/env bash

#MISE description="Start this worktree's paused stack: containers, then daemons"
#MISE dir="{{ config_root }}"

set -e

# A new worktree boots its stack and then pauses it (see .config/wt.toml and
# `git:workboot`), so the containers, migrations and seed data are already
# there and waking is just starting them again. `infra:start` is idempotent and
# does the starting -- `docker compose up -d` starts stopped containers -- plus
# the readiness waits and the Temporal namespace check.

gitdir="$(git rev-parse --absolute-git-dir)"

# `mise run pause` parks a placeholder server on the site port (see
# `mise run park`). It has to let go before vite can bind, and a wake started
# from the CLI never goes through the parker's own hand-over path — so kill it
# here. A parker that already exited (it woke us) leaves a stale pid file.
parked="$gitdir/gram-stack-parked.pid"
if [ -f "$parked" ]; then
    kill "$(cat "$parked")" 2> /dev/null || true
    rm -f "$parked"
fi

# No containers at all means this worktree was never booted (or was nuked), so
# there is nothing to wake: migrations have never run and the databases are
# empty. Do the full boot instead of starting daemons against an empty DB.
if [ -z "$(docker compose ps -aq 2>/dev/null)" ]; then
    echo "No containers for this worktree yet — running a full boot instead."
    exec ./zero --agent
fi

mise run infra:start
mise run start

rm -f "$gitdir/gram-stack-paused"
