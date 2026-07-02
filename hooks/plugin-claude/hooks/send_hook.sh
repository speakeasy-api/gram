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

api_key="${GRAM_HOOKS_API_KEY:-}"
project_slug="${GRAM_HOOKS_PROJECT_SLUG:-}"

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
    fi
  fi
fi

payload=$(cat)
if type gram_enrich_identity_payload >/dev/null 2>&1; then
  payload=$(gram_enrich_identity_payload "$payload")
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

# Forward the body to stdout so Claude can read PreToolUse decisions from it.
echo "$body"

# Only treat a real 2xx as allow. curl returns 000 on connection failure, DNS
# error, or timeout, and a 3xx (e.g. an http->https redirect, which curl does
# not follow here) carries no decision body — neither must silently allow the
# call, otherwise blocking policies are bypassed when the server is unreachable
# or misconfigured. The 2>/dev/null guards keep a non-numeric code from leaking
# a shell error before we fall through to the block path below.
if [ "$http_code" -ge 200 ] 2>/dev/null && [ "$http_code" -lt 300 ] 2>/dev/null; then
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
