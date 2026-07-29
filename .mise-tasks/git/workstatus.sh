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
             | [(if .is_current then "@" else "+" end), .branch, (.url_active | tostring), .url]
             | @tsv')

if [ -z "$rows" ]; then
    echo "No worktree URLs yet — ports are assigned by \`mise run git:workinit\`."
    exit 0
fi

width=$(printf '%s\n' "$rows" | cut -f2 | awk '{ if (length($0) > m) m = length($0) } END { print m }')

printf '  \033[1m%-*s  %-6s %s\033[0m\n' "$width" "Branch" "State" "URL"

printf '%s\n' "$rows" | while IFS=$'\t' read -r marker branch active url; do
    if [ "$active" = "true" ]; then
        state=$'\033[32m● up\033[0m  '
    else
        state=$'\033[31m○ down\033[0m'
    fi
    printf '%s %-*s  %b %s\n' "$marker" "$width" "$branch" "$state" "$url"
done
