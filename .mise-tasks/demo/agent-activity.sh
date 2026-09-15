#!/usr/bin/env bash

#MISE description="Fill local agent_events with believable activity by driving real agent sessions as seeded demo users"
#MISE dir="{{ config_root }}"

#USAGE flag "--project <slug>" help="Project whose organization's seeded users to drive" default="default"
#USAGE flag "--prompts <n>" help="Prompts per user" default="10"
#USAGE flag "--minutes <n>" help="Give up after this many minutes, whichever comes first" default="15"
#USAGE flag "--harnesses <list>" help="Comma-separated harnesses to deal the users across: claude, codex" default="claude,codex"
#USAGE flag "--claude-model <model>" help="Model Claude Code drives. The cheapest one is the default: this is volume, not quality" default="haiku"
#USAGE flag "--codex-model <model>" help="Model Codex drives. Falls back to whatever the machine's Codex config selects if this one will not resolve" default="gpt-5.4-mini"

set -euo pipefail

# Local development only. Explore, the agent sessions page and everything else
# over agent_events are dull on a fresh machine: one developer, one surface,
# one bar on every chart. This drives real agent sessions — real tools, real
# tokens, real hook and MCP traffic — attributes each one to a different
# seeded user, and deals those users across the harnesses we support, so the
# breakdowns have something to break down on both axes.
#
# Attribution is the whole trick. Every harness reports the identity of the
# account signed in at the keyboard and offers no override, so all six users
# would otherwise land as one. Both honour OTEL_RESOURCE_ATTRIBUTES, though,
# so each run is marked there and the shared collector rewrites that onto the
# attribute Gram reads. See local/otel/gram-demo-forward.yaml for the pipeline
# that does it.

project_slug="${usage_project}"
prompts_per_user="${usage_prompts}"
minutes="${usage_minutes}"
claude_model="${usage_claude_model}"
codex_model="${usage_codex_model}"

collector_logs_endpoint="http://localhost:${OTLP_HTTP_PORT}/v1/logs"

db_query() {
  docker exec -i "${COMPOSE_PROJECT_NAME:-gram}-gram-db-1" psql -U gram -d gram -tA -v ON_ERROR_STOP=1 "$@"
}

fail() {
  echo "demo:agent-activity: $1" >&2
  exit 1
}

# region: harnesses
#
# A harness answers three questions: can it run on this machine, what is worth
# asking it, and how does one turn carry the demo identity. Everything below
# dispatches on those three, so supporting another one is three more cases.

codex_bin=""

# The codex binary is not reliably on PATH — the editor extension ships its own
# copy and the standalone install is managed. Same candidates the hooks e2e
# harness walks.
resolve_codex_bin() {
  if [ -n "$codex_bin" ]; then
    return 0
  fi
  if command -v codex >/dev/null 2>&1; then
    codex_bin="$(command -v codex)"
    return 0
  fi
  for candidate in \
    "${CODEX_HOME:-$HOME/.codex}/packages/standalone/current/bin/codex" \
    "$HOME/.local/bin/codex" \
    "/usr/local/bin/codex" \
    "/Applications/ChatGPT.app/Contents/Resources/codex" \
    "/Applications/Codex.app/Contents/Resources/codex"; do
    if [ -x "$candidate" ]; then
      codex_bin="$candidate"
      return 0
    fi
  done
  return 1
}

harness_available() {
  case "$1" in
    claude) command -v claude >/dev/null 2>&1 ;;
    codex) resolve_codex_bin ;;
    *) return 1 ;;
  esac
}

harness_missing_reason() {
  case "$1" in
    claude) echo "the claude CLI is not on PATH" ;;
    codex) echo "no codex binary on PATH or in the usual install locations" ;;
    *) echo "unknown harness" ;;
  esac
}

