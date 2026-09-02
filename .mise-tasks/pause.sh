#!/usr/bin/env bash

#MISE description="Stop this worktree's daemons and pause its containers, keeping their data"
#MISE dir="{{ config_root }}"
#MISE alias="sleep"

set -e

# The counterpart of `mise run wake`. Unlike `infra:stop`, this does NOT `down`
# the compose project: containers and volumes stay, so waking again is a
# `docker compose start` rather than a re-create plus migrations plus seed.
# The shared stack (compose.shared.yml) is left alone -- other worktrees use it.

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
gitdir="$(git rev-parse --absolute-git-dir)"
: > "$gitdir/gram-stack-paused"


# Park on the site port in the dashboard's place, so the worktree's URL answers
# with a page that says the stack is paused and offers a Resume button, instead
# of a connection error. Detached and nohup'd: `pause` is often run from a shell that goes away
# (and from `git:workboot`, whose whole process group is killed when the boot
# hook finishes). The parker exits on its own once it has woken the stack, and
# `mise run wake` kills it first when the wake comes from the CLI instead.
if [ -z "${GRAM_NO_PARK:-}" ]; then
    # `setsid` where it exists (Linux, and macOS with util-linux installed)
    # puts the parker in its own session, so a caller whose whole process
    # GROUP is signalled -- which is how wt reaps the post-start hook -- cannot
    # take it down with them. nohup+& is the portable fallback.
    if command -v setsid > /dev/null 2>&1; then
        GRAM_PARK_GIT_DIR="$gitdir" setsid mise run park \
            > "$gitdir/gram-stack-park.log" 2>&1 &
    else
        GRAM_PARK_GIT_DIR="$gitdir" nohup mise run park \
            > "$gitdir/gram-stack-park.log" 2>&1 &
    fi
    disown 2> /dev/null || true
fi

echo "Stack paused. Resume with \`mise run wake\`, or from ${GRAM_SITE_URL:-the dashboard URL}."
