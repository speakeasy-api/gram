#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--local" help="Always use local plugin directory instead of published plugin"
#USAGE flag "--project <slug>" help="Project slug for OTEL session validation (enables blocking)" default="ecommerce-api"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

# Auto-provision a dev API key and configure OTEL export so the session
# gets validated in Redis. Required for blocking policies to work.
project_slug="${usage_project}"
echo "Provisioning dev API key for project: ${project_slug}"
api_key=$(go run ./server/cmd/dev-api-key --project-slug="$project_slug" 2>&1)
if [[ "$api_key" == gram_* ]]; then
  export CLAUDE_CODE_ENABLE_TELEMETRY=1
  export OTEL_LOGS_EXPORTER=otlp
  export OTEL_METRICS_EXPORTER=otlp
  export OTEL_EXPORTER_OTLP_PROTOCOL=http/json
  export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs"
  export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/metrics"
  export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=${api_key},x-project-slug=${project_slug}"
  echo "OTEL configured (key: ${api_key:0:20}...)"
else
  echo "Warning: failed to provision API key: ${api_key}"
  echo "Blocking policies won't work without session validation."
fi
echo ""

if [ "${usage_local:-}" = "true" ] || ! git diff --quiet HEAD -- hooks/; then
  echo "Using local plugin directory: ./hooks/plugin-claude-test"
  echo ""
  exec claude --plugin-dir ./hooks/plugin-claude-test --debug
else
  echo "No local changes in hooks/ — using published plugin"
  echo ""
  exec claude --debug
fi
