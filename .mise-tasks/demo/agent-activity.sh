#!/usr/bin/env bash

#MISE description="Fill local agent_events with believable activity by driving real agent sessions as seeded demo users"
#MISE dir="{{ config_root }}"

#USAGE flag "--project <slug>" help="Project whose organization's seeded users to drive" default="default"
#USAGE flag "--prompts <n>" help="Prompts per user" default="10"
#USAGE flag "--minutes <n>" help="Give up after this many minutes, whichever comes first" default="15"
#USAGE flag "--harnesses <list>" help="Comma-separated harnesses to deal the users across: claude, codex" default="claude,codex"
#USAGE flag "--claude-model <model>" help="Model Claude Code drives. The cheapest one is the default: this is volume, not quality" default="haiku"
#USAGE flag "--codex-model <model>" help="Model Codex drives. The cheap one a ChatGPT account can reach is the default. Codex is dropped from the run if it will not resolve, rather than silently escalating; pass an empty string to accept the machine's configured model at whatever it costs" default="gpt-5.6-luna"

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
#
# Every prompt below is read-only work, and both harnesses are held to that --
# Codex by its read-only sandbox, Claude Code by an allowlist of the tools the
# prompts actually name. An instruction injected through a file, a plugin or an
# MCP response cannot write to the checkout this runs in.

project_slug="${usage_project}"
prompts_per_user="${usage_prompts}"
minutes="${usage_minutes}"
claude_model="${usage_claude_model}"
codex_model="${usage_codex_model}"

collector_logs_endpoint="http://localhost:${OTLP_HTTP_PORT}/v1/logs"

# One turn's share of the budget is capped here as well as by the deadline, so
# a single session that wedges cannot swallow the whole run before the loop
# gets to look at the clock again.
max_turn_seconds=240

# Codex takes its OTel settings from a config file rather than the
# environment, and that file carries the run's API key. It is written into
# CODEX_HOME because that is where Codex looks for a named profile, mode 600,
# and removed again on the way out.
codex_profile_name="gram-demo-activity"
codex_profile_file="${CODEX_HOME:-$HOME/.codex}/${codex_profile_name}.config.toml"
codex_profile_marker="# Written by mise run demo:agent-activity; removed when it exits."

plugin_out=""
turn_log=""
org_id=""
codex_profile_owned=false
current_turn_pid=""
current_watchdog_pid=""
codex_dropped=false
codex_consecutive_failures=0

db_query() {
  docker exec -i "${COMPOSE_PROJECT_NAME:-gram}-gram-db-1" psql -U gram -d gram -tA -v ON_ERROR_STOP=1 "$@"
}

fail() {
  echo "demo:agent-activity: $1" >&2
  exit 1
}

# Depth-first, because an agent is not one process. `pkill -P` reaps the
# subshell's direct child and leaves its grandchildren running, reparented and
# still working -- which on an interrupt means a turn carrying on against a key
# this script has already revoked. Children first, so the parent cannot spawn
# more while its own tree is being taken down.
kill_tree() {
  local pid="$1" child
  [ -n "$pid" ] || return 0
  for child in $(pgrep -P "$pid" 2>/dev/null); do
    kill_tree "$child"
  done
  kill "$pid" 2>/dev/null || true
}

# The key is minted for this run and must not outlive it. Revoking at the top
# of the next run is no help on a machine where this run was the last one, and
# no help at all if the developer interrupts this one.
cleanup() {
  local rc=$?
  # An interrupt reaches this trap with a turn still running. Take the agent
  # down first: otherwise it carries on working with a key this trap is about
  # to revoke and a config file it is about to delete.
  if [ -n "$current_turn_pid" ]; then
    kill_tree "$current_turn_pid"
  fi
  if [ -n "$current_watchdog_pid" ]; then
    kill_tree "$current_watchdog_pid"
  fi
  # Spelled out rather than `[ ... ] && ...`: a false test there returns
  # non-zero, and under `set -e` that would leave the trap before it got to
  # the revocation, which is the one step that matters.
  if [ -n "$plugin_out" ]; then
    rm -rf "$plugin_out"
  fi
  if [ -n "$turn_log" ]; then
    rm -f "$turn_log"
  fi
  # Only ever the file this run wrote. Removing it unconditionally would
  # delete a developer's own profile of the same name -- including, when the
  # name is already taken, the very file write_codex_profile refused to touch.
  if $codex_profile_owned; then
    rm -f "$codex_profile_file"
  fi
  if [ -n "$org_id" ]; then
    db_query -v org_id="$org_id" >/dev/null 2>&1 <<<"UPDATE api_keys SET deleted_at = NOW() WHERE organization_id = :'org_id' AND name = 'dev-demo-activity' AND deleted IS FALSE" || true
  fi
  exit "$rc"
}
trap cleanup EXIT INT TERM

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
# each harness's own vocabulary; neither is a translation of the other. All of
# it is reading, which is what lets both harnesses run without write access.
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

