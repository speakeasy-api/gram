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
# `mise run park`), which serves the "Resuming stack" page for as long as it
# holds it. Killing it is deferred until the containers are up, just before
# vite binds: a wake takes tens of seconds, and every reload or extra tab in
# that window should get the page rather than a connection error.
parked="$gitdir/gram-stack-parked.pid"
release_port() {
    if [ -f "$parked" ]; then
        kill "$(cat "$parked")" 2> /dev/null || true
        rm -f "$parked"
        # The listener closes on signal delivery, not on return from kill.
        sleep 1
    fi
}

# No containers at all means this worktree was never booted (or was nuked), so
# there is nothing to wake: migrations have never run and the databases are
# empty. Do the full boot instead of starting daemons against an empty DB.
if [ -z "$(docker compose ps -aq 2>/dev/null)" ]; then
    echo "No containers for this worktree yet — running a full boot instead."
    release_port
    exec ./zero --agent
fi

mise run infra:start
release_port
mise run start

rm -f "$gitdir/gram-stack-paused"
