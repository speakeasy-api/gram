#!/usr/bin/env bash

#MISE description="Magellan: let a Claude Code agent explore the dashboard and record bugs it can prove"
#MISE dir="{{ config_root }}"
#USAGE flag "--since <since>" default="24 hours ago" help="Focus on dashboard changes merged to main since this git date"
#USAGE flag "--budget <minutes>" default="45" help="Rough time budget for the agent"

# Two targets:
#
# - Local (default): this worktree's stack, logged in through dev-idp.
# - Remote: set MAGELLAN_SITE_URL, MAGELLAN_ORG and MAGELLAN_SESSION (a
#   `gram_session` token for a dedicated bot user). Remote runs only inside the
#   agent jail (e2e/agent-jail/README.md), so it needs Linux and sudo; CI is
#   where it runs. The agent never holds the session: the jail loads it into
#   the browser before the agent starts.
#
# Either way the agent works in a workspace that holds copies of what it may
# read (context/) and one directory it may write (out/). Its only browser
# command is magellan-browser (e2e/magellan/browser.mjs).

set -euo pipefail

# The production sites a remote run may target. A session is only ever sent
# to one of these, whatever MAGELLAN_SITE_URL says.
allowed_sites="https://app.getgram.ai"

budget="${usage_budget:?}"
case "$budget" in '' | *[!0-9]*)
    echo "--budget must be a number of minutes" >&2
    exit 1
    ;;
esac

run_id="$(date -u +%Y%m%dT%H%M%SZ)"
ws="e2e/.magellan/${run_id}"
mkdir -p "$ws/context/stories" "$ws/out" "$ws/bin"
touch "$ws/out/findings.jsonl"

remote=""
if [ -n "${MAGELLAN_SITE_URL:-}" ]; then
    remote=1
    : "${MAGELLAN_ORG:?MAGELLAN_ORG is required with MAGELLAN_SITE_URL}"
    : "${MAGELLAN_SESSION:?MAGELLAN_SESSION is required with MAGELLAN_SITE_URL}"
    site_url="${MAGELLAN_SITE_URL%/}"
    ok=""
    for s in $allowed_sites; do [ "$site_url" = "$s" ] && ok=1; done
    if [ -z "$ok" ]; then
        echo "MAGELLAN_SITE_URL must be one of: $allowed_sites" >&2
        exit 1
    fi
    org="$MAGELLAN_ORG"
    login="The browser is already logged in as a bot user, on the org's home page. Never log out: it ends the bot's session for good."
else
    mise run ensure-stack
    site_url="$GRAM_SITE_URL"
    org="${MAGELLAN_ORG:-speakeasy}"
    login="The browser is open on the dashboard. Log in with the \"Log in\" button."
fi
case "$org" in '' | *[!A-Za-z0-9_-]*)
    echo "MAGELLAN_ORG must be a plain slug" >&2
    exit 1
    ;;
esac

# What changed recently is where new bugs are most likely. File paths only:
# commit subjects are free text that would reach the agent as instructions.
git fetch --quiet origin main || echo "warning: could not fetch origin/main; focus may be stale" >&2
# A failing git log stops the run here instead of passing for a quiet day.
changed="$(git log origin/main --since="${usage_since:?}" --format= --name-only -- \
    client/dashboard/src ':!client/dashboard/src/sdk' ':!*.test.ts' ':!*.test.tsx')"
changed="$(printf '%s\n' "$changed" | grep -v '^$' | sort -u || true)"
{
    echo "# Dashboard files changed on main since ${usage_since}"
    echo
    echo "${changed:-(none — roam instead)}"
} >"$ws/context/focus.md"

# Copies of what the agent may read. The dashboard source maps files to
# routes; the generated SDK, fonts and images add nothing.
tar -C client/dashboard --exclude=./src/sdk --exclude=./src/fonts --exclude=./src/assets \
    -cf - ./src | tar -C "$ws/context" -xf -
mv "$ws/context/src" "$ws/context/dashboard-src"
for story in e2e/flows/*/story.md; do
    [ -f "$story" ] || continue
    cp "$story" "$ws/context/stories/$(basename "$(dirname "$story")").md"
done

node e2e/agent-jail/render.mjs e2e/magellan/EXPLORE.md \
    "RUN_ID=${run_id}" \
    "BUDGET=${budget}" \
    "SITE_URL=${site_url}" \
    "ORG=${org}" \
    "LOGIN=${login}" >"$ws/prompt.md"

# Read and Glob/Grep work inside the workspace only; Write only under out/.
tools="Bash,Read,Write,Edit,Glob,Grep"
allowed="Write(./out/**),Edit(./out/**),Bash(magellan-browser:*)"

if [ -n "$remote" ]; then
    sudo --preserve-env=MAGELLAN_SESSION \
        e2e/magellan/explore-ci.sh "$ws" "$site_url" "$org" "$budget" "$tools" "$allowed"
else
    session="magellan-${run_id}"
    abs_ws="$(cd "$ws" && pwd)"
    cat >"$ws/bin/browser.json" <<JSON
{
  "siteURL": "${site_url}",
  "session": "${session}",
  "outDir": "${abs_ws}/out",
  "backend": ["mise", "run", "playwright"],
  "cwd": "$(pwd)"
}
JSON
    cat >"$ws/bin/magellan-browser" <<SH
#!/usr/bin/env bash
exec node "$(pwd)/e2e/magellan/browser.mjs" "${abs_ws}/bin/browser.json" "\$@"
SH
    chmod +x "$ws/bin/magellan-browser"
    mise run playwright -s="$session" open "$site_url" >/dev/null
    trap 'mise run playwright -s="$session" close >/dev/null 2>&1 || true' EXIT
    (
        cd "$ws"
        PATH="$abs_ws/bin:$PATH" claude -p \
            --tools "$tools" \
            --allowedTools "$allowed" \
            --max-turns 200 <prompt.md >transcript.txt 2>&1
    ) || echo "the agent exited with an error; see $ws/transcript.txt" >&2
fi

echo "$ws"
wc -l <"$ws/out/findings.jsonl" | xargs echo "findings:"
