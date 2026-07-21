#!/usr/bin/env bash
#MISE description="Destroy all infra resources"
#MISE dir="{{ config_root }}"

set -e

# Best-effort: stop this worktree's pitchfork daemons and prune stopped
# entries from `pitchfork list` (clean is global across worktrees)
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local || true
    pitchfork clean || true
fi

docker compose --profile "*" down --volumes --remove-orphans

# dev-idp's SQLite database lives outside docker -- nuke it too so a
# follow-up `./zero` boots from a clean mock-workos/oauth2 state.
rm -rf local/devidp

echo ""
echo "💥 All infra resources destroyed"
echo "💥 Run \`./zero\` to get back up and running"