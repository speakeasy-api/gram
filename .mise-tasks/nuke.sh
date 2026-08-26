#!/usr/bin/env bash
#MISE description="Destroy all infra resources"
#MISE dir="{{ config_root }}"

set -e

# --keep-shared: leave shared containers and volumes running. Worktree removal
# still drops this worktree's ClickHouse database before its local stack.
keep_shared=0
for arg in "$@"; do
    case "$arg" in
        --keep-shared) keep_shared=1 ;;
        *) echo "unknown argument: $arg" >&2; exit 1 ;;
    esac
done

# Best-effort: stop this worktree's pitchfork daemons and prune stopped
# entries from `pitchfork list` (clean is global across worktrees)
if pitchfork supervisor status &> /dev/null; then
    pitchfork stop --all-local || true
    pitchfork clean || true
fi

# Main-tree default is never dropped; worktrees remove only their selected
# validated namespace.
clickhouse_database="${CLICKHOUSE_DATABASE:-default}"
if [[ ! "$clickhouse_database" =~ ^[a-z][a-z0-9_]*$ ]]; then
    echo "invalid CLICKHOUSE_DATABASE '$clickhouse_database'; refusing to use it in DROP DATABASE" >&2
    exit 1
fi
if [ "$clickhouse_database" != "default" ]; then
    drop_cmd=(
        docker compose -f compose.shared.yml -p gram-shared exec -T clickhouse
        clickhouse-client --user gram --password gram --database default
        --query "DROP DATABASE IF EXISTS \`${clickhouse_database}\`"
    )
    if [ "$keep_shared" -eq 1 ]; then
        "${drop_cmd[@]}"
    else
        "${drop_cmd[@]}" 2>/dev/null || true
    fi
fi

docker compose --profile "*" down --volumes --remove-orphans

# Full nuke destroys the fixed shared ClickHouse, Temporal, Pub/Sub, Presidio,
# and LGTM stack. This affects every worktree, hence --keep-shared on removal.
if [ "$keep_shared" -eq 0 ]; then
    docker compose -f compose.shared.yml -p gram-shared down --volumes --remove-orphans
fi

# dev-idp's SQLite database lives outside docker -- nuke it too so a
# follow-up `./zero` boots from a clean mock-workos/oauth2 state.
rm -rf local/devidp

echo ""
echo "💥 All infra resources destroyed"
echo "💥 Run \`./zero\` to get back up and running"