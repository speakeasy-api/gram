#!/usr/bin/env bash

#MISE description="Stop sending this worktree's traces to the local Jaeger UI"
#MISE dir="{{ config_root }}"
#USAGE flag "--stop" help="Also remove the shared Jaeger container (other worktrees that opted in lose their trace view)"

set -euo pipefail

# Must match the endpoint `traces:up` writes.
endpoint="http://localhost:14317"

# Only undo what `traces:up` installed. Any other endpoint is the developer's
# own setting, and a worktree that never opted in has nothing to undo.
current="$(mise config get --file mise.local.toml env.OTEL_EXPORTER_OTLP_ENDPOINT 2> /dev/null || true)"
if [ "$current" = "$endpoint" ]; then
  mise unset --file mise.local.toml OTEL_EXPORTER_OTLP_ENDPOINT

  # Back on the default endpoint from mise.toml (the shared otlp-sink).
  for daemon in server worker; do
    if mise exec -- pitchfork status "$daemon" 2> /dev/null | grep -q "Status: running"; then
      mise exec -- pitchfork restart "$daemon"
    fi
  done
  echo "This worktree exports traces to the shared sink again."
else
  echo "This worktree did not opt in with traces:up; its trace settings are unchanged."
fi

if [ "${usage_stop:-false}" = "true" ]; then
  docker rm -f gram-traces-jaeger > /dev/null 2>&1 || true
  echo "Removed the shared Jaeger container."
fi
