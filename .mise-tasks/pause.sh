#!/usr/bin/env bash

#MISE description="Stop this worktree's daemons and pause its containers, keeping their data"
#MISE dir="{{ config_root }}"
#MISE alias="sleep"

set -e

# The counterpart of `mise run wake`. Unlike `infra:stop`, this does NOT `down`
# the compose project: containers and volumes stay, so waking again is a
# `docker compose start` rather than a re-create plus migrations plus seed.
# The shared stack (compose.shared.yml) is left alone -- other worktrees use it.
# Recurring Temporal schedules are namespace-scoped, so they can be paused for
# this worktree without disrupting another worktree's worker.

gitdir="$(git rev-parse --absolute-git-dir)"

# Pause and wake move the same containers in opposite directions, and both can
# start without the developer: the idle sweep pauses, the parker's Resume
# button wakes. Interleaved, they leave the worktree half-up -- daemons running
# against stopped databases, or a paused marker over a running stack. A symlink
# is the portable atomic test-and-set here (macOS has no flock binary).
lock="$gitdir/gram-stack-lock"
lock_identity="$$:pause"

# `ln -s` is the atomic test-and-set: creating a symlink fails if the name
# exists, and its target carries the owner's pid and intended schedule state,
# so both appear in one step. (A lock file written after a `mkdir` has a window
# where it exists with no owner recorded, which another process reads as
# abandoned.) The target never has to resolve.
release_lock() {
    # Only release a lock this process still owns.
    if [ "$(readlink "$lock" 2> /dev/null)" = "$lock_identity" ]; then
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
    local reap="$lock.reap" identity owner
    ln -s "$$" "$reap" 2> /dev/null || return 1
    identity="$(readlink "$lock" 2> /dev/null || true)"
    owner="${identity%%:*}"
    if [ -n "$owner" ] && ! kill -0 "$owner" 2> /dev/null; then
        echo "Clearing a stack lock left behind by a dead process ($owner)." >&2
        rm -f "$lock"
    fi
    rm -f "$reap"
}

locked=false
for _ in $(seq 1 60); do
    if ln -s "$lock_identity" "$lock" 2> /dev/null; then
        trap release_lock EXIT
        locked=true
        break
    fi
    reap_stale_lock || true
    sleep 1
done

if [ "$locked" != true ]; then
    identity="$(readlink "$lock" 2> /dev/null || true)"
    owner="${identity%%:*}"
    echo "Another pause or wake (pid ${owner:-unknown}) has held this worktree's stack lock for a minute; giving up." >&2
    echo "If nothing is running, remove $lock and retry." >&2
    exit 1
fi

# Stopping a Gram worker does not stop Temporal schedules: the shared server
# would keep firing them, accumulating executions and CPU work while the stack
# is asleep. Pause them before stopping the worker, and remember only the ones
# changed here so wake does not override a developer's manual pause.
if ! mise run temporal:schedules --state pause --lock-owner "$$"; then
    echo "⚠️  Some Temporal schedules could not be paused; the stack will still be stopped." >&2
fi

# Missing/stopped daemons are successful no-ops in Pitchfork. Real stop errors
# must surface rather than marking a still-running application stack paused.
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --group application
    pitchfork stop idle-pause
fi

docker compose --profile "*" stop

# Marker for `git:workstatus` / `git:worktui`: a paused stack has no boot
# marker, which otherwise reads as `down` -- i.e. as a worktree that never
# booted and needs a full `./zero --agent`. It also outranks port liveness,
# since the parker below deliberately keeps the site port listening. `wake`
# removes it.
: > "$gitdir/gram-stack-paused"

# The idle sweep's activity clock. Left behind, it is a timestamp from before
# this pause, so the next wake would be measured against it and could be
# paused again on the sweep's first quiet sample.
rm -f "$gitdir/gram-stack-lastseen"

# Pitchfork owns the listener, so normal worktree teardown stops it too.
if pitchfork supervisor start && pitchfork start park; then
    echo "Stack paused. Resume with \`mise run wake\`, or from ${GRAM_SITE_URL:-the dashboard URL}."
else
    echo "Stack paused, but the resume page could not start. Use \`mise run wake\` to resume." >&2
    echo "Inspect \`pitchfork logs park\` for the startup failure." >&2
fi
