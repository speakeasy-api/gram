#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

# Presidio analyzer, shared across all worktrees under a fixed project name so a
# worktree's COMPOSE_PROJECT_NAME cannot fork it into a second copy. Bringing it
# up is idempotent, so every worktree can safely (re)assert it here. A failure
# here (e.g. a transient pull of the ~1 GB image) must NOT take down this
# worktree's own databases, so warn and continue rather than aborting.
docker compose -f compose.shared.yml -p gram-shared up -d \
  || echo "⚠️  Shared Presidio analyzer failed to start; continuing. PII scanning stays degraded until it is up." >&2

# --remove-orphans clears the now-shared gram-presidio container that a
# pre-existing worktree still runs under its own project (compose.yml no longer
# declares it), so the duplicate ~1 GB analyzer stops. Profile-gated services
# (litellm, tunnel, local-registry) remain declared in compose.yml and are not
# treated as orphans.
docker compose up -d --remove-orphans || exit 1

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

# run_bounded <seconds> <command...>
# Runs the command via `gum spin` (gum is provided by mise), which aborts it
# after <seconds> and propagates its exit code (124 on timeout). This bounds
# each probe so a hung `docker compose exec` or Docker Engine call cannot block
# past the deadline.
run_bounded() {
    local secs="$1"
    shift
    gum spin --timeout "${secs}s" --show-output -- "$@"
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
            run_bounded 10 docker compose ps -a "$service" >&2 || true
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

# Temporal is the last of the three to become usable, and the first thing that
# needs it is `mise run seed`, whose deployment step starts a workflow. Without
# this probe seed fails with `error starting deployment: context deadline
# exceeded` — a 10s timeout on a Temporal that isn't serving yet — which on a
# cold worktree leaves the stack up but only partially seeded.
#
# `cluster health` hangs rather than erroring when the dev-server's SQLite
# persistence is wedged, so it relies on run_bounded to cap each attempt.
wait_for "Temporal" gram-temporal \
    docker compose exec -T gram-temporal temporal operator cluster health
