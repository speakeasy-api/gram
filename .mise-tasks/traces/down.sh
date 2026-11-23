#!/usr/bin/env bash

#MISE description="Stop sending this worktree's traces to the local Jaeger UI"
#MISE dir="{{ config_root }}"
#USAGE flag "--stop" help="Also remove the shared Jaeger container (other worktrees that opted in lose their trace view)"

set -euo pipefail

mise unset --file mise.local.toml OTEL_EXPORTER_OTLP_ENDPOINT

# Back on the default endpoint from mise.toml (the shared otlp-sink).
for daemon in server worker; do
  if mise exec -- pitchfork status "$daemon" 2> /dev/null | grep -q "Status: running"; then
    mise exec -- pitchfork restart "$daemon"
  fi
done

if [ "${usage_stop:-false}" = "true" ]; then
  docker rm -f gram-traces-jaeger > /dev/null 2>&1 || true
  echo "Removed the shared Jaeger container."
fi
echo "This worktree exports traces to the shared sink again."
