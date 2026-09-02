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
# against stopped databases, or a paused marker over a running stack.
lockfile="$gitdir/gram-stack.lock"

# Held by the kernel, not by a file we have to clean up: an advisory lock on an
# open descriptor is released when the holder dies, however it dies, so there is
# no stale lock to detect and no takeover protocol to get wrong. Every
# file-based scheme needed one (is the owner alive? may I clear it?) and each
# had a window where two processes both believed they held it.
#
# Re-exec under the lock rather than wrapping the body: the descriptor survives
# exec, so the lock covers this script to its last line.
#
# perl rather than flock(1), which macOS does not ship, and which does not tell
# us which descriptor it used -- and the descriptor number is load-bearing here.
# It is inherited by every child, so a long-lived one (the parker, a pitchfork
# daemon) would go on holding this lock for hours after the script exits. Those
# are started through `without_stack_lock`, which closes it first.
if [ -z "${GRAM_STACK_LOCK_HELD:-}" ]; then
    export GRAM_STACK_LOCK_HELD=1

    if command -v perl > /dev/null 2>&1; then
        perl -e '
            $^F = 255;  # keeps the descriptor off close-on-exec
            open(my $fh, ">>", $ARGV[0]) or die "stack lock: $!\n";
            eval {
                local $SIG{ALRM} = sub { die "timeout\n" };
                alarm 60;
                flock($fh, 2) or die "stack lock: $!\n";
                alarm 0;
            };
            if ($@) {
                print STDERR "Another pause or wake has held this worktree'"'"'s stack lock for a minute; giving up.\n";
                exit 1;
            }
            $ENV{GRAM_STACK_LOCK_FD} = fileno($fh);
            exec(@ARGV[1 .. $#ARGV]) or die "exec: $!\n";
        ' "$lockfile" "$0" "$@"
        exit $?
    fi

    echo "⚠️  perl is not available; running without a stack lock." >&2
fi

# Run something that outlives this script without handing it the lock. The
# parent keeps the descriptor, so the lock still covers the wait.
without_stack_lock() {
    if [ -n "${GRAM_STACK_LOCK_FD:-}" ]; then
        eval "exec ${GRAM_STACK_LOCK_FD}>&-"
    fi
    "$@"
}

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
    (
        export GRAM_PARK_GIT_DIR="$gitdir"
        if command -v setsid > /dev/null 2>&1; then
            without_stack_lock setsid mise run park
        elif command -v perl > /dev/null 2>&1; then
            without_stack_lock perl -MPOSIX -e 'setsid; exec @ARGV' mise run park
        else
            without_stack_lock nohup mise run park
        fi
    ) > "$gitdir/gram-stack-park.log" 2>&1 &
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
