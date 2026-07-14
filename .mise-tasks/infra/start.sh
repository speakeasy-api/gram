#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

docker compose up -d || exit 1

# Maximum time (seconds) to wait for a service to accept queries before giving
# up. Bounded so headless callers (e.g. `./zero --agent`) fail fast instead of
# hanging forever when infra never becomes healthy. Override with
# INFRA_READINESS_TIMEOUT. Validate as a plain decimal integer before using it
# in arithmetic: unvalidated values would be evaluated by $((...)) (allowing
# command substitution) and leading zeroes would be misread as octal.
READINESS_TIMEOUT="${INFRA_READINESS_TIMEOUT:-30}"
if [[ "$READINESS_TIMEOUT" =~ ^[0-9]+$ ]]; then
    READINESS_TIMEOUT=$((10#$READINESS_TIMEOUT))
else
    echo "⚠️  Ignoring invalid INFRA_READINESS_TIMEOUT='$READINESS_TIMEOUT'; using 30." >&2
    READINESS_TIMEOUT=30
fi

# Resolve a `timeout` binary if available (coreutils `timeout`, or `gtimeout`
# from Homebrew coreutils on macOS). Used to bound each probe so a hung
# `docker compose exec` or Docker Engine call cannot block past the deadline.
TIMEOUT_BIN=""
if command -v timeout > /dev/null 2>&1; then
    TIMEOUT_BIN="timeout"
elif command -v gtimeout > /dev/null 2>&1; then
    TIMEOUT_BIN="gtimeout"
fi

# run_bounded <seconds> <command...>
# Runs the command, killing it after <seconds> when a timeout binary is
# available; otherwise runs it unbounded (best effort on systems without one).
run_bounded() {
    local secs="$1"
    shift
    if [[ -n "$TIMEOUT_BIN" ]]; then
        "$TIMEOUT_BIN" "$secs" "$@"
    else
        "$@"
    fi
}

# wait_for <display-name> <compose-service> <check command...>
# Retries the check until it succeeds or the timeout elapses. Each probe is
# bounded by the time left on the deadline so a hung probe cannot block past it.
# On timeout it prints the container status and recent logs, then exits nonzero
# so the caller can detect the infrastructure failure.
wait_for() {
    local name="$1" service="$2"
    shift 2

    local deadline=$((SECONDS + READINESS_TIMEOUT))
    while true; do
        local remaining=$((deadline - SECONDS))
        if ((remaining <= 0)); then
            echo "❌ Timed out after ${READINESS_TIMEOUT}s waiting for ${name} to be ready." >&2
            echo "Container status:" >&2
            run_bounded 10 docker compose ps "$service" >&2 || true
            echo "Recent ${service} logs:" >&2
            run_bounded 10 docker compose logs --tail=50 "$service" >&2 || true
            exit 1
        fi

        # Cap each probe at the remaining time (and 5s for periodic feedback).
        local probe=$((remaining < 5 ? remaining : 5))
        if run_bounded "$probe" "$@" > /dev/null 2>&1; then
            return 0
        fi

        echo "Waiting for ${name} to be ready..."
        sleep 1
    done
}

# Use psql to wait for the database to be ready
wait_for "Postgres" gram-db \
    docker compose exec -T gram-db psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1"

# ClickHouse takes longer than Postgres to accept queries. Migrations run
# immediately after infra starts, so without waiting here the first ClickHouse
# migration can fail with a connection EOF.
wait_for "ClickHouse" clickhouse \
    docker compose exec -T clickhouse clickhouse-client --user "$CLICKHOUSE_USERNAME" --password "$CLICKHOUSE_PASSWORD" -q "SELECT 1"
