#!/usr/bin/env bash

#MISE description="Stop this worktree's daemons and pause its containers, keeping their data"
#MISE dir="{{ config_root }}"
#MISE alias="sleep"

set -e

# The counterpart of `mise run wake`. Unlike `infra:stop`, this does NOT `down`
# the compose project: containers and volumes stay, so waking again is a
# `docker compose start` rather than a re-create plus migrations plus seed.
# The shared stack (compose.shared.yml) is left alone -- other worktrees use it.

gitdir="$(git rev-parse --absolute-git-dir)"

# Pause and wake move the same containers in opposite directions, and both can
# start without the developer: the idle sweep pauses, the parker's Resume
# button wakes. Interleaved, they leave the worktree half-up -- daemons running
# against stopped databases, or a paused marker over a running stack. A symlink
# is the portable atomic test-and-set here (macOS has no flock binary).
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

# Best-effort, as in infra:stop: a supervisor that is not running is not an
# error here, and neither is a daemon that has already exited.
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local || true
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

# Park on the site port in the dashboard's place, so the worktree's URL answers
# with a page that says the stack is paused and offers a Resume button, instead
# of a connection error. The parker has to outlive this script: `pause` is
# often run from a shell that goes away, and from `git:workboot`, whose whole
# process GROUP wt signals when the hook finishes.
if [ -z "${GRAM_NO_PARK:-}" ]; then
    # Pausing an already-paused worktree finds a parker still holding the port.
    # Dropping its pid file would orphan it: the replacement dies on
    # EADDRINUSE, and the next wake has no pid left to kill, so vite cannot
    # bind either. The parker that is up is as good as a new one.
    parked="$gitdir/gram-stack-parked.pid"
    park_pid="$(cat "$parked" 2> /dev/null || true)"
    if [ -n "$park_pid" ] && ps -o command= -p "$park_pid" 2> /dev/null | grep -q "park"; then
        echo "Stack paused. Resume with \`mise run wake\`, or from ${GRAM_SITE_URL:-the dashboard URL}."
        exit 0
    fi
    rm -f "$parked"

    # A new session is what actually detaches it -- `nohup` only ignores
    # SIGHUP and leaves the process in this group, so wt's reap would still
    # take it down. setsid where it exists (Linux), perl's POSIX::setsid
    # otherwise (macOS ships perl but not setsid), nohup as the last resort.
    if command -v setsid > /dev/null 2>&1; then
        GRAM_PARK_GIT_DIR="$gitdir" setsid mise run park \
            > "$gitdir/gram-stack-park.log" 2>&1 &
    elif command -v perl > /dev/null 2>&1; then
        GRAM_PARK_GIT_DIR="$gitdir" perl -MPOSIX -e 'setsid; exec @ARGV' \
            mise run park > "$gitdir/gram-stack-park.log" 2>&1 &
    else
        GRAM_PARK_GIT_DIR="$gitdir" nohup mise run park \
            > "$gitdir/gram-stack-park.log" 2>&1 &
    fi
    disown 2> /dev/null || true

    # The parker writes its pid file once it is actually listening, so this
    # also catches the failure that matters: another process already holds the
    # site port. Reported rather than fatal -- the stack IS paused either way,
    # and the only thing lost is the resume page.
    for _ in $(seq 1 20); do
        [ -f "$gitdir/gram-stack-parked.pid" ] && break
        sleep 0.5
    done
    if [ ! -f "$gitdir/gram-stack-parked.pid" ]; then
        echo "⚠️  The resume page did not come up; wake with \`mise run wake\`." >&2
        echo "    Log: $gitdir/gram-stack-park.log" >&2
    fi
fi

echo "Stack paused. Resume with \`mise run wake\`, or from ${GRAM_SITE_URL:-the dashboard URL}."
