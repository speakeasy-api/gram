#!/usr/bin/env bash

#MISE description="Have a Claude Code agent re-walk a flow story, then heal its script or report the flow broken"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]
#USAGE arg "<flow>" help="Directory name under e2e/flows/, e.g. catalog-connect"

# Runs on your machine, against this worktree's stack, with no sandbox: the
# agent runs as you. CI runs the same prompt (.github/prompts/heal-flow.md)
# inside GitHub Agentic Workflows' AWF sandbox (.github/workflows/flow-checks.md).

set -euo pipefail

flow="${usage_flow:?}"
if [[ ! "$flow" =~ ^[a-z0-9-]+$ ]] || [ ! -f "e2e/flows/${flow}/story.md" ]; then
    echo "No story at e2e/flows/${flow}/story.md" >&2
    exit 1
fi

mkdir -p e2e/.flows
verdict_file="e2e/.flows/${flow}-verdict.json"

{
    cat <<EOF
# Heal the flow check \`${flow}\`

The local stack is running. The dashboard is at ${GRAM_SITE_URL:?}.

Commands:

- Browser: \`mise run playwright -s=${flow} <command>\`.
- Replay the flow: \`mise run test:flows flows/${flow}/\`.
- Type-check: \`aube run -F @gram/e2e type-check\`.
- Branch diff: \`git diff origin/main...HEAD -- client/ e2e/flows/\`.

EOF
    cat .github/prompts/heal-flow.md
    cat <<EOF

## Report

Finish with exactly one line of JSON and nothing after it:

{"flow":"${flow}","verdict":"healed|broken|changed","step":"<story step that failed or moved, or null>","summary":"<one sentence>"}
EOF
} | claude -p \
    --tools "Bash,Read,Write,Edit,Glob,Grep" \
    --allowedTools "Read,Glob,Grep,Edit(./e2e/flows/${flow}/flow.spec.ts),Write(./e2e/flows/${flow}/flow.spec.ts),Write(./e2e/.flows/**),Bash(mise run playwright:*),Bash(mise run test:flows:*),Bash(git diff:*),Bash(aube run -F @gram/e2e type-check)" \
    --max-turns 80 |
    tee "e2e/.flows/${flow}-transcript.txt" |
    tail -n 1 >"$verdict_file"

cat "$verdict_file"
