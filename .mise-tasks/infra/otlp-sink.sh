#!/usr/bin/env bash

#MISE description="Start the shared OTLP sink that ACKs local telemetry"
#MISE dir="{{ config_root }}"

set -euo pipefail

# A leftover grafana/otel-lgtm container from before the sink existed still
# holds 4317/4318, and `restart: unless-stopped` brings it back after a stop.
docker ps -a --filter "label=com.docker.compose.project=gram-shared" \
  --filter "label=com.docker.compose.service=lgtm" -q 2>/dev/null \
  | xargs -r docker rm -f > /dev/null 2>&1 || true

docker compose -f compose.shared.yml -p gram-shared up -d --wait --wait-timeout 30 otlp-sink
