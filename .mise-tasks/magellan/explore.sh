#!/usr/bin/env bash

#MISE description="Magellan: let a Claude Code agent explore the dashboard and record bugs it can prove"
#MISE dir="{{ config_root }}"
#USAGE flag "--since <since>" default="24 hours ago" help="Focus on dashboard changes merged to main since this git date"
#USAGE flag "--budget <minutes>" default="45" help="Rough time budget for the agent"

# Two targets:
#
# - Local (default): this worktree's stack, logged in through dev-idp.
# - Remote: set MAGELLAN_SITE_URL, MAGELLAN_ORG and MAGELLAN_SESSION (a
#   `gram_session` token for a dedicated bot user, captured from a real login).
#   No login can be scripted against WorkOS, so the session is injected into
#   the browser instead. See e2e/magellan/README.md.

set -euo pipefail

run_dir="e2e/.magellan/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run_dir"
touch "$run_dir/findings.jsonl"

# Commands the agent must not run: they read or replace the bot's login, or
# run arbitrary code in the page, any of which could leak the session.
denied=()

if [ -n "${MAGELLAN_SITE_URL:-}" ]; then
    : "${MAGELLAN_ORG:?MAGELLAN_ORG is required with MAGELLAN_SITE_URL}"
    : "${MAGELLAN_SESSION:?MAGELLAN_SESSION is required with MAGELLAN_SITE_URL}"
    site_url="${MAGELLAN_SITE_URL%/}"
    case "$site_url" in
        https://*) ;;
        *)
            echo "MAGELLAN_SITE_URL must use https://" >&2
            exit 1
            ;;
    esac
    org="$MAGELLAN_ORG"
    pw="playwright-cli -s=magellan"
    allowed_pw="Bash(playwright-cli -s=magellan:*)"
    for cmd in state-save state-load cookie-list cookie-get cookie-set cookie-delete cookie-clear run-code; do
        denied+=(--disallowedTools "Bash(playwright-cli -s=magellan ${cmd}:*)")
    done

    # Outside the run directory, which the agent can read: this file is a
    # login. It exists only until the browser has loaded it.
    state_file="$(mktemp -t magellan-state.XXXXXX)"
    trap 'rm -f "$state_file"' EXIT
    # The token goes through the environment, not argv, so `ps` never shows it.
    MAGELLAN_STATE_FILE="$state_file" node -e '
      const { MAGELLAN_SITE_URL: site, MAGELLAN_SESSION: token, MAGELLAN_STATE_FILE: file } = process.env;
      const cookie = { name: "gram_session", value: token, domain: new URL(site).hostname, path: "/",
        expires: Math.floor(Date.now() / 1000) + 86400, httpOnly: true, secure: true, sameSite: "Lax" };
      require("node:fs").writeFileSync(file, JSON.stringify({ cookies: [cookie], origins: [] }));
    '
    # Log the browser in before the agent starts, so neither the token nor the
    # state file is ever in its reach.
    $pw open >/dev/null
    $pw state-load "$state_file" >/dev/null
    rm -f "$state_file"
    unset MAGELLAN_SESSION
    login="The browser session \`magellan\` is already open and logged in as a bot user; go to \`$site_url/$org\`. Never log out: it ends the bot's session for good."
else
    mise run ensure-stack
    site_url="$GRAM_SITE_URL"
    org="${MAGELLAN_ORG:-speakeasy}"
    pw="mise run playwright -s=magellan"
    allowed_pw="Bash(mise run playwright:*)"
    login="Log in with the \"Log in\" button."
fi

# What changed recently is where new bugs are most likely. The agent maps
# these files to routes itself. File paths only: commit subjects are free text
# that would reach the agent as instructions.
git fetch --quiet origin main || echo "warning: could not fetch origin/main; focus may be stale" >&2
# Tests and the generated SDK change no page a user sees.
# A failing git log stops the run here instead of passing for a quiet day.
changed="$(git log origin/main --since="${usage_since:?}" --format= --name-only -- \
    client/dashboard/src ':!client/dashboard/src/sdk' ':!*.test.ts' ':!*.test.tsx')"
changed="$(printf '%s\n' "$changed" | grep -v '^$' | sort -u || true)"
{
    echo "# Dashboard files changed on main since ${usage_since}"
    echo
    echo "${changed:-(none — roam instead)}"
} >"$run_dir/focus.md"

# `#` delimits sed below, so none of the substituted values may contain one.
sed -e "s#{{RUN_DIR}}#${run_dir}#g" \
    -e "s#{{BUDGET}}#${usage_budget:?}#g" \
    -e "s#{{SITE_URL}}#${site_url}#g" \
    -e "s#{{ORG}}#${org}#g" \
    -e "s#{{PW}}#${pw}#g" \
    -e "s#{{LOGIN}}#${login}#g" \
    e2e/magellan/EXPLORE.md |
    claude -p \
        --allowedTools "Read,Glob,Grep,Edit(./${run_dir}/**),Write(./${run_dir}/**),${allowed_pw},Bash(git log:*),Bash(git diff:*),Bash(date:*)" \
        ${denied[@]+"${denied[@]}"} \
        --max-turns 200 \
        >"$run_dir/transcript.txt"

echo "$run_dir"
wc -l <"$run_dir/findings.jsonl" | xargs echo "findings:"