# A spread of work rather than a spread of wording: each prompt lands a
# different shape in agent_events — bare turns with no tool at all, the
# built-in tools, an MCP call, a skill, a subagent, and a failure so `status`
# has something other than ok in it. The two lists cover the same ground in
# each harness's own vocabulary; neither is a translation of the other.
claude_prompts=(
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

codex_prompts=(
  "In one sentence, what is a write-ahead log for?"
  "Run date and tell me what day it is."
  "List the markdown files at the top of this repo and name two."
  "Read /tmp/definitely-not-here-4821 and tell me what happened."
  "Count how many times the word migration appears in CLAUDE.md."
  "Call the whoami tool on the assistants-dev MCP server and say which project it reports."
  "Read the first 10 lines of go.mod and tell me the module path."
  "Run git log --oneline -3 and summarize each of those commits in one line."
  "Run ls /nope-not-here and explain the exit code."
  "Summarize what the env section of mise.toml configures, in two sentences."
)

prompt_for() {
  case "$1" in
    claude) printf '%s' "${claude_prompts[$(( $2 % ${#claude_prompts[@]} ))]}" ;;
    codex) printf '%s' "${codex_prompts[$(( $2 % ${#codex_prompts[@]} ))]}" ;;
  esac
}

# Appended, not replaced: mise.toml already sets worktree here and the
# transform needs the marker alongside it, not instead of it.
demo_resource_attrs() {
  printf '%s' "${OTEL_RESOURCE_ATTRIBUTES:+${OTEL_RESOURCE_ATTRIBUTES},}gram.demo.user_email=$1"
}

# Logs only, in both harnesses. Metrics would land in agent_metrics carrying
# the real account's identity, since only the log pipeline does the rewrite.
run_claude() {
  CLAUDE_CODE_ENABLE_TELEMETRY=1 \
  OTEL_LOGS_EXPORTER=otlp \
  OTEL_METRICS_EXPORTER=none \
  OTEL_EXPORTER_OTLP_PROTOCOL=http/json \
  OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="$collector_logs_endpoint" \
  OTEL_EXPORTER_OTLP_LOGS_HEADERS="gram-key=${api_key},gram-project=${project_slug}" \
  OTEL_RESOURCE_ATTRIBUTES="$(demo_resource_attrs "$1")" \
  OTEL_LOG_USER_PROMPTS=1 \
  OTEL_LOG_ASSISTANT_RESPONSES=1 \
  OTEL_LOG_TOOL_DETAILS=1 \
  OTEL_LOGS_EXPORT_INTERVAL=1000 \
  GRAM_HOOKS_SERVER_URL="$GRAM_SERVER_URL" \
  GRAM_HOOKS_SITE_URL="$GRAM_SITE_URL" \
  GRAM_HOOKS_API_KEY="$api_key" \
  GRAM_HOOKS_PROJECT_SLUG="$project_slug" \
    claude --model "$claude_model" \
      --setting-sources project,local \
      --plugin-dir "${plugin_out}/plugin-claude" \
      --permission-mode bypassPermissions \
      -p "$2" >/dev/null 2>&1
}

# Codex is configured through config.toml rather than the environment, so the
# settings arrive as -c overrides. Each one replaces the whole subtree it names
# rather than merging into it, which is what makes this safe to run against a
# developer's own config: their otel block routinely points at a real Gram
# project with a live key, and their MCP servers at production. Both are
# replaced outright here so a generated prompt can only reach the local stack.
# Everything we do not name — provider, auth, sandbox policy — stays theirs,
# the same way the Claude Code path uses whatever account is signed in.
run_codex() {
  local model_args=()
  if [ -n "$codex_model" ]; then
    model_args=(--model "$codex_model")
  fi

  OTEL_RESOURCE_ATTRIBUTES="$(demo_resource_attrs "$1")" \
    "$codex_bin" exec \
      --cd "$PWD" \
      --skip-git-repo-check \
      --dangerously-bypass-hook-trust \
      --dangerously-bypass-approvals-and-sandbox \
      ${model_args[@]+"${model_args[@]}"} \
      -c otel.environment=dev \
      -c otel.log_user_prompt=true \
      -c otel.trace_exporter=none \
      -c otel.metrics_exporter=none \
      -c "otel.exporter={ \"otlp-http\" = { endpoint = \"${collector_logs_endpoint}\", protocol = \"json\", headers = { \"Gram-Key\" = \"${api_key}\", \"Gram-Project\" = \"${project_slug}\" } } }" \
      -c 'mcp_servers={ "assistants-dev" = { command = "mise", args = ["x", "--", "go", "run", "./server/cmd/dev-mcp"] } }' \
      -c 'plugins={}' \
      -- "$2" >/dev/null 2>&1
}

