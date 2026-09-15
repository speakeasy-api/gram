#!/usr/bin/env bash

#MISE description="Fill local agent_events with believable activity by driving real Claude Code sessions as seeded demo users"
#MISE dir="{{ config_root }}"

#USAGE flag "--project <slug>" help="Project whose organization's seeded users to drive" default="default"
#USAGE flag "--prompts <n>" help="Prompts per user" default="10"
#USAGE flag "--minutes <n>" help="Give up after this many minutes, whichever comes first" default="15"
#USAGE flag "--model <model>" help="Model to drive. The cheapest one is the default: this is volume, not quality" default="haiku"

set -euo pipefail

# Local development only. Explore, the agent sessions page and everything else
# over agent_events are dull on a fresh machine: one developer, one surface,
# one bar on every chart. This drives real Claude Code sessions — real tools,
# real tokens, real hook and MCP traffic — and attributes each one to a
# different seeded user, so the breakdowns have something to break down.
#
# Attribution is the whole trick. Claude Code reports user.email from the
# signed-in Anthropic account and offers no override, so every session would
# otherwise land as whoever is at the keyboard. It does honour
# OTEL_RESOURCE_ATTRIBUTES, so each run is marked there and the shared
# collector rewrites that onto the attribute Gram reads. See
# local/otel/gram-demo-forward.yaml for the pipeline that does it.

project_slug="${usage_project}"
prompts_per_user="${usage_prompts}"
minutes="${usage_minutes}"
model="${usage_model}"

collector_logs_endpoint="http://localhost:${OTLP_HTTP_PORT}/v1/logs"

db_query() {
  docker exec -i "${COMPOSE_PROJECT_NAME:-gram}-gram-db-1" psql -U gram -d gram -tA -v ON_ERROR_STOP=1 "$@"
}

fail() {
  echo "demo:agent-activity: $1" >&2
  exit 1
}

command -v claude >/dev/null 2>&1 || fail "claude is not on PATH; this task drives the real CLI"

# The rewrite happens in the shared collector, so a run with it down would
# silently attribute every session to the real account instead. Better to stop.
curl -sf -o /dev/null -X POST "$collector_logs_endpoint" \
  -H 'Content-Type: application/json' -d '{"resourceLogs":[]}' 2>/dev/null ||
  fail "no OTLP collector on ${collector_logs_endpoint} — run \`mise run infra:start\` first"

curl -skf -o /dev/null "${GRAM_SERVER_URL}/health" 2>/dev/null ||
  curl -sk -o /dev/null -w '%{http_code}' --max-time 5 "${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs" 2>/dev/null | grep -qE '^[0-9]' ||
  fail "the Gram server is not answering on ${GRAM_SERVER_URL} — run \`mise run start\` first"

project_row=$(db_query -v slug="$project_slug" <<<"SELECT id, organization_id FROM projects WHERE slug = :'slug' AND deleted IS FALSE ORDER BY created_at LIMIT 1" 2>/dev/null || true)
[ -n "$project_row" ] || fail "project '${project_slug}' not found — run \`mise run seed\` first"
project_id="${project_row%%|*}"
org_id="${project_row##*|}"

# Same two features hooks:test enables: without them ingest drops the rows on
# the floor and the whole run produces nothing.
db_query -v org_id="$org_id" >/dev/null <<<"INSERT INTO organization_features (organization_id, feature_name) VALUES (:'org_id', 'session_capture'), (:'org_id', 'logs') ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING"

# The seeded local identities, which `gram demo-seed --local` writes with this
# domain. Real users of the org (you) are deliberately excluded: the point is
# to generate activity that is obviously synthetic.
# Read into an array the long way: mapfile is bash 4, and macOS ships 3.2.
users=()
while IFS= read -r seeded_email; do
  [ -n "$seeded_email" ] && users+=("$seeded_email")
done < <(db_query -v org_id="$org_id" <<<"SELECT u.email FROM users u JOIN organization_user_relationships our ON our.user_id = u.id WHERE our.organization_id = :'org_id' AND our.deleted_at IS NULL AND u.deleted_at IS NULL AND u.email LIKE '%@local.getgram.ai' ORDER BY u.created_at")
[ "${#users[@]}" -gt 0 ] || fail "no seeded @local.getgram.ai users in this organization — run \`mise run seed\` first"

# One key for the whole run, not one per session: minting per session would
# revoke the previous one mid-flight. Named apart from hooks:test's fixture so
# the two tasks do not revoke each other.
db_query -v org_id="$org_id" >/dev/null <<<"UPDATE api_keys SET deleted_at = NOW() WHERE organization_id = :'org_id' AND name = 'dev-demo-activity' AND deleted IS FALSE"
token_hex=$(openssl rand -hex 32)
api_key="gram_local_${token_hex}"
key_hash=$(printf '%s' "$api_key" | shasum -a 256 | awk '{print $1}')
creator=$(db_query -v org_id="$org_id" <<<"SELECT u.id FROM users u JOIN organization_user_relationships our ON our.user_id = u.id WHERE our.organization_id = :'org_id' AND our.deleted_at IS NULL ORDER BY u.created_at LIMIT 1")
db_query -v org_id="$org_id" -v project_id="$project_id" -v user_id="$creator" -v key_prefix="gram_local_${token_hex:0:5}" -v key_hash="$key_hash" >/dev/null <<<"INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes) VALUES (:'org_id', :'project_id', :'user_id', 'dev-demo-activity', :'key_prefix', :'key_hash', '{hooks}')"

