#!/usr/bin/env bash

#MISE description="Open the Pitchfork Web UI"

set -e

# Pitchfork does not expose the web UI address anywhere queryable (no CLI
# command, state file or IPC request carries it). The supervisor also
# auto-bumps to a nearby free port when web.bind_port is taken, so the
# configured port cannot be trusted either. The only record of the actual
# bound address is the supervisor's "Web UI listening on http://..." log line.

if ! pitchfork supervisor status >/dev/null 2>&1; then
  echo "error: pitchfork supervisor is not running (try: pitchfork supervisor start)" >&2
  exit 1
fi

state_dir="${PITCHFORK_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/pitchfork}"
log_file="${PITCHFORK_LOGS_DIR:-$state_dir/logs}/pitchfork/pitchfork.log"

url=""
if [[ -f "$log_file" ]]; then
  url="$(grep -o 'Web UI listening on http://[^ ]*' "$log_file" | tail -n1 | sed 's/^Web UI listening on //')"
fi

if [[ -z "$url" ]]; then
  echo "error: could not find the web UI address in $log_file" >&2
  echo "hint: enable it with 'pitchfork settings set web.auto_start true' then run 'pitchfork supervisor start --force'" >&2
  exit 1
fi

if ! curl -fsS -o /dev/null --max-time 2 "$url"; then
  echo "error: web UI at $url is not responding; restart the supervisor with 'pitchfork supervisor start --force'" >&2
  exit 1
fi

exec mise run open:_thing "$url"
