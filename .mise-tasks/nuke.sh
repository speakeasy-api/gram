#!/usr/bin/env bash

#MISE description="Destroy all infra resources"
#MISE dir="{{ config_root }}"

set -e

# --keep-shared leaves compose.shared.yml services running.
# Accept the former flag for older worktree hooks; volume teardown replaces it.
keep_shared=0
for arg in "$@"; do
    case "$arg" in
        --keep-shared) keep_shared=1 ;;
        --delete-namespace) ;;
        *) echo "unknown argument: $arg" >&2; exit 1 ;;
    esac
done

# Stop must finish before deleting workflow state; pruning stopped supervisor
# entries is best-effort (clean is global across worktrees).
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local
    pitchfork clean || true
fi

docker compose --profile "*" down --volumes --remove-orphans

# Shared services live under a fixed project. Nuke means "destroy all infra", so
# tear them down too; `./zero` recreates them. This affects every worktree,
# hence --keep-shared for worktree removal.
if [ "$keep_shared" -eq 0 ]; then
    docker compose -f compose.shared.yml -p gram-shared down --volumes --remove-orphans
fi

# A fresh server must not inherit pause bookkeeping from deleted schedules.
rm -f "$(git rev-parse --absolute-git-dir)/gram-stack-paused-schedules"

# dev-idp's SQLite database lives outside docker -- nuke it too so a
# follow-up `./zero` boots the dev-idp from a clean database.
rm -rf local/devidp

echo ""
echo "💥 All infra resources destroyed"
echo "💥 Run \`./zero\` to get back up and running"
