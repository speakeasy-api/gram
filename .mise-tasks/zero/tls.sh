#!/usr/bin/env bash
set -euo pipefail

# MISE description='Install SSL certs for local development.'
# MISE hide=true

ROOT_DIR="$(git rev-parse --show-toplevel)"
CERT_PATH=${CERT_PATH:-"$ROOT_DIR"/local/ssl/certs/local.pem}
KEY_PATH=${KEY_PATH:-"$ROOT_DIR"/local/ssl/keys/local-key.pem}

check_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: $1 not found on PATH" >&2
    exit 1
  fi
}

ensure_dir() {
  local dir
  dir="$1"
  if [ -n "$dir" ] && [ ! -d "$dir" ]; then
    mkdir -p "$dir"
  fi
}

trust_local_ca() {
  echo "Trusting local certificate."
  mkcert -install >/dev/null 2>&1
  echo "Local certificate is trusted."
}

gen_keypair() {
  echo "Generating cert and key..."
  mkcert \
    -cert-file "$CERT_PATH" -key-file "$KEY_PATH" localhost 127.0.0.1 ::1

  echo "  cert => $CERT_PATH"
  echo "  key  => $KEY_PATH"
}

main() {
  check_command mkcert

  trust_local_ca
  ensure_dir "$(dirname "$CERT_PATH")"
  ensure_dir "$(dirname "$KEY_PATH")"

  if [ ! -f "$CERT_PATH" ] || [ ! -f "$KEY_PATH" ]; then
    gen_keypair
  fi
}

main "$@"