run_turn() {
  case "$1" in
    claude) run_claude "$2" "$3" ;;
    codex)
      if run_codex "$2" "$3"; then
        return 0
      fi
      # The cheap model is a guess about someone else's Codex setup: it is a
      # deployment name on whatever provider they configured, and on a custom
      # one it simply will not resolve. Give up the guess on the first failure
      # rather than every turn, and let their own default carry the run.
      if [ -n "$codex_model" ]; then
        echo "        (codex could not run ${codex_model}; falling back to the configured model)"
        codex_model=""
        run_codex "$2" "$3"
        return $?
      fi
      return 1
      ;;
  esac
}

# endregion: harnesses

curl -sf -o /dev/null -X POST "$collector_logs_endpoint" \
  -H 'Content-Type: application/json' -d '{"resourceLogs":[]}' 2>/dev/null ||
  fail "no OTLP collector on ${collector_logs_endpoint} — run \`mise run infra:start\` first"

curl -skf -o /dev/null "${GRAM_SERVER_URL}/health" 2>/dev/null ||
  curl -sk -o /dev/null -w '%{http_code}' --max-time 5 "${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs" 2>/dev/null | grep -qE '^[0-9]' ||
  fail "the Gram server is not answering on ${GRAM_SERVER_URL} — run \`mise run start\` first"

# Anything the machine cannot run is dropped rather than fatal: a developer
# with only one of these installed still gets a populated dashboard, just with
# every user on the one harness.
harnesses=()
uses_claude=false
for requested in $(echo "${usage_harnesses}" | tr ',' ' '); do
  case "$requested" in
    claude | codex) ;;
    *) fail "unknown harness '${requested}' — supported: claude, codex" ;;
  esac
  if harness_available "$requested"; then
    harnesses+=("$requested")
    [ "$requested" = "claude" ] && uses_claude=true
  else
    echo "Skipping ${requested}: $(harness_missing_reason "$requested")."
  fi
done
[ "${#harnesses[@]}" -gt 0 ] || fail "none of the requested harnesses are installed"

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

# Dealt once, not per turn. A person picks a tool and stays with it, so the
# harness breakdown should line up with the user breakdown rather than
# scattering every user across both.
user_harness=()
for (( u = 0; u < ${#users[@]}; u++ )); do
  user_harness+=("${harnesses[$(( u % ${#harnesses[@]} ))]}")
done

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
# Only Claude Code loads it — export-hook-plugin renders Claude and Cursor
# trees, and Codex is neither.
plugin_out=""
if $uses_claude; then
  plugin_out="$(mktemp -d)"
  trap 'rm -rf "$plugin_out"' EXIT
  echo "Rendering the hook plugin…"
  (cd server && go run ./cmd/export-hook-plugin -out "$plugin_out" >/dev/null)
fi

deadline=$(( $(date +%s) + minutes * 60 ))
sessions=0
turns_done=0
total=$(( ${#users[@]} * prompts_per_user ))

deal=""
for harness in "${harnesses[@]}"; do
  count=0
  for (( u = 0; u < ${#users[@]}; u++ )); do
    [ "${user_harness[$u]}" = "$harness" ] && count=$(( count + 1 ))
  done
  deal="${deal:+${deal}, }${count} on ${harness}"
done

echo ""
echo "Driving ${#users[@]} seeded users × ${prompts_per_user} prompts (${total} sessions): ${deal}."
echo "Attributing through the shared collector at ${collector_logs_endpoint}, stopping after ${minutes}m."
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
    harness="${user_harness[$u]}"
    # Offset per user so two users never run the same prompt in the same
    # round, and each works through the whole variety over the run.
    prompt="$(prompt_for "$harness" $(( round + u )))"
    turns_done=$(( turns_done + 1 ))
    printf '[%2d/%d] %-6s %-24s %s\n' "$turns_done" "$total" "$harness" "${email%@*}" "${prompt:0:52}…"

    run_turn "$harness" "$email" "$prompt" || echo "        (that turn failed; carrying on)"

    sessions=$(( sessions + 1 ))
  done
done

echo ""
echo "Done: ${sessions} sessions across ${#users[@]} users and ${#harnesses[@]} harness(es)."
echo "The collector batches, so give it a few seconds before looking."
echo "The sessions land wherever this branch reads agent telemetry: the"
echo "observability pages today, and Explore once agent_events ships."
