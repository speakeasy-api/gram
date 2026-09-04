#!/usr/bin/env bash

#MISE description="Destroy all infra resources"
#MISE dir="{{ config_root }}"

set -e

# --keep-shared leaves compose.shared.yml services running.
# --delete-namespace removes this worktree's namespace from the shared Temporal
# server and is reserved for worktree-removal callers.
keep_shared=0
delete_temporal_namespace=0
for arg in "$@"; do
    case "$arg" in
        --keep-shared) keep_shared=1 ;;
        --delete-namespace) delete_temporal_namespace=1 ;;
        *) echo "unknown argument: $arg" >&2; exit 1 ;;
    esac
done

if [ "$delete_temporal_namespace" -eq 1 ]; then
    if [ "$keep_shared" -ne 1 ]; then
        echo "--delete-namespace requires --keep-shared so Temporal remains available." >&2
        exit 1
    fi
    # Worktree namespaces follow this generated prefix. Refuse to delete the
    # primary tree's `default` namespace even when explicitly requested.
    if [[ "$TEMPORAL_NAMESPACE" != gram-infra-* ]]; then
        echo "Refusing to delete non-worktree Temporal namespace $TEMPORAL_NAMESPACE." >&2
        exit 1
    fi
    if ! mise run temporal:schedules --state pause; then
        echo "⚠️  Could not pause every schedule in $TEMPORAL_NAMESPACE before cleanup." >&2
    fi
fi

# Best-effort: stop this worktree's pitchfork daemons and prune stopped
# entries from `pitchfork list` (clean is global across worktrees)
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local || true
    pitchfork clean || true
fi

docker compose --profile "*" down --volumes --remove-orphans

# Shared services live under a fixed project. Nuke means "destroy all infra", so
# tear them down too; `./zero` recreates them. This affects every worktree,
# hence --keep-shared for worktree removal.
if [ "$keep_shared" -eq 0 ]; then
    docker compose -f compose.shared.yml -p gram-shared down --volumes --remove-orphans
fi

# Worktree removal is the end of this namespace's lifecycle. Without deleting
# it, every removed worktree leaves its schedules and a Temporal system worker
# behind forever in the shared SQLite database. Best-effort so an unavailable
# shared server does not make an otherwise-local worktree impossible to remove;
# the schedules were still paused above when Temporal was reachable.
if [ "$delete_temporal_namespace" -eq 1 ]; then
    if docker compose -f compose.shared.yml -p gram-shared exec -T gram-temporal \
        temporal operator namespace delete \
        --namespace "$TEMPORAL_NAMESPACE" \
        --yes > /dev/null; then
        rm -f "$(git rev-parse --absolute-git-dir)/gram-stack-paused-schedules"
    else
        echo "⚠️  Could not delete Temporal namespace $TEMPORAL_NAMESPACE; it may need manual cleanup." >&2
    fi
fi

# dev-idp's SQLite database lives outside docker -- nuke it too so a
# follow-up `./zero` boots from a clean mock-workos/oauth2 state.
rm -rf local/devidp

echo ""
echo "💥 All infra resources destroyed"
echo "💥 Run \`./zero\` to get back up and running"