# Codex is configured through config.toml rather than the environment. A named
# profile is layered over the developer's own config, which keeps their
# provider, auth and model working while replacing the whole `[otel]` block
# rather than merging into it — their block routinely points at a real Gram
# project with a live key, and none of it survives this. The credential lives
# here rather than in a -c override so it stays out of the process arguments,
# where any local user could read it off `ps` for the length of the run.
write_codex_profile() {
  # A Codex that has been installed but never run has no CODEX_HOME yet.
  mkdir -p "$(dirname "$codex_profile_file")"
  if [ -e "$codex_profile_file" ] && ! head -1 "$codex_profile_file" | grep -qF "$codex_profile_marker"; then
    fail "${codex_profile_file} already exists and was not written by this task — move it aside first"
  fi
  ( umask 077
    cat > "$codex_profile_file" <<TOML
${codex_profile_marker}
# The other half of keeping this cheap: the model is only ever asked for the
# shallowest reasoning it offers. These prompts are one-liners about files and
# dates -- none of them is worth thinking hard about.
model_reasoning_effort = "low"

[otel]
environment = "dev"
log_user_prompt = true
# Logs only. Metrics and traces would carry the real account's identity, since
# only the log pipeline does the rewrite.
trace_exporter = "none"
metrics_exporter = "none"

[otel.exporter.otlp-http]
# Used verbatim as the logs URL: Codex does not append /v1/logs to it.
endpoint = "${collector_logs_endpoint}"
protocol = "json"
headers = { "Gram-Key" = "${api_key}", "Gram-Project" = "${project_slug}" }
TOML
  )
  codex_profile_owned=true
}

# An allowlist rather than `--permission-mode bypassPermissions` with the
# mutating tools disallowed: the bypass wins over a deny list, so that
# combination reads as a restriction and enforces nothing. Listing what the
# prompts need instead pre-approves exactly those and leaves everything else
# to be denied, which in a headless turn happens without a prompt to answer.
claude_allowed_tools="Bash,Read,Glob,Grep,Task,Skill,TodoWrite,mcp__assistants-dev__whoami"

# Logs only, in both harnesses, for the same reason.
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
      --allowed-tools "$claude_allowed_tools" \
      -p "$2" > "$turn_log" 2>&1
}

# The MCP servers are replaced for the same reason the otel block is: a
# developer's own point at production, and a generated prompt asking for a tool
# call has no business reaching one. The local server that replaces them is
# narrowed to the single tool the prompts name, because a read-only filesystem
# sandbox says nothing about what an MCP call can do -- dev-mcp otherwise
# offers assistant and trigger mutation. That matches the allowlist the Claude
# Code path runs under. Everything not named here — provider, auth, model —
# stays theirs.
run_codex() {
  local model_args=()
  if [ -n "$codex_model" ]; then
    model_args=(--model "$codex_model")
  fi

  OTEL_RESOURCE_ATTRIBUTES="$(demo_resource_attrs "$1")" \
    "$codex_bin" exec \
      --profile "$codex_profile_name" \
      --cd "$PWD" \
      --skip-git-repo-check \
      --dangerously-bypass-hook-trust \
      --sandbox read-only \
      ${model_args[@]+"${model_args[@]}"} \
      -c 'approval_policy="never"' \
      -c 'mcp_servers={ "assistants-dev" = { command = "mise", args = ["x", "--", "go", "run", "./server/cmd/dev-mcp"], enabled_tools = ["whoami"] } }' \
      -c 'plugins={}' \
      -- "$2" > "$turn_log" 2>&1
}

