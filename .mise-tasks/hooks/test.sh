#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--project <slug>" help="Project slug for OTEL session validation (enables blocking)" default="ecommerce-api"
#USAGE flag "--local" help="Deprecated no-op; the plugin is always rendered locally now from the current branch's generator."

set -euo pipefail

export GRAM_HOOKS_SERVER_URL=$GRAM_SERVER_URL

# Provision a dev API key for the chosen project so Claude's OTEL exporter can
# authenticate against /rpc/hooks.otel and the server can validate the
# session, and so the hook script's Gram-Key header authenticates against the
# local server (the published plugin bakes in a prod key that 401s here).
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
  echo "Error: project '${project_slug}' not found — cannot provision dev key." >&2
  exit 1
fi

project_id="${project_row%%|*}"
org_id="${project_row##*|}"

user_id=$(db_query <<<"SELECT id FROM users LIMIT 1" 2>/dev/null || true)
if [ -z "$user_id" ]; then
  echo "Error: no users in DB — cannot provision dev key." >&2
  exit 1
fi

# Soft-delete any prior dev key for this project so we can stash a new
# plaintext we know.
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
echo ""

# Render the observability plugin using the same generator the publish flow
# uses (server/internal/plugins.GenerateObservabilityPluginPackage), so the
# test harness exercises the real templated hook.sh — no hand-maintained
# stub to drift from prod.
plugin_dir="hooks/.local/plugin-claude"
rm -rf "$plugin_dir"
mkdir -p "$plugin_dir"

echo "Rendering plugin into ${plugin_dir}..."
go run ./server/cmd/dev-observability-plugin \
  --out "$plugin_dir" \
  --platform claude \
  --api-key "$api_key" \
  --project-slug "$project_slug" \
  --server-url "$GRAM_SERVER_URL" \
  --org-name "Gram Local"
echo ""

exec claude --plugin-dir "$plugin_dir" --debug
