#!/usr/bin/env bash

#MISE description="Start the shared OTLP sink that ACKs local telemetry"
#MISE dir="{{ config_root }}"

set -euo pipefail

docker compose -f compose.shared.yml -p gram-shared up -d --wait --wait-timeout 30 otlp-sink