# Rendering the hook plugin makes the session emit hook_registered and the
# hook execution pair, which is a whole class of event the run would miss.
plugin_out="$(mktemp -d)"
trap 'rm -rf "$plugin_out"' EXIT
echo "Rendering the hook plugin…"
(cd server && go run ./cmd/export-hook-plugin -out "$plugin_out" >/dev/null)

# A spread of work rather than a spread of wording: each of these lands a
# different shape in agent_events — bare turns with no tool at all, the
# built-in tools, an MCP call, a skill, a subagent, and a failure so `status`
# has something other than ok in it.
prompts=(
  "In one sentence, what is a columnar database good at?"
  "Use the Bash tool to print the current date, then tell me what day it is."
  "Use Glob to list the markdown files at the top of this repo and name two."
  "Use the Bash tool to read /tmp/definitely-not-here-4821 and tell me what happened."
  "Use Grep to count how many times the word migration appears in CLAUDE.md."
  "Call the whoami tool on the assistants-dev MCP server and say which project it reports."
  "Use the Read tool on the first 10 lines of go.mod and tell me the module path."
  "Use the Agent tool to have a subagent summarize what the mise.toml env section configures, in two sentences."
  "Invoke the postgresql skill and tell me in one sentence what it says about migrations."
  "Use the Bash tool to run: ls /nope-not-here, then explain the exit code."
)

deadline=$(( $(date +%s) + minutes * 60 ))
sessions=0
turns_done=0
total=$(( ${#users[@]} * prompts_per_user ))

echo ""
echo "Driving ${#users[@]} seeded users × ${prompts_per_user} prompts (${total} sessions) on ${model}, stopping after ${minutes}m."
echo "Attributing through the shared collector at ${collector_logs_endpoint}."
echo ""

# Round index outermost so every user makes progress before anyone finishes:
# when the deadline cuts the run short, the result is still a spread of users
# rather than the first two having done everything.
for (( round = 0; round < prompts_per_user; round++ )); do
  for (( u = 0; u < ${#users[@]}; u++ )); do
    now=$(date +%s)
    if [ "$now" -ge "$deadline" ]; then
      echo ""
      echo "Reached the ${minutes}m limit."
      break 2
    fi

    email="${users[$u]}"
    # Offset per user so two users never run the same prompt in the same
    # round, and each works through the whole variety over the run.
    prompt="${prompts[$(( (round + u) % ${#prompts[@]} ))]}"
    turns_done=$(( turns_done + 1 ))
    printf '[%2d/%d] %-28s %s\n' "$turns_done" "$total" "${email%@*}" "${prompt:0:56}…"

    # Appended, not replaced: mise.toml already sets worktree here and the
    # transform needs the marker alongside it, not instead of it.
    resource_attrs="${OTEL_RESOURCE_ATTRIBUTES:+${OTEL_RESOURCE_ATTRIBUTES},}gram.demo.user_email=${email}"

    # Logs only. Metrics would land in agent_metrics carrying the real
    # account's identity, since only the log pipeline does the rewrite.
    CLAUDE_CODE_ENABLE_TELEMETRY=1 \
    OTEL_LOGS_EXPORTER=otlp \
    OTEL_METRICS_EXPORTER=none \
    OTEL_EXPORTER_OTLP_PROTOCOL=http/json \
    OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="$collector_logs_endpoint" \
    OTEL_EXPORTER_OTLP_LOGS_HEADERS="gram-key=${api_key},gram-project=${project_slug}" \
    OTEL_RESOURCE_ATTRIBUTES="$resource_attrs" \
    OTEL_LOG_USER_PROMPTS=1 \
    OTEL_LOG_ASSISTANT_RESPONSES=1 \
    OTEL_LOG_TOOL_DETAILS=1 \
    OTEL_LOGS_EXPORT_INTERVAL=1000 \
    GRAM_HOOKS_SERVER_URL="$GRAM_SERVER_URL" \
    GRAM_HOOKS_SITE_URL="$GRAM_SITE_URL" \
    GRAM_HOOKS_API_KEY="$api_key" \
    GRAM_HOOKS_PROJECT_SLUG="$project_slug" \
      claude --model "$model" \
        --setting-sources project,local \
        --plugin-dir "${plugin_out}/plugin-claude" \
        --permission-mode bypassPermissions \
        -p "$prompt" >/dev/null 2>&1 || echo "        (that turn failed; carrying on)"

    sessions=$(( sessions + 1 ))
  done
done

echo ""
echo "Done: ${sessions} sessions across ${#users[@]} users."
echo "The collector batches, so give it a few seconds before looking."
echo "The sessions land wherever this branch reads agent telemetry: the"
echo "observability pages today, and Explore once agent_events ships."
