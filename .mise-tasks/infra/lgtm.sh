#!/usr/bin/env bash

#MISE description="Start the optional shared LGTM observability stack (Grafana, Tempo, Prometheus)"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Large image, unused by most local work. Opt in here or with
# `mise run infra:start --lgtm`. `mise run open:grafana` starts it on demand.
#
# LGTM binds the same host OTLP ports as the default sink. Remove the sink
# first (stop is not enough: `restart: unless-stopped` would bring it back
# on daemon restart and fight LGTM for 4317/4318).
docker compose -f compose.shared.yml -p gram-shared rm -sf otlp-sink >/dev/null 2>&1 || true
if ! docker compose -f compose.shared.yml -p gram-shared --profile lgtm up -d lgtm; then
  echo "⚠️  LGTM failed to start; restoring the OTLP sink so exporters keep a listener." >&2
  docker compose -f compose.shared.yml -p gram-shared up -d otlp-sink || true
  exit 1
fi
