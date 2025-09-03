#!/usr/bin/env bash

#MISE description="Setup a Gram HTTPS certificate for local development."
#MISE hide=true

set -euo pipefail

CADDY_APP_DIR=

fail() { echo "$1" >&2; exit 1; }

check_caddy() {
  local msg;
  msg="🚨 'caddy' not found in PATH. Did you run 'mise install'?"

  command -v caddy >/dev/null 2>&1 || fail "$msg";
}

set_caddy_app_dir() {
  check_caddy ;
  CADDY_APP_DIR="$(
    caddy environ |
    awk -F= '/AppDataDir/{print $2"/pki/authorities/local/"}'
  )"

  [ -n "$CADDY_APP_DIR" ] || fail "Could not determine Caddy AppDataDir"
}

caddy_already_init() {
  [ -f "${CADDY_APP_DIR}root.crt" ]
}

ask_for_user_trust() {
  cat <<'MSG'

🔒 Installing local HTTPS certificate into System Certificate Trust Settings.
   Enter password to continue.
   This is a one-time action per machine.

MSG

}

trust_caddy_cert() {
  local caddy_pid
  local rootcrt="${CADDY_APP_DIR}root.crt"

  ask_for_user_trust ;

  # 1) start caddy (keep it running for trust)
  caddy run --config Caddyfile >/dev/null 2>&1 &
  caddy_pid=$! ;
  trap 'kill "$caddy_pid" >/dev/null 2>&1 || true' EXIT ;

  # 2) wait up to 2s for root.crt to appear
  local tries ;
  tries=0 ;
  while [ ! -f "$rootcrt" ] && [ $tries -lt 4 ] ; do
    sleep 0.5
    tries=$((tries+1))
  done
  [ -f "$rootcrt" ] || fail "Timed out waiting for $rootcrt"

  # 3) trust the CA
  sleep 2 && sudo caddy trust --config Caddyfile 2>/dev/null;

  # 4) stop caddy now that trust is installed
  kill "$caddy_pid" >/dev/null 2>&1 || true
  wait "$caddy_pid" >/dev/null 2>&1 || true
  trap - EXIT
}

main() {
  set_caddy_app_dir ;
  if caddy_already_init ; then
    exit 0 ;
  else
    trust_caddy_cert ;
    echo -e '\n✅ Certificate is trusted. Ready for local development.' ;
  fi
}

main "$@" ;
