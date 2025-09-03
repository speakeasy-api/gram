#!/usr/bin/env bash

#MISE description="Run an HTTPS reverse proxy for local development."

set -euo pipefail

CADDY_FILE="$(git rev-parse --show-toplevel 2>/dev/null)/Caddyfile";

fail() {
  echo "$1" && exit 1;
}

check_caddy() {
  if ! command -v caddy >/dev/null 2>&1; then
    fail "🚨 'caddy' not found in PATH. Did you run mise install?" ;
  fi

  if [[ ! -f "$CADDY_FILE" ]]; then
    fail "❌ No Caddyfile found at $CADDY_FILE" ;
  fi
}

run_proxy() {
  caddy run --config Caddyfile ;
}

check_caddy ;
run_proxy ;
