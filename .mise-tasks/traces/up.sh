#!/usr/bin/env bash

#MISE description="Opt in to viewing this worktree's traces in a local Jaeger UI"
#MISE dir="{{ config_root }}"

set -euo pipefail

# The shared otlp-sink (compose.shared.yml) drops every span. This starts one
# Jaeger container, shared by every worktree that opts in, and points this
# worktree's OTLP exporters at it through mise.local.toml. The default sink is
# untouched; `mise run traces:down` undoes the opt-in.
#
# Spans carry OTEL_RESOURCE_ATTRIBUTES (worktree=...), so filter by that tag
# in the UI when several worktrees share the container.

container="gram-traces-jaeger"
image="jaegertracing/all-in-one:1.62.0@sha256:836e9b69c88afbedf7683ea7162e179de63b1f981662e83f5ebb68badadc710f"
otlp_grpc_port=14317
ui_port=16686
endpoint="http://localhost:${otlp_grpc_port}"

# Leave a collector the developer configured on their own alone: overwriting
# it here would lose it, and `traces:down` could not restore it.
current="$(mise config get --file mise.local.toml env.OTEL_EXPORTER_OTLP_ENDPOINT 2> /dev/null || true)"
if [ -n "$current" ] && [ "$current" != "$endpoint" ]; then
  echo "mise.local.toml already sets OTEL_EXPORTER_OTLP_ENDPOINT to another collector; leaving it unchanged." >&2
  echo "Remove that setting first to use the local Jaeger UI." >&2
  exit 1
fi

if [ -z "$(docker ps -q --filter "name=^${container}$")" ]; then
  docker rm -f "$container" > /dev/null 2>&1 || true
  docker run -d --name "$container" --restart unless-stopped \
    -e COLLECTOR_OTLP_ENABLED=true \
    -p "127.0.0.1:${otlp_grpc_port}:4317" \
    -p "127.0.0.1:${ui_port}:16686" \
    "$image" > /dev/null
fi

mise set --file mise.local.toml "OTEL_EXPORTER_OTLP_ENDPOINT=${endpoint}"

# Running daemons keep the old endpoint until they restart. The server and
# worker also hand the endpoint to local assistant runner containers, which
# are replaced on their next turn.
for daemon in server worker; do
  if mise exec -- pitchfork status "$daemon" 2> /dev/null | grep -q "Status: running"; then
    mise exec -- pitchfork restart "$daemon"
  fi
done

echo "Traces: http://localhost:${ui_port} (OTLP gRPC ${endpoint})"
echo "Other daemons pick the endpoint up on their next start."