# The deadline is only read between turns, so without this a session that
# wedges runs past the advertised limit indefinitely. The watchdog takes the
# turn's whole process tree: killing the subshell alone would leave the agent
# behind, and killing only its direct child would leave that agent's own.
run_with_budget() {
  local budget="$1"
  shift

  # Stderr of the job itself, not of the agent -- that is already captured in
  # the turn log. What this drops is the shell's own notice when the watchdog
  # kills the turn, which prints the terminated command line, and for Claude
  # Code that line carries the run's API key in its environment prefix.
  "$@" 2>/dev/null &
  local turn_pid=$!
  current_turn_pid="$turn_pid"

  ( sleep "$budget"
    kill_tree "$turn_pid" ) &
  local watchdog_pid=$!
  current_watchdog_pid="$watchdog_pid"

  local rc=0
  wait "$turn_pid" || rc=$?
  kill "$watchdog_pid" 2>/dev/null || true
  wait "$watchdog_pid" 2>/dev/null || true
  current_turn_pid=""
  current_watchdog_pid=""
  return "$rc"
}

run_turn() {
  local harness="$1" email="$2" prompt="$3" budget="$4"

  : > "$turn_log"

  case "$harness" in
    claude) run_with_budget "$budget" run_claude "$email" "$prompt" ;;
    codex)
      if run_with_budget "$budget" run_codex "$email" "$prompt"; then
        codex_consecutive_failures=0
        return 0
      fi
      codex_consecutive_failures=$(( codex_consecutive_failures + 1 ))

      # Codex reaching nothing at all is not a turn that failed, it is a
      # harness that cannot run: no credentials, or a provider that was
      # configured and then taken away. Dealt with like the model case rather
      # than letting a third of the run burn down one 401 at a time.
      if grep -qEi '401 unauthorized|missing bearer|invalid_api_key|not (logged in|authenticated)' "$turn_log"; then
        echo "        (codex has no working credentials here, so codex is out for this run."
        echo "         Run \`codex login\`, or put an API key in the environment, then try again.)"
        codex_dropped=true
        return 1
      fi
      # The cheap model is a guess about someone else's Codex setup: it is a
      # deployment name on whatever provider they configured, and on a custom
      # one it will not resolve. What it must not do then is quietly carry on
      # with whatever that machine has configured instead. This task exists to
      # make volume, and the developer asked for the cheapest thing that can
      # produce it -- a frontier model is not a reasonable substitute to pick
      # on their behalf. Detected on that specific failure only, so transient
      # and auth errors are not mistaken for it.
      if [ -n "$codex_model" ] && grep -qEi 'model_not_found|(model|deployment).{0,120}(not found|does not exist|is not supported)' "$turn_log"; then
        echo "        (codex cannot run ${codex_model} here, so codex is out for this run."
        echo "         Pass --codex-model <name> for one that resolves, or --codex-model ''"
        echo "         to accept whatever this machine has configured, at its cost.)"
        codex_dropped=true
        return 1
      fi

      # Whatever else is wrong, three in a row is a broken harness rather than
      # bad luck, and the rest of its turns are better spent elsewhere.
      if [ "$codex_consecutive_failures" -ge 3 ]; then
        echo "        (three codex turns in a row failed, so codex is out for this run."
        echo "         Its last output is the place to look.)"
        codex_dropped=true
      fi
      return 1
      ;;
  esac
}

# endregion: harnesses

curl -sf -o /dev/null -X POST "$collector_logs_endpoint" \
  -H 'Content-Type: application/json' -d '{"resourceLogs":[]}' 2>/dev/null ||
  fail "no OTLP collector on ${collector_logs_endpoint} — run \`mise run infra:start\` first"

# That collector is one container shared by every worktree, and it keeps the
# forwarding endpoint of whichever worktree started it. A run from a different
# tree would be attributed correctly and then delivered to someone else's
# server, so check rather than discover it in the wrong dashboard.
lgtm_container=$(docker compose -f compose.shared.yml -p gram-shared ps -q lgtm 2>/dev/null || true)
if [ -n "$lgtm_container" ]; then
  forwarding_to=$(docker inspect "$lgtm_container" --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null | sed -n 's/^GRAM_DEMO_OTLP_LOGS_ENDPOINT=//p' | head -1)
  if [ -n "$forwarding_to" ] && [ "$forwarding_to" != "${GRAM_DEMO_OTLP_LOGS_ENDPOINT:-}" ]; then
    fail "the shared collector forwards to ${forwarding_to}, which is not this worktree's ${GRAM_DEMO_OTLP_LOGS_ENDPOINT:-<unset>}. It keeps the endpoint of whichever worktree started it — restart it from here with \`mise run infra:start\`."
  fi
