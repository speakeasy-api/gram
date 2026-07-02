#!/usr/bin/env bash
# Send a Cursor hook event to Gram. The server is the sole authority on whether
# to block: it always responds 200 with a JSON body carrying `permission`
# ("allow"/"deny"), `user_message`, and `agent_message`. We relay that body to
# stdout so Cursor can honor the decision.
#
# When the server cannot be reached (timeout, DNS, connection refused) or
# returns a non-2xx status there is no decision to relay. We fail CLOSED — emit
# a deny — so an unreachable Gram server cannot silently bypass blocking
# policies. This matches the Claude hook (plugin-claude/hooks/send_hook.sh).
#
# Set GRAM_HOOKS_DEBUG=1 to print diagnostics to stderr.

set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
# Accept both the hooks-prefixed names (preferred, matches the Claude hook) and
# the legacy GRAM_API_KEY/GRAM_PROJECT_SLUG names for backward compatibility.
api_key="${GRAM_HOOKS_API_KEY:-${GRAM_API_KEY:-}}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-${GRAM_PROJECT_SLUG:-}}"

debug() {
  if [ -n "${GRAM_HOOKS_DEBUG:-}" ]; then
    printf 'gram-hooks(cursor): %s\n' "$1" >&2
  fi
}

# Minimal JSON string encoder (escapes backslash then double quote) so a block
# reason is always valid JSON without depending on python3/jq being present.
json_string() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  printf '"%s"' "$s"
}

# Emit a deny body Cursor honors as a block, with a human-readable reason.
emit_deny() {
  printf '{"permission":"deny","user_message":%s,"agent_message":%s}' \
    "$(json_string "$1")" "$(json_string "$1")"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# No env key: fall back to the credentials the browser login flow cached
# (auth_preflight.sh / login.sh). auth.sh installs an EXIT trap when sourced;
# the trap set later in this script deliberately replaces it and covers the
# only resource created here (the curl auth config).
if [ -z "$api_key" ] && [ -f "$script_dir/auth.sh" ]; then
  # shellcheck source=/dev/null
  if . "$script_dir/auth.sh" 2>/dev/null && type gram_hooks_read_auth >/dev/null 2>&1; then
    if gram_hooks_read_auth "$server_url" 2>/dev/null; then
      api_key="$GRAM_HOOKS_CACHED_API_KEY"
      [ -n "$project_slug" ] || project_slug="$GRAM_HOOKS_CACHED_PROJECT"
    fi
  fi
fi

# Not configured: this is a setup problem, not a policy decision. Allow the
# action (emit no decision) but surface the misconfiguration instead of failing
# silently — the single most common "my hook isn't firing" cause.
if [ -z "$api_key" ] || [ -z "$project_slug" ]; then
  echo '{}'
  echo "gram-hooks(cursor): not sending hook — run the plugin's hooks/login.sh to connect, or set GRAM_HOOKS_API_KEY (or GRAM_API_KEY) and GRAM_HOOKS_PROJECT_SLUG (or GRAM_PROJECT_SLUG)." >&2
  exit 0
fi
if [ -f "$script_dir/identity.sh" ]; then
  . "$script_dir/identity.sh"
fi
. "$script_dir/http.sh"

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
fi

hook_hostname=$(hostname 2>/dev/null || true)
hook_hostname_header=()
if [ -n "$hook_hostname" ]; then
  hook_hostname_header=(-H "X-Gram-Hook-Hostname: ${hook_hostname}")
fi

# Pass credentials via a curl config file (mode 600) rather than -H so the API
# key never appears in the process list.
auth_config=""
cleanup_auth_config() {
  if [ -n "$auth_config" ]; then
    rm -f "$auth_config"
  fi
}
trap cleanup_auth_config EXIT
auth_config=$(mktemp "${TMPDIR:-/tmp}/gram-hooks-curl.XXXXXX") || {
  debug "mktemp failed for curl auth config; failing closed"
  emit_deny "Speakeasy hooks: could not create a temporary auth file on this machine, so the tool call was blocked. Check that ${TMPDIR:-/tmp} is writable."
  exit 0
}
chmod 600 "$auth_config" || true
printf 'header = "Gram-Key: %s"\n' "$api_key" >>"$auth_config"
printf 'header = "Gram-Project: %s"\n' "$project_slug" >>"$auth_config"

# Retry transient resets (see http.sh) so a single reset no longer blocks the
# tool call; the server still decides allow/block from the HTTP code.
gram_http_post "${server_url}/rpc/hooks.cursor" "$payload" 10 \
  --config "$auth_config" \
  ${hook_hostname_header[@]+"${hook_hostname_header[@]}"}

http_code="$GRAM_HTTP_CODE"
body="$GRAM_HTTP_BODY"

# Relay the server's decision verbatim so Cursor can honor allow/deny. Only a
# real 2xx carries a decision; a 3xx (e.g. an unfollowed http->https redirect)
# does not, so it falls through to the fail-closed branch below. The 2>/dev/null
# guards keep a non-numeric code from leaking a shell error.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
  echo "$body"
  exit 0
fi

# No decision available. curl returns 000 on connection failure/timeout/DNS;
# any other non-2xx is a server-side error. Fail closed so an unreachable or
# erroring Gram server cannot silently bypass blocking policies.
if [ "$http_code" = "000" ]; then
  reason="Speakeasy could not reach the Gram server at ${server_url}, so this action was blocked. Check your network connection or GRAM_HOOKS_SERVER_URL."
else
  reason="Speakeasy hook returned HTTP ${http_code}, so this action was blocked. Retry in a moment; if it persists, check the Gram service status."
fi
debug "request failed (http_code=${http_code}); failing closed. body=${body}"
emit_deny "$reason"
exit 0
