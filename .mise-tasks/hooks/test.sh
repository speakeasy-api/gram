#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--local" help="Always use local plugin directory instead of published plugin"
#USAGE flag "--project <slug>" help="Project slug for OTEL session validation (enables blocking)" default="ecommerce-api"

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

# Provision a dev API key for the chosen project so Claude's OTEL exporter can
# authenticate against /rpc/hooks.otel and the server can validate the
# session. Without this, the hook's getSessionMetadata lookup misses and the
# risk scanner silently bails (no project to scope policies to).
#
# This is a local-dev shortcut — production keys go through /rpc/keys.create
# with proper auth and audit logging. Inlined here so it's obvious it's a
# test fixture, not a CLI tool.
project_slug="${usage_project}"
echo "Provisioning dev API key for project: ${project_slug}"

# db_query <psql -v args> < SQL
# Pipes SQL via stdin so psql's :'name' variable substitution actually fires
# (the -c flag bypasses the psql lexer that does the interpolation).
db_query() {
  docker exec -i gram-gram-db-1 psql -U gram -d gram -tA -v ON_ERROR_STOP=1 "$@"
}

project_row=$(db_query -v slug="$project_slug" <<<"SELECT id, organization_id FROM projects WHERE slug = :'slug' AND deleted IS FALSE LIMIT 1" 2>/dev/null || true)

if [ -z "$project_row" ]; then
  echo "Warning: project '${project_slug}' not found — blocking policies will be inert."
else
  project_id="${project_row%%|*}"
  org_id="${project_row##*|}"

  user_id=$(db_query <<<"SELECT id FROM users LIMIT 1" 2>/dev/null || true)
  if [ -z "$user_id" ]; then
    echo "Warning: no users in DB — skipping API key provisioning."
  else
    # Soft-delete any prior dev key for this project so we can stash a
    # new plaintext we know.
    db_query -v project_id="$project_id" >/dev/null <<<"UPDATE api_keys SET deleted_at = NOW() WHERE project_id = :'project_id' AND name = 'dev-hooks-test' AND deleted IS FALSE"

    token_hex=$(openssl rand -hex 32)
    api_key="gram_local_${token_hex}"
    key_prefix="gram_local_${token_hex:0:5}"
    key_hash=$(printf '%s' "$api_key" | shasum -a 256 | awk '{print $1}')

    db_query \
      -v org_id="$org_id" \
      -v project_id="$project_id" \
      -v user_id="$user_id" \
      -v key_prefix="$key_prefix" \
      -v key_hash="$key_hash" \
      >/dev/null <<<"INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes) VALUES (:'org_id', :'project_id', :'user_id', 'dev-hooks-test', :'key_prefix', :'key_hash', '{hooks}')"

    export CLAUDE_CODE_ENABLE_TELEMETRY=1
    export OTEL_LOGS_EXPORTER=otlp
    export OTEL_METRICS_EXPORTER=otlp
    export OTEL_EXPORTER_OTLP_PROTOCOL=http/json
    export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs"
    export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT="${GRAM_SERVER_URL}/rpc/hooks.otel/v1/metrics"
    export OTEL_EXPORTER_OTLP_HEADERS="Gram-Key=${api_key},Gram-Project=${project_slug}"
    echo "OTEL configured (key: ${api_key:0:20}...)"
  fi
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
