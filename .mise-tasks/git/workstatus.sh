#!/usr/bin/env bash

#MISE description="Show each worktree's stack state and dashboard URL"
#MISE dir="{{ config_root }}"
#MISE alias="gwst"
#MISE quiet=true

set -e

if ! command -v wt &> /dev/null; then
    echo "worktrunk (wt) not found. See https://worktrunk.dev" >&2
    exit 1
fi

# `wt list` renders the URL as an OSC-8 hyperlink showing only `:port`, and its
# up/down signal is a dimmed cell that no column template can read -- liveness
# lives solely in `url_active` in the JSON. So render it here instead.
#
# json-schema is pinned per-invocation because a future release switches the
# default to schema 2, which wraps the array in an envelope this jq would miss.
rows=$(wt --config-set 'list.json-schema=1' list --format json \
    | jq -r '.[] | select(.url // "" != "")
             | [(if .is_current then "@" else "+" end), .branch, (.url_active | tostring), .url, .path]
             | @tsv')

if [ -z "$rows" ]; then
    echo "No worktree URLs yet — ports are assigned by \`mise run git:workinit\`."
    exit 0
fi

# A worktree whose stack is still coming up, and one whose boot died, both look
# exactly like one that was never booted -- the port isn't listening in any of
# the three cases, and worktrunk tracks no hook liveness. `git:workboot` (the
# post-start hook) closes that gap with two markers in the worktree's git dir:
# its pid while the boot runs, and the exit code if the boot failed. Echoes
# `booting`, `failed`, or nothing.
boot_state() {
    local gitdir pid
    gitdir="$(git -C "$1" rev-parse --absolute-git-dir 2>/dev/null)"

    if [ -f "$gitdir/gram-stack-boot.pid" ]; then
        pid=$(cat "$gitdir/gram-stack-boot.pid")
        # Guards against a pid recycled after a hard kill, which would
        # otherwise leave the worktree reading as booting forever.
        if ps -o command= -p "$pid" 2>/dev/null | grep -q workboot; then
            echo booting
            return
        fi
    fi

    # Not `[ -f ... ] && echo`: under `set -e` a false test as the last command
    # would fail the whole script through the caller's assignment.
    if [ -f "$gitdir/gram-stack-boot.failed" ]; then
        echo failed
    fi
}

width=$(printf '%s\n' "$rows" | cut -f2 | awk '{ if (length($0) > m) m = length($0) } END { print m }')

printf '  \033[1m%-*s  %-9s %s\033[0m\n' "$width" "Branch" "State" "URL"

failures=0

# Here-string, not a pipe: a piped `while` runs in a subshell and the failure
# count wouldn't survive it.
while IFS=$'\t' read -r marker branch active url path; do
    # A listening port is the weakest signal here, so the markers outrank it.
    # `zero` runs `mise run start` before `mise run seed`, so the dashboard
    # answers while the org is still on the unseeded demo gate: port open plus a
    # live boot means seeding, and port open plus a failed boot usually means
    # the daemons came up but seeding never ran -- reporting either as `up`
    # sends you to a dashboard that looks broken. Clear a `failed` with another
    # `mise run git:workboot`.
    case "$(boot_state "$path"),$active" in
        booting,true) state=$'\033[33m◐ seeding\033[0m' ;;
        booting,*) state=$'\033[33m◐ booting\033[0m' ;;
        failed,*) state=$'\033[31m✗ failed\033[0m ' ; failures=$((failures + 1)) ;;
        *,true) state=$'\033[32m● up\033[0m     ' ;;
        *) state=$'\033[31m○ down\033[0m   ' ;;
    esac
    printf '%s %-*s  %b %s\n' "$marker" "$width" "$branch" "$state" "$url"
done <<< "$rows"

if [ "$failures" -gt 0 ]; then
    echo
    echo "✗ boot exited nonzero — the stack is incomplete (a failure before \`mise run seed\`"
    echo "  leaves the org on the demo gate). Logs: \`wt config state logs get\`."
    echo "  Retry from that worktree: \`mise run git:workboot\`."
fi
