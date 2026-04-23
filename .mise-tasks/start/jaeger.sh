#!/usr/bin/env bash

#MISE description="Start Jaeger for viewing OpenTelemetry traces emitted by Gram"
#MISE hide=true

set -e

docker compose up -d jaeger
echo "Jaeger UI: http://localhost:${JAEGER_WEB_PORT}"
echo "OTLP gRPC receiver: localhost:${OTLP_GRPC_PORT}"
docker compose logs -f jaeger
