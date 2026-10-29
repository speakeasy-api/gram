#!/usr/bin/env bash
# Experiment: authenticate a GitHub Actions job to a Speakeasy MCP server with
# workload identity federation (RFC 7523 jwt-bearer), with no stored secret.
#
#   1. Ask GitHub's OIDC endpoint for a JWT whose aud is the server's issuer.
#   2. Exchange it at the server's token endpoint for a short-lived session.
#   3. Use the session for MCP initialize + tools/list.
#
# Run inside a GitHub Actions job with `permissions: id-token: write`. To debug
# the exchange locally, set GITHUB_OIDC_TOKEN to a token minted for the same
# audience (it expires in minutes). Tokens are never printed.
set -euo pipefail

# The shared authorization server from the agent's setup screen. No resource:
# the session it returns is issuer-wide, valid on every MCP server attached to
# the issuer, each of which checks the agent's access on use.
ISSUER="https://id.speakeasy.com/oauth/usi/01a179ec-ee09-79d8-859a-a148f5cbb4bf"
TOKEN_ENDPOINT="${GRAM_TOKEN_ENDPOINT:-$ISSUER/token}"
AUDIENCE="${GRAM_AUDIENCE:-$ISSUER}"
RESOURCE="${GRAM_RESOURCE:-}"
MCP_URL="${GRAM_MCP_URL:-https://ai.speakeasy.com/mcp/clickhouse}"

die() { echo "FAIL: $*" >&2; exit 1; }
mask() { [ -n "${GITHUB_ACTIONS:-}" ] && echo "::add-mask::$1"; return 0; }
command -v jq >/dev/null || die "jq is required"

# Decodes a JWT's payload (base64url) for display; never prints the token.
jwt_claims() {
	local p
	p="$(printf '%s' "$1" | cut -d. -f2 | tr '_-' '/+')"
	while [ $(( ${#p} % 4 )) -ne 0 ]; do p="$p="; done
	printf '%s' "$p" | base64 -d 2>/dev/null
}

echo "== 1. GitHub OIDC token (aud=$AUDIENCE)"
if [ -n "${GITHUB_OIDC_TOKEN:-}" ]; then
	jwt="$GITHUB_OIDC_TOKEN"
else
	[ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] && [ -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] ||
		die "not in a GitHub Actions job with id-token: write (and GITHUB_OIDC_TOKEN unset)"
	jwt="$(curl -fsS -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
		"$ACTIONS_ID_TOKEN_REQUEST_URL&audience=$(jq -rn --arg a "$AUDIENCE" '$a|@uri')" | jq -r .value)"
fi
[ -n "$jwt" ] && [ "$jwt" != null ] || die "GitHub returned no token"
mask "$jwt"
jwt_claims "$jwt" | jq -c '{iss, sub, aud, exp, jti_present: (has("jti"))}'

echo "== 2. Exchange at $TOKEN_ENDPOINT"
resp="$(mktemp)"
resource_arg=()
[ -n "$RESOURCE" ] && resource_arg=(--data-urlencode "resource=$RESOURCE")
code="$(curl -sS -o "$resp" -w '%{http_code}' -X POST "$TOKEN_ENDPOINT" \
	-H 'accept: application/json' \
	--data-urlencode 'grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer' \
	--data-urlencode "assertion=$jwt" \
	"${resource_arg[@]}")"
if [ "$code" != 200 ]; then
	echo "HTTP $code: $(jq -c '{error, error_description}' "$resp" 2>/dev/null || head -c 300 "$resp")"
	rm -f "$resp"
	die "token exchange refused"
fi
session="$(jq -r .access_token "$resp")"
mask "$session"
echo "HTTP 200: $(jq -c '{token_type, expires_in, scope, has_refresh_token: (has("refresh_token"))}' "$resp")"
rm -f "$resp"

# Posts one JSON-RPC message. Sets MCP_LAST_CODE, MCP_LAST_SESSION and
# MCP_LAST_BODY (the JSON body, SSE "data:" lines unwrapped). Call it directly,
# not inside $(...): a command substitution is a subshell and drops the globals.
mcp_post() {
	local body="$1" headers out code
	headers="$(mktemp)"; out="$(mktemp)"
	code="$(curl -sS -D "$headers" -o "$out" -w '%{http_code}' -X POST "$MCP_URL" \
		-H "Authorization: Bearer $session" \
		-H 'content-type: application/json' \
		-H 'accept: application/json, text/event-stream' \
		-H 'mcp-protocol-version: 2025-06-18' \
		${MCP_SESSION_ID:+-H "mcp-session-id: $MCP_SESSION_ID"} \
		-d "$body")"
	MCP_LAST_CODE="$code"
	MCP_LAST_SESSION="$(grep -i '^mcp-session-id:' "$headers" | cut -d: -f2- | tr -d ' \r' || true)"
	if grep -qi '^content-type: *text/event-stream' "$headers"; then
		MCP_LAST_BODY="$(sed -n 's/^data: *//p' "$out")"
	else
		MCP_LAST_BODY="$(cat "$out")"
	fi
	rm -f "$headers" "$out"
}

echo "== 3. MCP at $MCP_URL"
mcp_post '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"wif-github-experiment","version":"0"}}}'
init="$MCP_LAST_BODY"
[ "$MCP_LAST_CODE" = 200 ] || die "initialize HTTP $MCP_LAST_CODE: $(printf '%s' "$init" | head -c 300)"
MCP_SESSION_ID="$MCP_LAST_SESSION"
echo "initialize: $(printf '%s' "$init" | jq -c '.result.serverInfo // .error')"
mcp_post '{"jsonrpc":"2.0","method":"notifications/initialized"}'
mcp_post '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
tools="$MCP_LAST_BODY"
[ "$MCP_LAST_CODE" = 200 ] || die "tools/list HTTP $MCP_LAST_CODE: $(printf '%s' "$tools" | head -c 300)"
printf '%s' "$tools" | jq -c '{tool_count: ((.result.tools // []) | length), first_tools: [(.result.tools // [])[:5][].name], error}'
echo "OK: GitHub workload reached $MCP_URL with no stored secret"
