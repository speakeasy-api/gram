#!/usr/bin/env bash

#MISE description="Check a pitchfork daemon is up and running"
#MISE hide=true
#MISE quiet=true
#USAGE flag "--name <name>" required=#true help="The name of the daemon"

set -euo pipefail

# Wildcard listeners accept loopback connections; explicit hosts must be probed directly.
probe_address() {
  local address="$1"
  case "$address" in
    :*|0.0.0.0:*|\[::\]:*) printf 'localhost:%s' "${address##*:}" ;;
    *) printf '%s' "$address" ;;
  esac
}

# Most probe targets below are plain-HTTP-only regardless of GRAM_HTTP_SCHEME:
# the Go/Python control servers and mock-oidc never enable TLS. The Vite dev
# servers (dashboard, admin-dashboard) and dev-idp's public listener serve TLS
# when GRAM_SSL_* is configured.
case "$usage_name" in
  dev-idp)
    mise run check:http --url "http://$(probe_address "${GRAM_DEVIDP_CONTROL_ADDRESS:-localhost:$GRAM_DEVIDP_CONTROL_PORT}")/healthz"
    ;;
  dev-idp-dashboard)
    # The app 307-redirects / to /home, so probe /home directly (check:http only accepts 200).
    mise run check:http --url "http://localhost:$GRAM_DEVIDP_DASHBOARD_PORT/home"
    ;;
  mock-oidc)
    mise run check:http --url "http://$(probe_address "${MOCK_OIDC_ADDRESS:-localhost:$GRAM_ADMIN_OIDC_EMULATOR_PORT}")/healthz"
    ;;
  server)
    mise run check:http --url "http://$(probe_address "${GRAM_CONTROL_ADDRESS:-localhost:$GRAM_CONTROL_PORT}")/healthz"
    ;;
  admin)
    mise run check:http --url "http://$(probe_address "${GRAM_ADMIN_CONTROL_ADDRESS:-localhost:$GRAM_ADMIN_CONTROL_PORT}")/healthz"
    ;;
  admin-dashboard)
    mise run check:http --url "$GRAM_HTTP_SCHEME://localhost:$GRAM_ADMIN_DASHBOARD_PORT/"
    ;;
  worker)
    mise run check:http --url "http://$(probe_address "${GRAM_WORKER_CONTROL_ADDRESS:-localhost:$GRAM_WORKER_CONTROL_PORT}")/healthz"
    ;;
  streams)
    mise run check:http --url "http://$(probe_address "${GRAM_STREAMS_CONTROL_ADDRESS:-localhost:$GRAM_STREAMS_CONTROL_PORT}")/healthz"
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
  *)
    echo "error: unknown daemon name: $usage_name" >&2
    exit 1
    ;;
esac
