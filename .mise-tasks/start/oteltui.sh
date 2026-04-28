#!/usr/bin/env bash

#MISE description="Start otel-tui for viewing OpenTelemetry traces and metrics emitted by Gram"
#MISE hide=true

set -e

docker compose up -d oteltui
docker compose attach oteltui