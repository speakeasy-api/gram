#!/usr/bin/env bash
# Dev helper for the AGE-2879 branch (remote-MCP header sourcing).
#
# It runs a local MCP upstream and a curl driver so you can prove which of the
# three header sources actually reaches the upstream:
#   1. static value          - configured on the header
#   2. inbound request header - value_from_request_header (pass-through)
#   3. attached environment   - empty-value header, name-matched to an env entry
#
# The upstream logs every header it received to stdout, so leave `upstream`
# running in its own terminal and watch it while you `call`. The upstream
# returns clean MCP JSON-RPC responses (initialize / tools/list / tools/call).
#
# Usage:
#   local/test-remote-mcp-headers.sh upstream            # start echo upstream (blocks)
#   local/test-remote-mcp-headers.sh call [curl args...] # POST initialize, show forwarded headers
#   local/test-remote-mcp-headers.sh help
#
# Config (env vars, with defaults):
#   PORT       9977                     upstream listen port
#   GRAM_URL   https://localhost:8080   Gram dev server base URL
#   GRAM_SLUG  (required for `call`)    mcp endpoint slug, e.g. speakeasy-f192963c
#   GRAM_KEY   (optional)               API key sent as `Authorization: Bearer`
set -euo pipefail

PORT="${PORT:-9977}"
GRAM_URL="${GRAM_URL:-https://localhost:8080}"

cmd_upstream() {
  echo "echo MCP upstream listening on http://localhost:${PORT}  (point your Gram remote MCP server here)" >&2
  PORT="$PORT" exec python3 - <<'PY'
import json, os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.environ["PORT"])

def log_headers(method):
    import datetime
    ts = datetime.datetime.now().strftime("%H:%M:%S")
    print(f"\n--- {ts}  POST /  method={method!r}", flush=True)

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        pass  # we do our own, prettier logging below

    def _log(self, method):
        log_headers(method)
        for k, v in self.headers.items():
            print(f"  {k}: {v}", flush=True)

    def _send(self, status, payload):
        body = json.dumps(payload).encode() if payload is not None else b""
        self.send_response(status)
        if body:
            self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_GET(self):
        self._log("GET")
        # MCP clients probe with GET; answer politely.
        self._send(200, {"jsonrpc": "2.0", "id": None, "result": {}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0") or "0")
        raw = self.rfile.read(length) if length else b""
        try:
            req = json.loads(raw or "{}")
        except json.JSONDecodeError:
            req = {}
        method = req.get("method", "")
        req_id = req.get("id")

        # Headers go to stdout only; the response body stays clean MCP.
        self._log(method)

        if req_id is None and method.startswith("notifications/"):
            self._send(202, None)
            return

        if method == "initialize":
            result = {
                "protocolVersion": "2025-03-26",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "mcp-echo", "version": "1.0"},
            }
        elif method == "tools/list":
            result = {
                "tools": [{
                    "name": "ping",
                    "description": "Returns pong",
                    "inputSchema": {"type": "object", "properties": {}},
                }],
            }
        elif method == "tools/call":
            result = {"content": [{"type": "text", "text": "pong"}], "isError": False}
        else:
            result = {}

        self._send(200, {"jsonrpc": "2.0", "id": req_id, "result": result})

ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
}

cmd_call() {
  : "${GRAM_SLUG:?set GRAM_SLUG to the mcp endpoint slug (e.g. speakeasy-f192963c)}"
  local args=(
    -sk -X POST "${GRAM_URL}/mcp/${GRAM_SLUG}"
    -H 'content-type: application/json'
    -H 'accept: application/json, text/event-stream'
  )
  if [[ -n "${GRAM_KEY:-}" ]]; then
    args+=(-H "authorization: Bearer ${GRAM_KEY}")
  fi
  # Any extra args (e.g. -H 'X-Passthrough-In: hello') are appended to curl,
  # letting you exercise the inbound/pass-through source.
  args+=("$@")
  args+=(-d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}')

  curl "${args[@]}"
  echo
  echo "--- watch the \`upstream\` terminal for the headers Gram forwarded ---" >&2
}

cmd_help() {
  sed -n '2,60p' "$0"
  cat >&2 <<EOF

Typical flow
------------
1. Start the upstream (leave it running in its own terminal):
     local/test-remote-mcp-headers.sh upstream

2. In the Gram dashboard, point a Remote MCP server at http://localhost:${PORT}
   and add one header per source you want to test:
     - static:   Value = "static-secret"           (header name e.g. X-Static-Token)
     - inbound:  "Value from request header" = X-Passthrough-In
     - env:      leave Value + request-header BLANK, non-secret; then attach an
                 Environment whose entry name maps to the header name.
                 Env name -> header name is Title-Case with '-':
                   X_UPSTREAM_API_KEY  ->  X-Upstream-Api-Key
   Note the endpoint slug Gram assigns (e.g. speakeasy-f192963c).

3. Call it and read result.receivedHeaders:
     export GRAM_SLUG=speakeasy-f192963c
     export GRAM_KEY=gram_local_...                 # if the endpoint is private
     # static + env headers (no inbound):
     local/test-remote-mcp-headers.sh call
     # add an inbound value for the pass-through header:
     local/test-remote-mcp-headers.sh call -H 'X-Passthrough-In: hello-from-client'
EOF
}

case "${1:-help}" in
  upstream|up|serve) shift || true; cmd_upstream ;;
  call)              shift || true; cmd_call "$@" ;;
  help|-h|--help)    cmd_help ;;
  *) echo "unknown command: ${1:-}" >&2; cmd_help; exit 2 ;;
esac
