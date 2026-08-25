#!/usr/bin/env bash
#MISE description="Start up databases, caches and so on"

# Warn when a cached image's architecture differs from the Docker host's.
# `compose up` never re-pulls a tag that already exists locally, so an image
# pulled while DOCKER_DEFAULT_PLATFORM=linux/amd64 was exported keeps running
# under emulation on every stack — on Apple Silicon an emulated Postgres
# backend segfaults under load (exit code 2 → crash recovery → every daemon
# fails its DB ping mid-boot). Warn-only: emulation mostly works, and a hard
# fail would strand hosts that have no native variant.
#
# Digest-pinned images are checked too. A pin is usually the digest of a
# multi-arch INDEX, not of one platform's manifest, so the cached copy behind it
# can still be the wrong architecture -- which is exactly how the pubsub
# emulator ended up running under emulation on every worktree stack while this
# check stayed silent.
host_arch="$(docker version --format '{{.Server.Arch}}' 2>/dev/null)"
if [ -n "$host_arch" ]; then
    # Keep this advisory check to a constant number of daemon round-trips no
    # matter how many images the stack declares: list local tags and digests
    # once, intersect with the compose images, then inspect the cached subset in
    # a single call (its output lines map back to the input by position). A
    # per-image inspect would add a slow serial round-trip per image on Docker
    # Desktop.
    local_tags="$(docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null)"
    local_digests="$(docker image ls --digests --format '{{.Repository}}@{{.Digest}}' 2>/dev/null)"
    cached_imgs=()
    while IFS= read -r img; do
        case "$img" in
            *@sha256:*)
                # `repo:tag@sha256:...` and `repo@sha256:...` both reduce to the
                # `repo@sha256:...` form that `docker image ls --digests` prints.
                # Only the final path component can carry a tag, so look for the
                # separating colon there — stripping from the whole reference
                # would eat a registry port instead (`host:5000/img@sha256:...`).
                repo="${img%@*}"
                case "${repo##*/}" in
                    *:*) repo="${repo%:*}" ;;
                esac
                key="${repo}@${img#*@}"
                ;;
            *:*) key="$img" ;;
            *) key="$img:latest" ;;
        esac
        if grep -qxF "$key" <<< "$local_tags"$'\n'"$local_digests"; then
            cached_imgs+=("$img")
        fi
    done < <({ docker compose config --images; docker compose -f compose.shared.yml -p gram-shared config --images; } 2>/dev/null | sort -u)
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
                # A digest-pinned ref cannot simply be re-pulled: the daemon
                # refuses to rebind a digest it already has ("cannot overwrite
                # digest"), so the local copy has to be evicted first — and
                # `docker rmi` refuses while ANY container still references it,
                # including sibling worktrees'. Scoping the eviction to this
                # compose project would therefore just make the remedy fail, so
                # it stays repo-wide and the caveat is stated instead. The
                # containers it removes come back with the next `infra:start`.
                case "$img" in
                    *@sha256:*)
                        fix="${fix_prefix}docker rm -f \$(docker ps -aq --filter ancestor=$img) 2>/dev/null; docker rmi $img && docker pull --platform linux/$host_arch $img"
                        fix="$fix (removes this image's containers in every worktree stack; each is recreated by its next \`mise infra:start\`)"
                        ;;
                    *) fix="${fix_prefix}docker pull --platform linux/$host_arch $img" ;;
                esac
                echo "⚠️  Cached image $img is $img_arch but this Docker host is $host_arch — it runs emulated and can crash under load. Fix: $fix" >&2
            fi
        done < <(docker image inspect "${cached_imgs[@]}" --format '{{.Architecture}}' 2>/dev/null)
    fi
fi

# This worktree's own stack, first — with --remove-orphans. Pre-existing
# worktrees can still run Pub/Sub or Presidio containers that compose.yml no
# longer declares. Removing this worktree's copies before asserting the shared
# services frees fixed ports in the main tree and removes obsolete remapped
# copies elsewhere. Profile-gated services (litellm, tunnel, local-registry)
# remain declared in compose.yml and are not treated as orphans.
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

# One-time migration: the main tree previously bound its per-worktree Pub/Sub
# emulator to the shared port. Remove only a non-shared emulator actually
# publishing 8088. Sibling worktrees on remapped ports keep running until their
# next git:worksync/infra:start migration.
docker ps -a --filter "label=com.docker.compose.service=pubsub-emulator" --filter "publish=8088" \
  --format '{{.Label "com.docker.compose.project"}} {{.ID}}' 2>/dev/null \
  | awk '$1 != "gram-shared" { print $2 }' \
  | xargs -r docker rm -f > /dev/null 2>&1 || true

# Pub/Sub is required by the local streams processes. Wait for its TCP
# healthcheck so a container that starts and immediately exits cannot let
# infrastructure startup report success.
docker compose -f compose.shared.yml -p gram-shared up -d --wait --wait-timeout 30 pubsub-emulator || exit 1

# Presidio and LGTM are shared too, but neither is a synchronous startup
# dependency. A transient image pull or cold model must not take down this
# worktree's databases, so warn and continue.
docker compose -f compose.shared.yml -p gram-shared up -d gram-presidio lgtm \
  || echo "⚠️  Optional shared Presidio/LGTM services failed to start; continuing with degraded PII scanning or observability." >&2

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
# Runs the command, aborting it after <seconds> and returning 124 in that case.
# This bounds each probe so a hung `docker compose exec` or Docker Engine call
# cannot block past the deadline.
#
# Deliberately implemented without `gum spin` (or anything else that draws to
# the terminal). `git:workboot` runs as a BACKGROUND process group under wt's
# post-start hook, and a background process that reads the controlling terminal
# is stopped with SIGTTIN. `gum spin` is a TUI, so it was suspended on its first
# probe -- and with it its own `--timeout` timer -- leaving the boot hung in
# "booting" with healthy containers and no further output. macOS ships no
# `timeout(1)`, hence the hand-rolled poll.
run_bounded() {
    local secs="$1"
    shift

    "$@" &
    local pid=$!

    # Poll at 200ms so a probe that answers immediately still returns
    # immediately. bash reaps exited background jobs, so `kill -0` failing is a
    # reliable "it finished" signal here.
    local ticks=$((secs * 5))
    local i=0
    while [ "$i" -lt "$ticks" ] && kill -0 "$pid" 2>/dev/null; do
        sleep 0.2
        i=$((i + 1))
    done

    if kill -0 "$pid" 2>/dev/null; then
        kill -TERM "$pid" 2>/dev/null
        sleep 1
        kill -KILL "$pid" 2>/dev/null
        wait "$pid" 2>/dev/null
        return 124
    fi

    wait "$pid"
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
