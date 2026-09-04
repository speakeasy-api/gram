#!/usr/bin/env bash

#MISE description="Pause or resume this worktree's recurring Temporal schedules"
#MISE dir="{{ config_root }}"
#MISE hide=true

#USAGE flag "--state <state>" required=#true help="Desired schedule state" { choices "pause" "unpause" }

set -euo pipefail

# Temporal is shared by every worktree, but schedules are scoped to this
# worktree's namespace. A stopped worker does not stop its schedules: the
# Temporal server keeps firing them and accumulating workflow executions. Keep
# track of exactly the schedules paused here so a developer's manually-paused
# schedule stays paused across a stack wake.
gitdir="$(git rev-parse --absolute-git-dir)"
state_file="$gitdir/gram-stack-paused-schedules"
reason="worktree stack paused"
shared_compose=(docker compose -f compose.shared.yml -p gram-shared)

case "$usage_state" in
    pause)
        # An already-paused stack owns the existing list. Do not replace it:
        # wake needs the original set to distinguish lifecycle pauses from
        # schedules that were paused manually.
        [ -f "$state_file" ] && exit 0

        if ! schedules="$("${shared_compose[@]}" exec -T gram-temporal temporal schedule list \
            --namespace "$TEMPORAL_NAMESPACE" \
            --output json)"; then
            echo "Could not list Temporal schedules in $TEMPORAL_NAMESPACE." >&2
            exit 1
        fi

        pending="$state_file.$$"
        : > "$pending"
        trap 'rm -f "$pending"' EXIT
        failed=false

        while IFS= read -r schedule_id; do
            [ -n "$schedule_id" ] || continue
            if "${shared_compose[@]}" exec -T gram-temporal temporal schedule toggle \
                --namespace "$TEMPORAL_NAMESPACE" \
                --schedule-id "$schedule_id" \
                --pause \
                --reason "$reason" > /dev/null; then
                printf '%s\n' "$schedule_id" >> "$pending"
            else
                echo "Could not pause Temporal schedule $schedule_id." >&2
                failed=true
            fi
        done < <(jq -r '.[] | select(.info.paused != true) | .scheduleId' <<< "$schedules")

        mv "$pending" "$state_file"
        trap - EXIT
        [ "$failed" = false ]
        ;;

    unpause)
        [ -f "$state_file" ] || exit 0
        failed=false

        while IFS= read -r schedule_id; do
            [ -n "$schedule_id" ] || continue
            if ! "${shared_compose[@]}" exec -T gram-temporal temporal schedule toggle \
                --namespace "$TEMPORAL_NAMESPACE" \
                --schedule-id "$schedule_id" \
                --unpause \
                --reason "worktree stack resumed" > /dev/null; then
                echo "Could not unpause Temporal schedule $schedule_id." >&2
                failed=true
            fi
        done < "$state_file"

        if [ "$failed" = false ]; then
            rm -f "$state_file"
        fi
        [ "$failed" = false ]
        ;;
esac
