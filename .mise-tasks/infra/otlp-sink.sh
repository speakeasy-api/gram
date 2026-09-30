#!/usr/bin/env bash

#MISE description="Start the shared OTLP sink that ACKs local telemetry without LGTM"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Default shared collector. Do not start it while LGTM already owns 4317/4318;
# that stack is the real sink in that case. `infra:lgtm` removes this
# container before binding those ports.
if docker ps --filter "label=com.docker.compose.project=gram-shared" \
  --filter "label=com.docker.compose.service=lgtm" \
  --filter "status=running" --format '{{.ID}}' | grep -q .; then
  echo "Shared LGTM is already listening on OTLP 4317/4318; leaving the sink down."
  exit 0
fi

docker compose -f compose.shared.yml -p gram-shared up -d --wait --wait-timeout 30 otlp-sink
