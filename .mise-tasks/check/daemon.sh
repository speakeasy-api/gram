#!/usr/bin/env bash

#MISE description="Check a pitchfork daemon is up and running"
#MISE hide=true
#MISE quiet=true
#USAGE flag "--name <name>" required=#true help="The name of the daemon"

set -euo pipefail

# Most probe targets below are plain-HTTP-only regardless of GRAM_HTTP_SCHEME:
# the Go/Python control servers, dev-idp, and mock-oidc never enable TLS. Only
# the dashboard (Vite) serves TLS when GRAM_SSL_* is configured.
case "$usage_name" in
  dev-idp)
    mise run check:http --url "http://localhost:$GRAM_DEVIDP_PORT/healthz"
    ;;
  dev-idp-dashboard)
    # The app 307-redirects / to /home, so probe /home directly (check:http only accepts 200).
    mise run check:http --url "http://localhost:$GRAM_DEVIDP_DASHBOARD_PORT/home"
    ;;
  mock-oidc)
    mise run check:http --url "http://localhost:$GRAM_ADMIN_OIDC_EMULATOR_PORT/healthz"
    ;;
  server)
    mise run check:http --url "http://localhost:$GRAM_CONTROL_PORT/healthz"
    ;;
  admin)
    mise run check:http --url "http://localhost:$GRAM_ADMIN_CONTROL_PORT/healthz"
    ;;
  worker)
    mise run check:http --url "http://localhost:$GRAM_WORKER_CONTROL_PORT/healthz"
    ;;
  streams)
    mise run check:http --url "http://localhost:$GRAM_STREAMS_CONTROL_PORT/healthz"
    ;;
  pystreams-multi)
    mise run check:http --url "http://localhost:$GRAM_PYSTREAMS_CONTROL_PORT/readyz"
    ;;
  dashboard)
    mise run check:http --url "$GRAM_HTTP_SCHEME://localhost:$GRAM_SITE_PORT/"
    ;;
  assistant-runtime)
    echo "no readiness probe: ${usage_name} streams logs and does not open a port."
    ;;
  tunnel-gateway)
    mise run check:http --url "http://localhost:$TUNNEL_GATEWAY_PUBLIC_PORT/healthz"
    ;;
  tunnel-postgres-mcp)
    echo "no readiness probe: ${usage_name} port 9000 is only reachable inside the Docker network."
    ;;
  *)
    echo "error: unknown daemon name: $usage_name" >&2
    exit 1
    ;;
esac
