#!/usr/bin/env bash
# The first thing to run inside the agent jail, as the jail user (see
# jail.sh). Loads the run's environment, opens one TCP listener per bridged
# port, then becomes the command.

set -euo pipefail

env_file="$1" ports="$2" sock_dir="$3"
shift 3

set -a
# shellcheck disable=SC1090
. "$env_file"
set +a

IFS=, read -ra list <<<"$ports"
for port in "${list[@]}"; do
    [ -n "$port" ] || continue
    node /opt/agent-jail/bridge.mjs tcp-to-unix "$port" "$sock_dir/$port.sock" &
done
for port in "${list[@]}"; do
    [ -n "$port" ] || continue
    for _ in $(seq 1 50); do
        (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break
        sleep 0.1
    done
done

exec "$@"
