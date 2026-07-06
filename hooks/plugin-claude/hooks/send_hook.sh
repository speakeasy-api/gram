#!/usr/bin/env bash

# Send a Claude Code hook event to Gram. The server is the sole authority on
# whether to block:
#   HTTP 2xx -> allow (exit 0). The JSON body is forwarded to Claude as-is;
#               for PreToolUse, Claude reads `hookSpecificOutput.permissionDecision`
#               from that body to honor any deny decision the server made.
#   HTTP 4xx/5xx -> block (exit 2). The server's `message` is relayed to
#                   stderr so Claude renders it as the block reason.
# We do not parse the body to derive the exit code — the script never makes
# the allow/deny decision, only the server does.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$script_dir/identity.sh" ]; then
  . "$script_dir/identity.sh"
fi
. "$script_dir/http.sh"

api_key="${GRAM_HOOKS_API_KEY:-${GRAM_API_KEY:-}}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"
gram_hooks_org_hint="${GRAM_HOOKS_ORG_ID:-}"
gram_hooks_cached_auth=""

gram_hooks_env_key_source() {
  if [ -n "${GRAM_HOOKS_API_KEY:-}" ]; then
    printf 'GRAM_HOOKS_API_KEY'
    return 0
  fi
  if [ -n "${GRAM_API_KEY:-}" ]; then
    printf 'GRAM_API_KEY'
    return 0
  fi
  return 1
}

gram_hooks_env_key_rejected_message() {
  local source
  source="$(gram_hooks_env_key_source)" || return 1
  printf 'Speakeasy hooks rejected the API key configured in %s. Update or unset %s, then run hooks/login.sh to reconnect hooks.' "$source" "$source"
}

gram_hooks_json_escape_string() {
  if command -v python3 >/dev/null 2>&1; then
    if python3 -c 'import json, sys; print(json.dumps(sys.stdin.read())[1:-1], end="")'; then
      return
    fi
  fi
  local s
  s="$(cat)"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/}"
  printf '%s' "$s"
}

gram_hooks_emit_login_nudge() {
  local context escaped
  context="Notice from the Speakeasy observability plugin installed in this workspace: this machine needs to reconnect before telemetry can be recorded. Handle the user's current request as normal, then directly ask whether to reconnect Speakeasy hooks now. If they agree, run: bash \"${script_dir}/login.sh\". Never run it without their explicit consent."
  escaped="$(printf '%s' "$context" | gram_hooks_json_escape_string)"
  printf '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"%s"}}\n' "$escaped"
}

gram_hooks_top_level_string_value() {
  local key="$1"
  local payload="$2"
  if ! command -v awk >/dev/null 2>&1; then
    return 1
  fi
  printf '%s' "$payload" | awk -v want="$key" '
BEGIN { depth = 0; in_string = 0; escape = 0; state = "key"; token = ""; current_key = "" }
{
  s = s $0 "\n"
}
END {
  for (i = 1; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (in_string) {
      if (escape) {
        token = token c
        escape = 0
      } else if (c == "\\") {
        escape = 1
      } else if (c == "\"") {
        in_string = 0
        if (depth == 1 && state == "key") {
          current_key = token
          state = "colon"
        } else if (depth == 1 && state == "value") {
          if (current_key == want) {
            print token
            exit 0
          }
          state = "after_value"
        }
      } else {
        token = token c
      }
      continue
    }
    if (c == "\"") {
      in_string = 1
      escape = 0
      token = ""
      continue
    }
    if (c == "{" || c == "[") {
      depth++
      continue
    }
    if (c == "}" || c == "]") {
      depth--
      continue
    }
    if (depth == 1 && state == "colon" && c == ":") {
      state = "value"
      continue
    }
    if (depth == 1 && c == ",") {
      state = "key"
      current_key = ""
      continue
    }
  }
  exit 1
}'
}

gram_hooks_is_user_prompt_submit() {
  local payload="$1"
  if command -v python3 >/dev/null 2>&1; then
    local event
    event="$(printf '%s' "$payload" | python3 -c "
import json, sys
try:
    data = json.loads(sys.stdin.read())
    print(data.get('hook_event_name') or data.get('event_name') or data.get('event') or '', end='')
except Exception:
    pass
" 2>/dev/null)" || true
    [ "$event" = "UserPromptSubmit" ] && return 0
  fi
  local event
  event="$(gram_hooks_top_level_string_value hook_event_name "$payload" ||
    gram_hooks_top_level_string_value event_name "$payload" ||
    gram_hooks_top_level_string_value event "$payload")" || true
  [ "$event" = "UserPromptSubmit" ] && return 0
  return 1
}

