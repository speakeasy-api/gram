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

response=$(curl -s -w "\n%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -d @- \
  --max-time 10 \
  "${server_url}/rpc/hooks.claude")

http_code=$(echo "$response" | tail -1)
body=$(echo "$response" | sed '$d')

# Forward the body to stdout so Claude can read PreToolUse decisions from it.
echo "$body"

# Only treat real 2xx/3xx as allow. curl returns 000 on connection failure,
# DNS error, or timeout — that must NOT silently allow the call, otherwise
# blocking policies are bypassed any time the Gram server is unreachable.
if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 400 ]; then
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
