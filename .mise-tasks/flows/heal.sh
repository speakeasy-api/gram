#!/usr/bin/env bash

#MISE description="Have a Claude Code agent re-walk a flow story, then heal its script or report the flow broken"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]
#USAGE arg "<flow>" help="Directory name under e2e/flows/, e.g. catalog-connect"

set -euo pipefail

flow="${usage_flow:?}"
if [ ! -f "e2e/flows/${flow}/story.md" ]; then
    echo "No story at e2e/flows/${flow}/story.md" >&2
    exit 1
fi

mkdir -p e2e/.flows
verdict_file="e2e/.flows/${flow}-verdict.json"

# The agent's last line is the verdict JSON (see e2e/flows/HEAL.md). Keep the
# full transcript for whoever reads the CI artifacts.
sed "s/{{FLOW}}/${flow}/g" e2e/flows/HEAL.md |
    claude -p \
        --permission-mode acceptEdits \
        --allowedTools "Read,Write,Edit,Glob,Grep,Bash(mise run playwright:*),Bash(mise run test:flows:*),Bash(git diff:*),Bash(git log:*),Bash(aube run -F @gram/e2e type-check)" \
        --max-turns 80 |
    tee "e2e/.flows/${flow}-transcript.txt" |
    tail -n 1 >"$verdict_file"

cat "$verdict_file"