gram_hooks_body_has_auth_failure_signal() {
  local body="$1"
  if command -v python3 >/dev/null 2>&1; then
    local auth_failed
    auth_failed="$(printf '%s' "$body" | python3 -c "
import json, sys
try:
    data = json.loads(sys.stdin.read())
    print(
        '1'
        if data.get('pluginAuthFailed') is True
        or str(data.get('systemMessage') or '').startswith('Speakeasy hooks rejected plugin auth.')
        else '',
        end='',
    )
except Exception:
    pass
" 2>/dev/null)" || true
    [ "$auth_failed" = "1" ] && return 0
  fi
  case "$body" in
    *'"pluginAuthFailed":true'* | *'"systemMessage":"Speakeasy hooks rejected plugin auth.'*) return 0 ;;
  esac
  return 1
}

gram_hooks_body_has_block_decision() {
  local body="$1"
  if command -v python3 >/dev/null 2>&1; then
    local blocked
    blocked="$(printf '%s' "$body" | python3 -c "
import json, sys
try:
    data = json.loads(sys.stdin.read())
    hook_output = data.get('hookSpecificOutput')
    print(
        '1'
        if data.get('decision') == 'block'
        or (isinstance(hook_output, dict) and hook_output.get('permissionDecision') == 'deny')
        else '',
        end='',
    )
except Exception:
    pass
" 2>/dev/null)" || true
    [ "$blocked" = "1" ] && return 0
  fi
  case "$body" in
    *'"decision":"block"'* | *'"permissionDecision":"deny"'*) return 0 ;;
  esac
  return 1
}

gram_hooks_body_without_auth_failure_signal() {
  local body="$1"
  if command -v python3 >/dev/null 2>&1; then
    local cleaned
    cleaned="$(printf '%s' "$body" | python3 -c "
import json, sys
try:
    data = json.loads(sys.stdin.read())
    if isinstance(data, dict):
        data.pop('pluginAuthFailed', None)
    print(json.dumps(data, separators=(',', ':')), end='')
except Exception:
    sys.exit(1)
" 2>/dev/null)" && {
      printf '%s\n' "$cleaned"
      return 0
    }
  fi
  printf '%s\n' "$body" |
    sed -e 's/,"pluginAuthFailed":true//g' -e 's/"pluginAuthFailed":true,//g'
}

# No env key: fall back to the credentials the browser login flow cached
# (auth_preflight.sh / login.sh) so authenticated policy enforcement still
# applies. auth.sh installs an EXIT trap when sourced; the trap set below
# deliberately replaces it and covers the only resource created here (the
# curl auth config).
if [ -z "$api_key" ] && [ -f "$script_dir/auth.sh" ]; then
  # shellcheck source=/dev/null
  if . "$script_dir/auth.sh" 2>/dev/null && type gram_hooks_read_auth >/dev/null 2>&1; then
    if gram_hooks_read_auth "$server_url" 2>/dev/null; then
      api_key="$GRAM_HOOKS_CACHED_API_KEY"
      [ -n "$project_slug" ] || project_slug="$GRAM_HOOKS_CACHED_PROJECT"
      gram_hooks_cached_auth=1
    fi
  fi
fi

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

if [ -z "$api_key" ] &&
  type gram_hooks_reauth_needed >/dev/null 2>&1 &&
  gram_hooks_reauth_needed; then
  if gram_hooks_is_user_prompt_submit "$payload"; then
    gram_hooks_emit_login_nudge
    exit 0
  fi
  echo "Speakeasy hooks need to reconnect. Run hooks/login.sh to reconnect hooks." >&2
  exit 2
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi
auth_config=""
auth_config_arg=()
cleanup_auth_config() {
  if [ -n "$auth_config" ]; then
    rm -f "$auth_config"
  fi
}
trap cleanup_auth_config EXIT
if [ -n "$api_key" ] || [ -n "$project_slug" ]; then
  if ! auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX"); then
    # Fail closed (exit 2) like every other failure path, but say why —
    # otherwise Claude shows a blocked tool call with an empty reason.
    echo "Speakeasy hooks: could not create a temporary auth file on this machine, so the tool call was blocked. Check that ${TMPDIR:-/tmp} is writable." >&2
    exit 2
  fi
  chmod 600 "$auth_config" || true
  # curl config quoted strings treat backslash and double quote specially, and
  # the config file is line-oriented; escape the metacharacters and strip CR/LF
  # so a corrupted value cannot break out of the directive or inject additional
  # config lines.
  api_key="${api_key//\\/\\\\}"
  api_key="${api_key//\"/\\\"}"
  api_key="${api_key//$'\n'/}"
  api_key="${api_key//$'\r'/}"
  project_slug="${project_slug//\\/\\\\}"
  project_slug="${project_slug//\"/\\\"}"
  project_slug="${project_slug//$'\n'/}"
  project_slug="${project_slug//$'\r'/}"
  if [ -n "$api_key" ]; then
    printf 'header = "Gram-Key: %s"\n' "$api_key" >>"$auth_config"
  fi
  if [ -n "$project_slug" ]; then
    printf 'header = "Gram-Project: %s"\n' "$project_slug" >>"$auth_config"
  fi
  auth_config_arg=(--config "$auth_config")
