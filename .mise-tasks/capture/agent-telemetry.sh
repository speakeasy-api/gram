#!/usr/bin/env bash

#MISE description="Capture agent telemetry: poll provider admin APIs in parallel with an agent session (interactive or headless), then dump anonymized NDJSON fixtures"
#MISE dir="{{ config_root }}"

#USAGE flag "--agent <agent>" default="claude" help="Agent to drive and capture: claude"
#USAGE flag "--project <slug>" default="default" help="Project slug rows are attributed to and dumped from"
#USAGE flag "--api-key <key>" env="GRAM_CAPTURE_API_KEY" help="Provider admin API key for the poll leg (claude: Anthropic admin key); empty skips polling"
#USAGE flag "--external-org-id <id>" env="GRAM_CAPTURE_EXTERNAL_ORG_ID" help="Provider-side organization ID, stamped on polled rows when set"
#USAGE flag "--lookback <duration>" default="168h" help="Poll and dump window as a Go duration (168h = 7 days)"
#USAGE flag "--out <dir>" default="local/agent-telemetry" help="Output directory for the NDJSON dump"
#USAGE flag "--prompt <text>" help="Drive the session headless with this single prompt instead of interactively"
#USAGE flag "--prompts-file <file>" help="Drive one headless session per non-empty line of this file"
#USAGE flag "--no-session" help="Skip the agent session; poll (if keyed) and dump only"
#USAGE flag "--raw" help="Disable anonymization in the dump"

set -euo pipefail

agent="${usage_agent:-claude}"
project="${usage_project:-default}"
api_key="${usage_api_key:-}"
external_org_id="${usage_external_org_id:-}"
lookback="${usage_lookback:-168h}"
out_dir="${usage_out:-local/agent-telemetry}"

if [ "$agent" != "claude" ]; then
  echo "capture:agent-telemetry: --agent must be claude, got '${agent}'" >&2
  exit 2
fi

# Headless prompts: --prompt gives one, --prompts-file one per non-empty line.
prompts=()
if [ -n "${usage_prompt:-}" ]; then
  prompts+=("$usage_prompt")
fi
if [ -n "${usage_prompts_file:-}" ]; then
  if [ ! -f "$usage_prompts_file" ]; then
    echo "capture:agent-telemetry: prompts file not found: ${usage_prompts_file}" >&2
    exit 2
  fi
  while IFS= read -r line; do
    if [ -n "$line" ]; then
      prompts+=("$line")
    fi
  done <"$usage_prompts_file"
fi
if [ "${#prompts[@]}" -gt 0 ] && [ "${usage_no_session:-false}" = "true" ]; then
  echo "capture:agent-telemetry: --prompt/--prompts-file conflict with --no-session" >&2
  exit 2
fi

capture_cmd=(go run ./server/cmd/capture-agent-telemetry --agent "$agent" --project "$project" --lookback "$lookback" --out "$out_dir")
if [ -n "$external_org_id" ]; then
  capture_cmd+=(--external-org-id "$external_org_id")
fi

# Leg 1: provider poll, in the background. Runs while the session is live so
# the historical admin-API backfill and the fresh session telemetry land
# together. --dump=false: the export happens once, after the session, so it
# includes the session's rows.
poll_pid=""
poll_log=""
if [ -n "$api_key" ]; then
  poll_log="$(mktemp -t capture-agent-telemetry-poll)"
  echo "Starting provider poll in background (log: ${poll_log})"
  "${capture_cmd[@]}" --api-key "$api_key" --dump=false >"$poll_log" 2>&1 &
  poll_pid=$!
else
  echo "No API key supplied — skipping the provider poll leg."
fi

# Don't leave the poller running if the session leg (or the user) aborts.
cleanup() {
  if [ -n "$poll_pid" ] && kill -0 "$poll_pid" 2>/dev/null; then
    kill "$poll_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Leg 2: agent session(s) with dev hooks + OTel wired to the local server
# (mise hooks:test). Sessions push OTLP live, so the server must be up.
# Headless when prompts were given, interactive otherwise.
if [ "${usage_no_session:-false}" != "true" ]; then
  if ! curl -s -o /dev/null --max-time 3 "${GRAM_SERVER_URL}"; then
    echo "capture:agent-telemetry: local server not reachable at ${GRAM_SERVER_URL} — run 'mise run start' first." >&2
    exit 1
  fi
  if [ "${#prompts[@]}" -gt 0 ]; then
    i=0
    for prompt in "${prompts[@]}"; do
      i=$((i + 1))
      echo "Running headless ${agent} session ${i}/${#prompts[@]}..."
      mise run hooks:test --agent "$agent" --project "$project" --print "$prompt"
      echo ""
    done
  else
    echo "Launching ${agent} session (exit the session to finish the capture)..."
    echo ""
    mise run hooks:test --agent "$agent" --project "$project"
  fi
fi

# Join the poll leg before dumping so its rows are in ClickHouse.
poll_failed=false
if [ -n "$poll_pid" ]; then
  if ! wait "$poll_pid"; then
    poll_failed=true
  fi
  poll_pid=""
  echo ""
  echo "--- provider poll output ---"
  cat "$poll_log"
  echo "----------------------------"
fi

# Final phase: dump everything captured in the window. No API key: the poll
# already ran (or was skipped), this pass only exports.
dump_cmd=("${capture_cmd[@]}")
if [ "${usage_raw:-false}" = "true" ]; then
  dump_cmd+=(--anonymize=false)
fi
"${dump_cmd[@]}"

echo ""
echo "Capture written to ${out_dir} (see manifest.json for the row breakdown)."
if [ "$poll_failed" = "true" ]; then
  echo "WARNING: the provider poll leg failed — the dump only contains session/pre-existing rows. See the poll output above." >&2
  exit 1
fi
