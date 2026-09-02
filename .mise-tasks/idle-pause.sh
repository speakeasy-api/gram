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
# Run it from cron or a launchd agent every few minutes:
#   */5 * * * * cd /path/to/gram && mise run idle-pause --all --minutes 60

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
        # Worktrees on a branch that predates this task would just print
        # "no task idle-pause found" on every cron run -- skip them quietly.
        (cd "$wt" && mise tasks info idle-pause &> /dev/null \
            && mise run idle-pause "${forward[@]}") || true
    done < <(git worktree list --porcelain | sed -n 's/^worktree //p')
    exit 0
fi

gitdir="$(git rev-parse --absolute-git-dir)"
branch="$(git branch --show-current 2> /dev/null || echo detached)"

# Already paused, or a boot is in flight: nothing to do either way.
[ -f "$gitdir/gram-stack-paused" ] && exit 0
if [ -f "$gitdir/gram-stack-boot.pid" ]; then
    pid=$(cat "$gitdir/gram-stack-boot.pid")
    ps -o command= -p "$pid" 2> /dev/null | grep -q workboot && exit 0
fi

# Nothing listening on the site port means the stack is down (not paused --
# a nuked or never-booted worktree); leave it alone.
if ! lsof -nP -iTCP:"${GRAM_SITE_PORT}" -sTCP:LISTEN -t > /dev/null 2>&1; then
    exit 0
fi

conns=$(lsof -nP \
    -iTCP:"${GRAM_SITE_PORT}" -iTCP:"${GRAM_SERVER_PORT}" \
    -sTCP:ESTABLISHED -t 2> /dev/null | wc -l | tr -d ' ')

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

echo "${branch}: idle ${idle}m — pausing"
rm -f "$stamp"
mise run pause
