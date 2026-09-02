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

# See the same block in `mise run pause`: two wakes can be asked for at once
# (the parker's Resume button and an `ensure-stack` dependency), and a wake can
# race a pause. Duplicated rather than sourced from a helper because every file
# under .mise-tasks/ is itself a task.
lock="$gitdir/gram-stack-lock"
for _ in $(seq 1 60); do
    if mkdir "$lock" 2> /dev/null; then
        trap 'rmdir "$lock" 2> /dev/null || true' EXIT
        break
    fi
    if [ -n "$(find "$lock" -maxdepth 0 -mmin +10 2> /dev/null)" ]; then
        echo "Clearing a stale stack lock." >&2
        rmdir "$lock" 2> /dev/null || true
    fi
    sleep 1
done

# A boot builds the stack from nothing and ends by pausing it. Waking underneath
# one races its migrations and its seed.
if [ -f "$gitdir/gram-stack-boot.pid" ]; then
    boot_pid=$(cat "$gitdir/gram-stack-boot.pid")
    if ps -o command= -p "$boot_pid" 2> /dev/null | grep -q workboot; then
        echo "This worktree is still booting — watch it with \`mise run git:workstatus\`." >&2
        exit 1
    fi
fi

# `mise run pause` parks a placeholder server on the site port (see
# `mise run park`), which serves the resume page for as long as it holds it.
# Killing it is deferred until the containers are up, just before vite binds: a
# wake takes tens of seconds, and every reload or extra tab in that window
# should get the page rather than a connection error.
parked="$gitdir/gram-stack-parked.pid"
release_port() {
    if [ -f "$parked" ]; then
        # A pid file can outlive its process (SIGKILL, a reboot) and pids are
        # reused, so check what the pid actually is before signalling it.
        park_pid=$(cat "$parked")
        if ps -o command= -p "$park_pid" 2> /dev/null | grep -q "park"; then
            kill "$park_pid" 2> /dev/null || true
            # The listener closes on signal delivery, not on return from kill.
            sleep 1
        fi
        rm -f "$parked"
    fi
}

# No containers at all means this worktree was never booted (or was nuked), so
# there is nothing to wake: migrations have never run and the databases are
# empty. Distinguish that from a compose that failed to answer -- treating an
# error as "no containers" would run `zero`, whose seed rewrites local data.
if ! containers="$(docker compose ps -aq 2> /dev/null)"; then
    echo "\`docker compose ps\` failed — is the Docker daemon running?" >&2
    exit 1
fi

if [ -z "$containers" ]; then
    echo "No containers for this worktree yet — running a full boot instead."
    release_port
    # Same markers and the same cold-volume timeout `git:workboot` uses: `exec`
    # replaces this shell, so nothing here runs afterwards to clean up.
    rm -f "$gitdir/gram-stack-paused" "$gitdir/gram-stack-lastseen"
    rmdir "$lock" 2> /dev/null || true
    trap - EXIT
    exec env INFRA_READINESS_TIMEOUT=300 ./zero --agent
fi

# `pause` stops every profile, so start every container that exists before
# `infra:start` asserts the default ones. Without this a worktree that had an
# optional profile up (litellm, local-registry) comes back missing it.
docker compose --profile "*" start > /dev/null 2>&1 || true

mise run infra:start
release_port
mise run start

# A stack that is up is no longer paused, is no longer failed (a `failed`
# marker outranks `paused` in git:workstatus, so a recovered worktree would
# keep reading as broken), and starts its idle clock from now.
rm -f "$gitdir/gram-stack-paused" "$gitdir/gram-stack-boot.failed" \
    "$gitdir/gram-stack-lastseen"
