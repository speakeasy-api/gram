#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally"
#MISE dir="{{ config_root }}"

#USAGE flag "--local" help="Always use local plugin directory instead of published plugin"
#USAGE flag "--project <slug>" help="Project slug for OTEL session validation (enables blocking)" default="default"

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

  # Enable the session_capture product feature for the org. Without it,
  # persistHook silently drops chat messages (enforcement still works, but no
  # agent sessions are stored and no risk analysis runs), so sessions + risk
  # events never appear locally. Idempotent and race-safe: ON CONFLICT targets
  # the partial unique index (organization_id, feature_name) WHERE deleted IS
  # FALSE, so a concurrent run can't trip a unique-constraint violation, while a
  # previously-disabled (soft-deleted) feature is still re-enabled.
  db_query -v org_id="$org_id" >/dev/null <<<"INSERT INTO organization_features (organization_id, feature_name) VALUES (:'org_id', 'session_capture') ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING"
  echo "Enabled session_capture for org: ${org_id}"

  user_row=$(db_query -v org_id="$org_id" <<<"SELECT u.id, u.email FROM users u JOIN organization_user_relationships our ON our.user_id = u.id WHERE our.organization_id = :'org_id' AND our.deleted_at IS NULL AND u.deleted_at IS NULL ORDER BY u.created_at ASC LIMIT 1" 2>/dev/null || true)
  if [ -z "$user_row" ]; then
    echo "Warning: no active users for org '${org_id}' in DB — skipping API key provisioning and hook attribution."
  else
    user_id="${user_row%%|*}"
    user_email="${GRAM_HOOKS_TEST_USER_EMAIL:-${user_row#*|}}"

    # {{ config_root }} only expands in the #MISE header, not the body, and the
    # shim path must be absolute since identity.sh runs it from Claude's cwd (not
    # this task's). Resolve the repo root from this script's location.
    config_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
    device_agent_shim="${config_root}/local/cmd/gram-hooks-test-identity"
    mkdir -p "$(dirname "$device_agent_shim")"
    cat >"$device_agent_shim" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${GRAM_HOOKS_TEST_RESOLVED_USER_EMAIL:-}"
EOF
    chmod +x "$device_agent_shim"
    export GRAM_HOOKS_TEST_RESOLVED_USER_EMAIL="$user_email"
    export GRAM_DEVICE_AGENT_COMMANDS="$device_agent_shim"
    echo "Hook sessions will be attributed to: ${user_email}"

    # API key names are unique per organization, so clear any prior local
    # fixture for this org before stashing a new plaintext we know.
    db_query -v org_id="$org_id" >/dev/null <<<"UPDATE api_keys SET deleted_at = NOW() WHERE organization_id = :'org_id' AND name = 'dev-hooks-test' AND deleted IS FALSE"

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
    export GRAM_HOOKS_API_KEY="${api_key}"
    export GRAM_HOOKS_PROJECT_SLUG="${project_slug}"
    echo "OTEL configured (key: ${api_key:0:20}...)"
  fi
fi
echo ""

if [ "${usage_local:-}" = "true" ]; then
  plugin_out="$(mktemp -d)"
  hooks_binary="${plugin_out}/speakeasy-hooks"
  echo "Building local hooks binary: ${hooks_binary}"
  go build -o "$hooks_binary" ./hooks/cmd/speakeasy-hooks
  "$hooks_binary" install \
    --provider=claude \
    --dir="${plugin_out}/plugin-claude" \
    --server-url="$GRAM_SERVER_URL" \
    --project="$project_slug" \
    --browser-login \
    --binary="$hooks_binary"
  echo ""
  exec claude --setting-sources project,local --plugin-dir "${plugin_out}/plugin-claude" --debug
elif ! git diff --quiet main -- server/internal/plugins/ server/cmd/export-hook-plugin/; then
  plugin_out="$(mktemp -d)"
  echo "Rendering local plugin into: ${plugin_out}"
  (cd server && go run ./cmd/export-hook-plugin -out "$plugin_out" >/dev/null)
  echo ""
  exec claude --setting-sources project,local --plugin-dir "${plugin_out}/plugin-claude" --debug
else
  echo "No branch changes to the plugin generators vs main — using published plugin"
  echo ""
  exec claude --setting-sources project,local --debug
fi