fi

# Retries transient resets (see http.sh) so a single reset no longer blocks
# the tool call; the server still decides allow/block from the HTTP code.
gram_http_post "${server_url}/rpc/hooks.claude" "$payload" 10 \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"} \
  ${auth_config_arg[@]+"${auth_config_arg[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null &&
  gram_hooks_body_has_auth_failure_signal "$body"; then
  env_reason=""
  if env_reason="$(gram_hooks_env_key_rejected_message)"; then
    :
  fi
  if [ -n "$gram_hooks_cached_auth" ] &&
    type gram_hooks_forget_auth >/dev/null 2>&1; then
    gram_hooks_forget_auth
    if type gram_hooks_mark_reauth_needed >/dev/null 2>&1; then
      gram_hooks_mark_reauth_needed
    fi
  fi
  if gram_hooks_body_has_block_decision "$body"; then
    if [ -n "$env_reason" ]; then
      echo "$env_reason" >&2
    fi
    gram_hooks_body_without_auth_failure_signal "$body"
    exit 0
  fi
  if [ -n "$env_reason" ]; then
    echo "$env_reason" >&2
    exit 2
  fi
  if gram_hooks_is_user_prompt_submit "$payload"; then
    gram_hooks_emit_login_nudge
    exit 0
  fi
  echo "Speakeasy hooks need to reconnect. Run hooks/login.sh to reconnect hooks." >&2
  exit 2
fi

if { [ "$http_code" = "401" ] || [ "$http_code" = "403" ]; } &&
  [ -n "$gram_hooks_cached_auth" ] &&
  [ -z "${GRAM_HOOKS_API_KEY:-${GRAM_API_KEY:-}}" ] &&
  type gram_hooks_forget_auth >/dev/null 2>&1; then
  gram_hooks_forget_auth
  if type gram_hooks_mark_reauth_needed >/dev/null 2>&1; then
    gram_hooks_mark_reauth_needed
  fi
  if gram_hooks_is_user_prompt_submit "$payload"; then
    gram_hooks_emit_login_nudge
    exit 0
  fi
fi

if { [ "$http_code" = "401" ] || [ "$http_code" = "403" ]; } &&
  env_reason="$(gram_hooks_env_key_rejected_message)"; then
  echo "$env_reason" >&2
  exit 2
fi

# Only treat a real 2xx as allow. curl returns 000 on connection failure, DNS
# error, or timeout, and a 3xx (e.g. an http->https redirect, which curl does
# not follow here) carries no decision body — neither must silently allow the
# call, otherwise blocking policies are bypassed when the server is unreachable
# or misconfigured. The 2>/dev/null guards keep a non-numeric code from leaking
# a shell error before we fall through to the block path below.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  # Forward the body to stdout so Claude can read PreToolUse decisions from it.
  echo "$body"
  exit 0
fi

# Best-effort: extract the server's `message` (already self-branded as
# "Speakeasy blocked this prompt: ...") so Claude shows it to the user.
# Falls back to a generic line if python3 isn't on PATH or the body isn't
# parseable, so the script still blocks correctly on minimal systems.
reason=""
if command -v python3 >/dev/null 2>&1; then
  reason=$(printf '%s' "$body" | python3 -c "
import json, sys
try:
    print(json.loads(sys.stdin.read()).get('message', ''), end='')
except Exception:
    pass
" 2>/dev/null) || true
fi

echo "${reason:-Speakeasy hook returned HTTP ${http_code}}" >&2
exit 2
