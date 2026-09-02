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

# `ln -s` is the atomic test-and-set: creating a symlink fails if the name
# exists, and its target carries the owner's pid, so the lock and the identity
# of its owner appear in one step. (A lock file written after a `mkdir` has a
# window where it exists with no owner recorded, which another process reads as
# abandoned.) The target is a pid string and never has to resolve.
release_lock() {
    # Only release a lock this process still owns.
    if [ "$(readlink "$lock" 2> /dev/null)" = "$$" ]; then
        rm -f "$lock"
    fi
}

# Clearing a dead owner's lock cannot be done from the plain retry loop:
# between reading the owner and removing it, another process can acquire the
# lock, and the removal then evicts a live holder -- two commands end up
# running against the same containers. So removal happens under its own lock,
# which is only ever taken by exclusive create; whoever holds it re-reads the
# owner before removing anything.
reap_stale_lock() {
    local reap="$lock.reap" owner
    ln -s "$$" "$reap" 2> /dev/null || return 1
    owner="$(readlink "$lock" 2> /dev/null || true)"
    if [ -n "$owner" ] && ! kill -0 "$owner" 2> /dev/null; then
        echo "Clearing a stack lock left behind by a dead process ($owner)." >&2
        rm -f "$lock"
    fi
    rm -f "$reap"
}

locked=false
for _ in $(seq 1 60); do
    if ln -s "$$" "$lock" 2> /dev/null; then
        trap release_lock EXIT
        locked=true
        break
    fi
    reap_stale_lock || true
    sleep 1
done

if [ "$locked" != true ]; then
    owner="$(readlink "$lock" 2> /dev/null || true)"
    echo "Another pause or wake (pid ${owner:-unknown}) has held this worktree's stack lock for a minute; giving up." >&2
    echo "If nothing is running, remove $lock and retry." >&2
    exit 1
fi

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
    rm -f "$gitdir/gram-stack-paused" "$gitdir/gram-stack-lastseen"
    # Not `exec`: a bare `zero --agent` publishes no boot marker of its own, so
    # this lock is the only thing keeping a pause or another wake off the
    # containers while it migrates and seeds. Running it as a child keeps the
    # EXIT trap, and with it the lock, until the boot is done.
    INFRA_READINESS_TIMEOUT=300 ./zero --agent
    exit $?
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
