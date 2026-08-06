#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

# Warn when a cached image's architecture differs from the Docker host's.
# `compose up` never re-pulls a tag that already exists locally, so an image
# pulled while DOCKER_DEFAULT_PLATFORM=linux/amd64 was exported keeps running
# under emulation on every stack — on Apple Silicon an emulated Postgres
# backend segfaults under load (exit code 2 → crash recovery → every daemon
# fails its DB ping mid-boot). Warn-only: emulation mostly works, and a hard
# fail would strand hosts that have no native variant. Digest-pinned images are
# skipped because a re-pull cannot change what a digest points at.
host_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null)"
if [ -n "$host_arch" ]; then
    # Keep this advisory check to two daemon round-trips no matter how many
    # images the stack declares: list local tags once, intersect with the
    # compose images, then inspect the cached subset in a single call (its
    # output lines map back to the input by position). A per-image inspect
    # would add a slow serial round-trip per image on Docker Desktop.
    local_tags="$(docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null)"
    cached_imgs=()
    while IFS= read -r img; do
        case "$img" in *@sha256:*) continue ;; esac
        case "$img" in *:*) tag="$img" ;; *) tag="$img:latest" ;; esac
        if grep -qxF "$tag" <<< "$local_tags"; then
            cached_imgs+=("$img")
        fi
    done < <(docker compose config --images 2>/dev/null | sort -u)
    # `docker pull` honors an exported DOCKER_DEFAULT_PLATFORM, so while it is
    # set the pull just re-fetches the same foreign-arch variant and the
    # warning would never clear — tell the user to unset it first.
    fix_prefix=""
    if [ -n "${DOCKER_DEFAULT_PLATFORM:-}" ]; then
        fix_prefix="unset DOCKER_DEFAULT_PLATFORM, then "
    fi
    if [ "${#cached_imgs[@]}" -gt 0 ]; then
        i=0
        while IFS= read -r img_arch; do
            img="${cached_imgs[$i]}"
            i=$((i + 1))
            if [ -n "$img_arch" ] && [ "$img_arch" != "$host_arch" ]; then
                echo "⚠️  Cached image $img is $img_arch but this Docker host is $host_arch — it runs emulated and can crash under load. Fix: ${fix_prefix}docker pull $img" >&2
            fi
        done < <(docker image inspect "${cached_imgs[@]}" --format '{{.Architecture}}' 2>/dev/null)
    fi
fi

# This worktree's own stack, first — with --remove-orphans. A pre-existing
# worktree (and the main tree) still runs a gram-presidio container under its own
# project that compose.yml no longer declares; removing it here, BEFORE asserting
# the shared analyzer below, frees the old host port (5050 on the main tree,
# which never remapped it) so the shared `up` can bind it in this same run
# instead of losing the port to the stale container and only converging next
# time. Profile-gated services (litellm, tunnel, local-registry) stay declared in
# compose.yml and are not treated as orphans.
docker compose up -d --remove-orphans || exit 1

# One-time migration: free host port 5050 for the shared analyzer. Before the
# shared stack existed, the main tree ran its own gram-presidio bound to 5050
# under its own compose project (the main tree never remaps PRESIDIO_PORT). The
# --remove-orphans above only touches THIS worktree's project, so that stale
# container would keep 5050 and block the shared `up` below. Remove ONLY the
# non-`gram-shared` container that actually holds 5050 — scoping by the port
# keeps this from touching a sibling worktree's still-in-use analyzer bound to
# its own remapped port, which the sibling's app is still pointed at until it
# runs `git:worksync`. Idempotent: once migrated there is nothing to remove.
docker ps -a --filter "label=com.docker.compose.service=gram-presidio" --filter "publish=5050" \
  --format '{{.Label "com.docker.compose.project"}} {{.ID}}' 2>/dev/null \
  | awk '$1 != "gram-shared" { print $2 }' \
  | xargs -r docker rm -f > /dev/null 2>&1 || true

# Presidio analyzer, shared across all worktrees under a fixed project name so a
# worktree's COMPOSE_PROJECT_NAME cannot fork it into a second copy. Bringing it
# up is idempotent, so every worktree can safely (re)assert it here. A failure
# here (e.g. a transient pull of the ~1 GB image) must NOT take down this
# worktree's own databases, so warn and continue rather than aborting.
docker compose -f compose.shared.yml -p gram-shared up -d \
  || echo "⚠️  Shared Presidio analyzer failed to start; continuing. PII scanning stays degraded until it is up." >&2

# Best-effort readiness for the shared analyzer. `up -d` returns once the
# container is created, not once its ~1 GB spaCy model has loaded, so poll the
# container's own healthcheck to keep infra:start's success signal honest. This
# is deliberately NON-fatal and NOT a hard gate: nothing in the startup path
# consumes Presidio synchronously (only background Temporal risk activities do,
# and they already tolerate/retry an unavailable analyzer), so a cold model load
# must not block the rest of the stack. Since Presidio is a long-lived shared
# singleton, this returns instantly on every run after the first. Override the
# bound with PRESIDIO_READINESS_TIMEOUT; <=0 or a non-integer skips the wait.
PRESIDIO_READINESS_TIMEOUT="${PRESIDIO_READINESS_TIMEOUT:-90}"
# Normalize to a plain decimal int before any arithmetic: a leading-zero
# override (e.g. "08") would otherwise be misread as octal — "08"/"09" error and
# "010" means 8 — so validate the digits, then re-base with 10# (matching
# INFRA_READINESS_TIMEOUT below). A non-integer becomes 0, which skips the wait.
if [[ "$PRESIDIO_READINESS_TIMEOUT" =~ ^[0-9]+$ ]]; then
  PRESIDIO_READINESS_TIMEOUT=$((10#$PRESIDIO_READINESS_TIMEOUT))
else
  PRESIDIO_READINESS_TIMEOUT=0
fi
presidio_cid="$(docker compose -f compose.shared.yml -p gram-shared ps -q gram-presidio 2>/dev/null)"
if [[ -n "$presidio_cid" && "$PRESIDIO_READINESS_TIMEOUT" -gt 0 ]]; then
  presidio_deadline=$((SECONDS + PRESIDIO_READINESS_TIMEOUT))
  until [ "$(docker inspect -f '{{.State.Health.Status}}' "$presidio_cid" 2>/dev/null)" = "healthy" ]; do
    if ((SECONDS >= presidio_deadline)); then
      echo "⚠️  Shared Presidio analyzer not healthy after ${PRESIDIO_READINESS_TIMEOUT}s; continuing (PII scanning catches up once it is ready)." >&2
      break
    fi
    sleep 2
  done
fi

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
