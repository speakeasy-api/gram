#!/usr/bin/env bash

# local/lib/compose.sh — the canonical container-engine entrypoint for local dev.
#
# This file is the ONLY place that knows which container engine and compose
# provider Gram uses locally: rootless Podman, driven by the standalone
# docker-compose CLI pointed at the podman API socket via DOCKER_HOST.
#
# Usage from bash scripts (preferred):
#
#     source "<repo-root>/local/lib/compose.sh"
#     compose up -d
#     compose --profile "*" down --remove-orphans
#
# Usage from non-shell callers (mts/mjs tasks, mprocs argv arrays) that cannot
# source a shell function — run this file as a command instead:
#
#     bash <repo-root>/local/lib/compose.sh logs -f otel-collector
#
# It is intentionally NOT executable so nothing lists it as a task; invoke it
# through `bash` in the command form.
#
# Responsibilities owned here (and nowhere else):
#   - Podman API socket path (per-uid) and starting `podman system service`
#     idempotently (no systemd socket activation; works in cloud sandboxes).
#   - Exporting DOCKER_HOST so docker-compose/testcontainers/atlas hit podman.
#   - Pre-building local images with `podman build`. Compose NEVER builds:
#     docker-compose v2 requires BuildKit for builds and podman's docker-compat
#     socket does not implement BuildKit sessions, so `compose up` always gets
#     `--no-build` appended and the affected services carry `image:` +
#     `pull_policy: never` in compose.yml.
#   - A healthcheck pump for hosts without a systemd (user) session: podman
#     schedules healthchecks via systemd timers, so without one, containers
#     stay "starting" forever and `depends_on: condition: service_healthy`
#     hangs. The pump runs `podman healthcheck run` manually every 5s.
#
# shellcheck shell=bash

GRAM_REPO_ROOT="${GRAM_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
export GRAM_REPO_ROOT

# Image tags for locally built services. These MUST stay in sync with the
# `image:` fields in compose.yml.
GRAM_CLICKHOUSE_IMAGE="gram-local-clickhouse:latest"
GRAM_TUNNEL_MCP_IMAGE="gram-postgres-mcp-streamable:07eb329c8c48"
GRAM_TUNNEL_MCP_BUILD_CONTEXT="https://github.com/crystaldba/postgres-mcp.git#07eb329c8c48e49640e0d1b5b35465d4d024c3ee"

podman_socket_path() {
    # NOTE: unix socket paths are limited to ~108 chars — never place the
    # socket under the repo/worktree path. One socket per user, shared by all
    # worktrees; project separation stays COMPOSE_PROJECT_NAME's job.
    if [ "$(id -u)" -eq 0 ]; then
        echo "/run/podman/podman.sock"
    else
        echo "${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/podman/podman.sock"
    fi
}

podman_socket_ready() {
    podman --url "unix://$1" version >/dev/null 2>&1
}

# Podman runs healthchecks via systemd (user) transient timers. Detect whether
# one is available for the current uid.
have_systemd_user() {
    if [ "$(id -u)" -ne 0 ]; then
        systemctl --user is-system-running >/dev/null 2>&1 && return 0
        return 1
    fi
    [ -d /run/systemd/system ] && return 0
    return 1
}

ensure_health_pump() {
    have_systemd_user && return 0
    pgrep -f gram-podman-health-pump >/dev/null 2>&1 && return 0
    (setsid bash -c 'exec -a gram-podman-health-pump bash -c "
        while :; do
            podman ps -q --filter health=starting 2>/dev/null | xargs -r -n1 podman healthcheck run >/dev/null 2>&1
            sleep 5
        done"' >/dev/null 2>&1 &)
}

ensure_podman_socket() {
    local sock
    sock="$(podman_socket_path)"
    export DOCKER_HOST="unix://${sock}"
    if podman_socket_ready "$sock"; then
        ensure_health_pump
        return 0
    fi

    local log_dir="${XDG_STATE_HOME:-$HOME/.local/state}"
    mkdir -p "$log_dir" "$(dirname "$sock")"
    local lock="${sock}.start.lock"
    (
        flock 9
        podman_socket_ready "$sock" && exit 0
        rm -f "$sock"
        (setsid podman system service --time=0 "unix://${sock}" \
            >>"${log_dir}/gram-podman-service.log" 2>&1 &)
    ) 9>"$lock"

    local i
    for i in $(seq 1 40); do
        if podman_socket_ready "$sock"; then
            ensure_health_pump
            return 0
        fi
        sleep 0.25
    done
    echo "❌ podman API socket failed to start at $sock (see ${log_dir}/gram-podman-service.log)" >&2
    return 1
}

# Pre-build images that compose.yml declares with `pull_policy: never`.
# Idempotent: skips when the tagged image already exists.
ensure_local_images() {
    podman image exists "$GRAM_CLICKHOUSE_IMAGE" || \
        podman build -t "$GRAM_CLICKHOUSE_IMAGE" "${GRAM_REPO_ROOT}/local/clickhouse"
    # The tunnel image (git-URL build context) is built lazily by
    # `ensure_tunnel_image` from the tunnel task — not here — so plain
    # `compose up` never clones/builds a remote repo.
}

# Force a rebuild of the local ClickHouse image (used by clickhouse:rebuild).
rebuild_clickhouse_image() {
    podman build -t "$GRAM_CLICKHOUSE_IMAGE" "${GRAM_REPO_ROOT}/local/clickhouse"
}

# Build the tunnel Postgres MCP image from its git URL context if missing.
# `podman build` supports git URLs with a #<commit> fragment natively.
ensure_tunnel_image() {
    podman image exists "$GRAM_TUNNEL_MCP_IMAGE" || \
        podman build -t "$GRAM_TUNNEL_MCP_IMAGE" "$GRAM_TUNNEL_MCP_BUILD_CONTEXT"
}

# compose <args...> — the one true compose invocation. Ensures the podman
# socket is up, pre-builds local images for `up`, and always disables compose
# builds (see header). Global flags like `--profile x` may precede the
# subcommand exactly as with the raw CLI.
compose() {
    ensure_podman_socket || return 1

    # Find the compose subcommand, skipping global flags and their values.
    local subcommand="" skip_next=0 arg
    for arg in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        case "$arg" in
            --profile|-p|--project-name|-f|--file|--env-file|--project-directory|--progress)
                skip_next=1
                ;;
            -*) ;;
            *)
                subcommand="$arg"
                break
                ;;
        esac
    done

    if [ "$subcommand" = "up" ]; then
        ensure_local_images || return 1
        set -- "$@" --no-build
    fi

    # In command form (see footer), replace this bash process with the compose
    # CLI so callers that signal/kill us (e.g. `gum spin` timeouts) hit the
    # real process instead of orphaning a grandchild.
    if [ "${GRAM_COMPOSE_EXEC:-0}" = "1" ]; then
        exec docker-compose -f "${GRAM_REPO_ROOT}/compose.yml" --project-directory "$GRAM_REPO_ROOT" "$@"
    fi
    docker-compose -f "${GRAM_REPO_ROOT}/compose.yml" --project-directory "$GRAM_REPO_ROOT" "$@"
}

# Command form: `bash local/lib/compose.sh <compose args...>` for callers that
# cannot source shell functions (gum spin, mprocs, Node scripts).
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    set -euo pipefail
    GRAM_COMPOSE_EXEC=1 compose "$@"
fi
