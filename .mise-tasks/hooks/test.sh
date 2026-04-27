#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--local" help="Always use local plugin directory instead of published plugin"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

# Configure OTEL export so the session gets validated in Redis.
# Without this, blocking policies can't resolve the project.
if [ -n "${GRAM_HOOKS_API_KEY:-}" ] && [ -n "${GRAM_HOOKS_PROJECT_SLUG:-}" ]; then
  export CLAUDE_CODE_ENABLE_TELEMETRY=1
  export OTEL_LOGS_EXPORTER=otlp
  export OTEL_METRICS_EXPORTER=otlp
  export OTEL_EXPORTER_OTLP_PROTOCOL=http/json
  export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs"
  export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/metrics"
  export OTEL_EXPORTER_OTLP_HEADERS="x-api-key=${GRAM_HOOKS_API_KEY},x-project-slug=${GRAM_HOOKS_PROJECT_SLUG}"
  echo "OTEL export configured (project: ${GRAM_HOOKS_PROJECT_SLUG})"
else
  echo "Note: Set GRAM_HOOKS_API_KEY and GRAM_HOOKS_PROJECT_SLUG to enable"
  echo "      session validation for blocking policy testing."
fi

if [ "${usage_local:-}" = "true" ] || ! git diff --quiet HEAD -- hooks/; then
  echo "Using local plugin directory: ./hooks/plugin-claude-test"
  echo ""
  exec claude --plugin-dir ./hooks/plugin-claude-test --debug
else
  echo "No local changes in hooks/ — using published plugin"
  echo ""
  exec claude --debug
fi
