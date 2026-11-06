#!/usr/bin/env bash

#MISE description="Have a Claude Code agent re-walk a flow story, then heal its script or report the flow broken"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]
#USAGE arg "<flow>" help="Directory name under e2e/flows/, e.g. catalog-connect"

# Runs on your machine, against this worktree's stack. CI runs the same prompt
# inside the agent jail instead (e2e/flows/heal-ci.sh).

set -euo pipefail

flow="${usage_flow:?}"
if [[ ! "$flow" =~ ^[a-z0-9-]+$ ]] || [ ! -f "e2e/flows/${flow}/story.md" ]; then
    echo "No story at e2e/flows/${flow}/story.md" >&2
    exit 1
fi

mkdir -p e2e/.flows
verdict_file="e2e/.flows/${flow}-verdict.json"

# shellcheck disable=SC2016 # literal backticks: markdown for the prompt
diff_hint='DIFF=`git diff origin/main...HEAD -- client/ e2e/flows/`'
node e2e/agent-jail/render.mjs e2e/flows/HEAL.md \
    "FLOW=${flow}" \
    "RESULTS=e2e/.flows/results.json" \
    "ARTIFACTS=e2e/.flows/artifacts/" \
    "$diff_hint" \
    "BROWSER=mise run playwright -s=${flow}" \
    "TEST=mise run test:flows flows/${flow}/" \
    "TYPECHECK=aube run -F @gram/e2e type-check" \
    "SITE_URL=${GRAM_SITE_URL:?}" |
    claude -p \
        --tools "Bash,Read,Write,Edit,Glob,Grep" \
        --allowedTools "Read,Glob,Grep,Edit(./e2e/flows/${flow}/**),Write(./e2e/flows/${flow}/**),Write(./e2e/.flows/**),Bash(mise run playwright:*),Bash(mise run test:flows:*),Bash(git diff:*),Bash(aube run -F @gram/e2e type-check)" \
        --max-turns 80 |
    tee "e2e/.flows/${flow}-transcript.txt" |
    tail -n 1 >"$verdict_file"

cat "$verdict_file"
