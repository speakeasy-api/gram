#!/usr/bin/env bash

#MISE description="Magellan: let a Claude Code agent explore your local dashboard and record bugs it can prove"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]
#USAGE flag "--since <since>" default="24 hours ago" help="Focus on dashboard changes merged to main since this git date"
#USAGE flag "--budget <minutes>" default="45" help="Rough time budget for the agent"

# Local only: this worktree's stack, logged in through dev-idp, with no
# sandbox (the agent runs as you). For trying out prompt changes. The nightly
# production run is a GitHub Agentic Workflow in speakeasy-api/gram-infra that
# runs the same prompt (.github/prompts/magellan-explore.md) in the AWF sandbox.

set -euo pipefail

budget="${usage_budget:?}"
case "$budget" in '' | *[!0-9]*)
    echo "--budget must be a number of minutes" >&2
    exit 1
    ;;
esac

run_id="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="e2e/.magellan/${run_id}"
mkdir -p "$run_dir/out"
touch "$run_dir/out/findings.jsonl"
session="magellan-${run_id}"
org="${MAGELLAN_ORG:-speakeasy}"

# File paths only: commit subjects are free text.
git fetch --quiet origin main || echo "warning: could not fetch origin/main; focus may be stale" >&2
changed="$(git log origin/main --since="${usage_since:?}" --format= --name-only -- \
    client/dashboard/src ':!client/dashboard/src/sdk' ':!*.test.ts' ':!*.test.tsx')"
changed="$(printf '%s\n' "$changed" | grep -v '^$' | sort -u || true)"

mise run playwright -s="$session" open "$GRAM_SITE_URL" >/dev/null
trap 'mise run playwright -s="$session" close >/dev/null 2>&1 || true' EXIT

{
    cat <<EOF
# Explore the local dashboard

- Run id: ${run_id}. Time budget: ${budget} minutes.
- Site: ${GRAM_SITE_URL}. Org: ${org}. The browser is open on the dashboard;
  log in with the "Log in" button.
- Browser command: \`mise run playwright -s=${session} <command>\`.
- Save screenshots with \`--filename=${run_dir}/out/<name>.png\`.
- Record each finding as one JSON object per line, appended to
  \`${run_dir}/out/findings.jsonl\`, with the fields listed below.

Focus list (dashboard files changed on main since ${usage_since}):

${changed:-(none — roam instead)}

EOF
    cat .github/prompts/magellan-explore.md
} | claude -p \
    --tools "Bash,Read,Write,Edit,Glob,Grep" \
    --allowedTools "Read,Glob,Grep,Write(./${run_dir}/out/**),Edit(./${run_dir}/out/**),Bash(mise run playwright -s=${session}:*)" \
    --max-turns 200 >"$run_dir/transcript.txt" 2>&1 ||
    echo "the agent exited with an error; see $run_dir/transcript.txt" >&2

echo "$run_dir"
wc -l <"$run_dir/out/findings.jsonl" | xargs echo "findings:"
