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

if [ -n "${MAGELLAN_SITE_URL:-}" ]; then
    : "${MAGELLAN_ORG:?MAGELLAN_ORG is required with MAGELLAN_SITE_URL}"
    : "${MAGELLAN_SESSION:?MAGELLAN_SESSION is required with MAGELLAN_SITE_URL}"
    site_url="${MAGELLAN_SITE_URL%/}"
    org="$MAGELLAN_ORG"
    pw="playwright-cli -s=magellan"
    allowed_pw="Bash(playwright-cli -s=magellan:*)"

    # Outside the run directory, which gets uploaded: this file is a login.
    state_file="$(mktemp -t magellan-state.XXXXXX)"
    trap 'rm -f "$state_file"' EXIT
    host="$(node -e 'console.log(new URL(process.argv[1]).hostname)' "$site_url")"
    node -e '
      const [host, token, file] = process.argv.slice(1);
      const cookie = { name: "gram_session", value: token, domain: host, path: "/",
        expires: Math.floor(Date.now() / 1000) + 86400, httpOnly: true, secure: true, sameSite: "Lax" };
      require("node:fs").writeFileSync(file, JSON.stringify({ cookies: [cookie], origins: [] }));
    ' "$host" "$MAGELLAN_SESSION" "$state_file"
    login="You are already logged in as a bot user. Start with \`$pw open\`, \`$pw state-load $state_file\`, then \`$pw goto $site_url/$org\`. Never log out: it ends the bot's session for good. Do not read the state file."
else
    mise run ensure-stack
    site_url="$GRAM_SITE_URL"
    org="${MAGELLAN_ORG:-speakeasy}"
    pw="mise run playwright -s=magellan"
    allowed_pw="Bash(mise run playwright:*)"
    login="Log in with the \"Log in\" button."
fi

# What changed recently is where new bugs are most likely. The agent maps
# these files to routes itself.
git fetch --quiet origin main || true
{
    echo "# Dashboard changes on main since ${usage_since:?}"
    echo
    # Tests and the generated SDK change no page a user sees.
    git log origin/main --since="$usage_since" --format='## %h %s' --name-only -- \
        client/dashboard/src ':!client/dashboard/src/sdk' ':!*.test.ts' ':!*.test.tsx' |
        grep -v '^$' || echo "(none — roam instead)"
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
        --permission-mode acceptEdits \
        --allowedTools "Read,Write,Edit,Glob,Grep,${allowed_pw},Bash(git log:*),Bash(git diff:*),Bash(date:*)" \
        --max-turns 200 \
        >"$run_dir/transcript.txt"

echo "$run_dir"
wc -l <"$run_dir/findings.jsonl" | xargs echo "findings:"
