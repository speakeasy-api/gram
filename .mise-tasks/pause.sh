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
# against stopped databases, or a paused marker over a running stack. mkdir is
# the portable atomic test-and-set (macOS has no flock binary).
lock="$gitdir/gram-stack-lock"
for _ in $(seq 1 60); do
    if mkdir "$lock" 2> /dev/null; then
        trap 'rmdir "$lock" 2> /dev/null || true' EXIT
        break
    fi
    # A lock left behind by a killed pause or wake would otherwise block this
    # worktree forever, and the operation it guards is bounded by the wake's
    # own runtime.
    if [ -n "$(find "$lock" -maxdepth 0 -mmin +10 2> /dev/null)" ]; then
        echo "Clearing a stale stack lock." >&2
        rmdir "$lock" 2> /dev/null || true
    fi
    sleep 1
done

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
    rm -f "$gitdir/gram-stack-parked.pid"

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
