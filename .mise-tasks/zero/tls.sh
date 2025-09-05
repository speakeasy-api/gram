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

is_https() {
  case "$1" in
    https://*) return 0 ;;
    *) return 1 ;;
  esac
}

detect_tls() {
  is_https "$GRAM_SERVER_URL" || is_https "$GRAM_SITE_URL"
}

show_urls() {
  printf "\t%s: %s\n\t%s: %s\n" \
    GRAM_SERVER_URL "$GRAM_SERVER_URL" \
    GRAM_SITE_URL "$GRAM_SITE_URL" >&2;
}

turn_off_tls() {
  echo "No https endpoints: skipping TLS"
  show_urls

  mise set --file mise.local.toml GRAM_SSL_CERT_FILE=
  mise set --file mise.local.toml GRAM_SSL_KEY_FILE=
}

turn_on_tls() {
  echo "Found https endpoints: enabling TLS"
  show_urls

  mise set --file mise.local.toml GRAM_SSL_CERT_FILE="{{config_root}}/local/ssl/certs/local.pem"
  mise set --file mise.local.toml GRAM_SSL_KEY_FILE="{{config_root}}/local/ssl/keys/local-key.pem"

}

trust_local_ca() {
  echo "Trusting local certificate."
  mkcert -install >/dev/null 2>&1
  echo "Local certificate is trusted."
}

found_keypair() {
  [ -f "$CERT_PATH" ] && [ -f "$KEY_PATH" ]
}

gen_keypair() {
  ensure_dir "$(dirname "$CERT_PATH")"
  ensure_dir "$(dirname "$KEY_PATH")"

  echo "Generating cert and key..."
  mkcert \
    -cert-file "$CERT_PATH" -key-file "$KEY_PATH" localhost 127.0.0.1 ::1

  echo "  cert => $CERT_PATH"
  echo "  key  => $KEY_PATH"
}

main() {
  if ! detect_tls; then turn_off_tls && exit 0; fi

  check_command mkcert
  turn_on_tls
  trust_local_ca
  if ! found_keypair; then gen_keypair; fi
}

main "$@"