fi

curl -skf -o /dev/null "${GRAM_SERVER_URL}/health" 2>/dev/null ||
  curl -sk -o /dev/null -w '%{http_code}' --max-time 5 "${GRAM_SERVER_URL}/rpc/hooks.otel/v1/logs" 2>/dev/null | grep -qE '^[0-9]' ||
  fail "the Gram server is not answering on ${GRAM_SERVER_URL} — run \`mise run start\` first"

# Anything the machine cannot run is dropped rather than fatal: a developer
# with only one of these installed still gets a populated dashboard, just with
# every user on the one harness.
harnesses=()
uses_claude=false
uses_codex=false
for requested in $(echo "${usage_harnesses}" | tr ',' ' '); do
  case "$requested" in
    claude | codex) ;;
    *) fail "unknown harness '${requested}' — supported: claude, codex" ;;
  esac
  if harness_available "$requested"; then
    harnesses+=("$requested")
    [ "$requested" = "claude" ] && uses_claude=true
    [ "$requested" = "codex" ] && uses_codex=true
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
# the two tasks do not revoke each other. Revoked again by the exit trap.
db_query -v org_id="$org_id" >/dev/null <<<"UPDATE api_keys SET deleted_at = NOW() WHERE organization_id = :'org_id' AND name = 'dev-demo-activity' AND deleted IS FALSE"
token_hex=$(openssl rand -hex 32)
api_key="gram_local_${token_hex}"
key_hash=$(printf '%s' "$api_key" | shasum -a 256 | awk '{print $1}')
creator=$(db_query -v org_id="$org_id" <<<"SELECT u.id FROM users u JOIN organization_user_relationships our ON our.user_id = u.id WHERE our.organization_id = :'org_id' AND our.deleted_at IS NULL ORDER BY u.created_at LIMIT 1")
db_query -v org_id="$org_id" -v project_id="$project_id" -v user_id="$creator" -v key_prefix="gram_local_${token_hex:0:5}" -v key_hash="$key_hash" >/dev/null <<<"INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes) VALUES (:'org_id', :'project_id', :'user_id', 'dev-demo-activity', :'key_prefix', :'key_hash', '{hooks}')"

turn_log="$(mktemp)"

# Rendering the hook plugin makes the session emit hook_registered and the
# hook execution pair, which is a whole class of event the run would miss.
# Only Claude Code loads it — export-hook-plugin renders Claude and Cursor
# trees, and Codex is neither, so a Codex-only run skips this entirely.
if $uses_claude; then
  plugin_out="$(mktemp -d)"
  echo "Rendering the hook plugin…"
  (cd server && go run ./cmd/export-hook-plugin -out "$plugin_out" >/dev/null)
fi

if $uses_codex; then
  write_codex_profile
fi

# Who picks up the slack if codex drops out over its model. Empty when codex
# is the only harness, which the loop treats as the end of the run.
standby_harness=""
for harness in "${harnesses[@]}"; do
  if [ "$harness" != "codex" ]; then
    standby_harness="$harness"
    break
  fi
done

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
    # A harness that dropped out mid-run hands its remaining users to whoever
    # is left, so the run still fills the dashboard rather than idling through
    # every turn it had dealt away.
    if [ "$harness" = "codex" ] && $codex_dropped; then
      if [ -z "$standby_harness" ]; then
        echo ""
        echo "No harness left to run. Stopping."
        break 2
      fi
      harness="$standby_harness"
    fi
    # Offset per user so two users never run the same prompt in the same
    # round, and each works through the whole variety over the run.
    prompt="$(prompt_for "$harness" $(( round + u )))"
    turns_done=$(( turns_done + 1 ))
    printf '[%2d/%d] %-6s %-24s %s\n' "$turns_done" "$total" "$harness" "${email%@*}" "${prompt:0:52}…"

    turn_budget=$(( deadline - now ))
    [ "$turn_budget" -gt "$max_turn_seconds" ] && turn_budget=$max_turn_seconds

    run_turn "$harness" "$email" "$prompt" "$turn_budget" || echo "        (that turn failed or ran out of time; carrying on)"

    sessions=$(( sessions + 1 ))
  done
done

echo ""
echo "Done: ${sessions} sessions across ${#users[@]} users."
echo "The collector batches, so give it a few seconds before looking."
echo "The sessions land wherever this branch reads agent telemetry: the"
echo "observability pages today, and Explore once agent_events ships."
