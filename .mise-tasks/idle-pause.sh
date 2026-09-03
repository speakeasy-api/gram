#!/usr/bin/env bash

#MISE description="Pause this worktree's stack if nothing has used it for a while"
#MISE dir="{{ config_root }}"

#USAGE flag "--minutes <minutes>" default="60" help="Idle time before pausing"
#USAGE flag "--all" help="Check every worktree instead of this one"
#USAGE flag "--dry-run" help="Report what would be paused without pausing"

set -e

# The complement to `mise run wake`: waking is cheap and now happens on its own
# (opening the dashboard, or any task that depends on `ensure-stack`), so
# stacks accumulate as you move between worktrees. This pauses the ones nobody
# is using.
#
# Activity signal: established TCP connections to the site and server ports. An
# open dashboard tab holds a vite HMR websocket, an agent driving the API holds
# a connection for the duration of its request, and `pitchfork` itself connects
# to neither -- so "no connections" really is "nobody is using this". A single
# quiet sample is not enough (nothing is connected between two requests), so
# the last-seen time is kept in the worktree's git dir and only a stretch of
# quiet longer than --minutes pauses the stack.
#
# Scheduled by pitchfork, which is already running the stack it watches -- see
# the `idle-pause` cron daemon in pitchfork.toml. Deliberately not a launchd
# agent or a systemd timer: those install a job on the developer's machine that
# outlives the repo, and we have a hard requirement not to touch their system.

minutes="${usage_minutes:-60}"
check_all="${usage_all:-false}"
dry_run="${usage_dry_run:-false}"

if [ "$check_all" = "true" ]; then
    # Each worktree has its own ports and its own git dir, and mise resolves
    # both from the directory -- so recurse per worktree rather than trying to
    # read another worktree's environment from here.
    forward=(--minutes "$minutes")
    [ "$dry_run" = "true" ] && forward+=(--dry-run)

    while read -r wt; do
        [ -d "$wt" ] || continue
        # Worktrees on a branch that predates this task would just print "no
        # task idle-pause found" on every run -- skip those quietly. Any other
        # discovery failure (a broken mise config, a missing toolchain) is worth
        # hearing about.
        if ! probe="$(cd "$wt" && mise tasks info idle-pause 2>&1)"; then
            case "$probe" in
                *"Task not found"* | *"no task"*) continue ;;
                *) echo "⚠️  cannot run idle-pause in $wt: $probe" >&2; continue ;;
            esac
        fi
        (cd "$wt" && mise run idle-pause "${forward[@]}") \
            || echo "⚠️  idle-pause failed in $wt" >&2
    done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
    exit 0
fi

gitdir="$(git rev-parse --absolute-git-dir)"
# `git branch --show-current` succeeds with empty output on a detached HEAD,
# so `||` never fires.
branch="$(git branch --show-current 2> /dev/null)"
[ -n "$branch" ] || branch="detached"

# Already paused, or a boot is in flight: nothing to do either way.
[ -f "$gitdir/gram-stack-paused" ] && exit 0
if [ -f "$gitdir/gram-stack-boot.pid" ]; then
    pid=$(cat "$gitdir/gram-stack-boot.pid")
    ps -o command= -p "$pid" 2> /dev/null | grep -q workboot && exit 0
fi

# `lsof` on macOS and every dev container that has it; `ss` on the Linux hosts
# that do not (a missing tool would otherwise read as "no connections" and
# pause a stack somebody is using).
if command -v lsof > /dev/null 2>&1; then
    listening() {
        lsof -nP -iTCP:"$1" -sTCP:LISTEN -t > /dev/null 2>&1
    }
    established() {
        # `grep -c` last in a pipeline would mask a failing probe as zero
        # connections, which is exactly the answer that pauses a stack.
        local out
        out="$(lsof -nP -iTCP:"$1" -iTCP:"$2" -sTCP:ESTABLISHED -t 2> /dev/null)" \
            || [ -z "$out" ] || return 1
        printf '%s' "$out" | grep -c . | tr -d ' '
    }
elif command -v ss > /dev/null 2>&1; then
    listening() {
        ss -Hltn "sport = :$1" 2> /dev/null | grep -q .
    }
    established() {
        local out
        out="$(ss -Htn "state established ( sport = :$1 or sport = :$2 )" 2> /dev/null)" \
            || return 1
        printf '%s' "$out" | grep -c . | tr -d ' '
    }
else
    echo "Neither lsof nor ss is available; cannot tell whether this stack is in use." >&2
    exit 0
fi

# Nothing listening on the site port means the stack is down (not paused --
# a nuked or never-booted worktree); leave it alone.
if ! listening "${GRAM_SITE_PORT}"; then
    exit 0
fi

if ! conns=$(established "${GRAM_SITE_PORT}" "${GRAM_SERVER_PORT}"); then
    echo "Could not inspect connections for ${GRAM_SITE_PORT}/${GRAM_SERVER_PORT}; leaving the stack alone." >&2
    exit 0
fi

stamp="$gitdir/gram-stack-lastseen"
now=$(date +%s)

if [ "$conns" -gt 0 ]; then
    echo "$now" > "$stamp"
    exit 0
fi

# First quiet sample after a wake has no stamp to compare against. Start the
# clock now rather than pausing a stack that came up seconds ago.
if [ ! -f "$stamp" ]; then
    echo "$now" > "$stamp"
    exit 0
fi

idle=$(( (now - $(cat "$stamp")) / 60 ))
if [ "$idle" -lt "$minutes" ]; then
    exit 0
fi

if [ "$dry_run" = "true" ]; then
    echo "${branch}: idle ${idle}m — would pause"
    exit 0
fi

# The sample that decided this is up to five minutes old. Cheap insurance
# against pausing a stack somebody started using in between -- and a probe that
# fails here counts as "in use" for the same reason as above.
if ! recheck=$(established "${GRAM_SITE_PORT}" "${GRAM_SERVER_PORT}"); then
    exit 0
fi
if [ "$recheck" -gt 0 ]; then
    echo "$now" > "$stamp"
    exit 0
fi

echo "${branch}: idle ${idle}m — pausing"
rm -f "$stamp"

# `mise run pause` stops every local daemon -- including the pitchfork cron
# daemon this task is running under when the schedule fired it. Run it in its
# own session so pausing cannot kill the pause halfway through, before the
# containers are stopped. Output goes to the worktree's git dir, since the
# daemon's own log dies with it.
if command -v setsid > /dev/null 2>&1; then
    setsid mise run pause > "$gitdir/gram-stack-idle-pause.log" 2>&1 &
else
    nohup mise run pause > "$gitdir/gram-stack-idle-pause.log" 2>&1 &
fi
disown 2> /dev/null || true
