#!/usr/bin/env bash

#MISE description="Magellan: let a Claude Code agent explore the dashboard and record bugs it can prove"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]
#USAGE flag "--since <since>" default="24 hours ago" help="Focus on dashboard changes merged to main since this git date"
#USAGE flag "--budget <minutes>" default="45" help="Rough time budget for the agent"

set -euo pipefail

run_dir="e2e/.magellan/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$run_dir"
touch "$run_dir/findings.jsonl"

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

sed -e "s#{{RUN_DIR}}#${run_dir}#g" -e "s#{{BUDGET}}#${usage_budget:?}#g" e2e/magellan/EXPLORE.md |
    claude -p \
        --permission-mode acceptEdits \
        --allowedTools "Read,Write,Edit,Glob,Grep,Bash(mise run playwright:*),Bash(git log:*),Bash(git diff:*),Bash(date:*)" \
        --max-turns 200 \
        >"$run_dir/transcript.txt"

echo "$run_dir"
wc -l <"$run_dir/findings.jsonl" | xargs echo "findings:"
