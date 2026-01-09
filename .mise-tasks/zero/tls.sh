#!/usr/bin/env bash
set -euo pipefail

# MISE description='Install SSL certs for local development.'
# MISE hide=true

ROOT_DIR="$(git rev-parse --show-toplevel)"
CERT_PATH=${CERT_PATH:-"$ROOT_DIR"/local/ssl/certs/local.pem}
KEY_PATH=${KEY_PATH:-"$ROOT_DIR"/local/ssl/keys/local-key.pem}

extract_hostname() {
  local url="$1"
  # Remove protocol
  url="${url#*://}"
  # Remove everything after the first slash (path)
  url="${url%%/*}"
  # Remove port number
  url="${url%%:*}"
  echo "$url"
}

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
  printf "\t%s: %s\n\t%s: %s\n\t%s: %s\n" \
    GRAM_SERVER_URL "$GRAM_SERVER_URL" \
    GRAM_SITE_URL "$GRAM_SITE_URL" \
    VITE_GRAM_ELEMENTS_STORYBOOK_URL "$VITE_GRAM_ELEMENTS_STORYBOOK_URL" >&2;
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
  echo "Trusting local certificate." >&2

  # Run mkcert -install and capture output for diagnostics (do not swallow it)
  if output="$(mkcert -install 2>&1)"; then
    echo "Local certificate is trusted." >&2

    # If we're running under WSL, warn that Windows browsers may still show 'Not secure'
    if grep -qi microsoft /proc/version 2>/dev/null; then
      echo "" >&2
      echo "NOTE: Detected WSL environment." >&2
      echo "mkcert installed the CA in the Linux trust store. If you open this site with a Windows browser (Edge/Chrome), the browser may still show 'Not secure'." >&2
      echo "Quick recommendation: for fast local development, switch to HTTP to avoid browser certificate warnings:" >&2
      echo "  mise set --file mise.local.toml GRAM_SERVER_URL=http://localhost:8080" >&2
      echo "  mise set --file mise.local.toml GRAM_SITE_URL=http://localhost:5173" >&2
      echo "  mise set --file mise.local.toml VITE_GRAM_ELEMENTS_STORYBOOK_URL=http://localhost:6006" >&2
      echo "Then re-run ./zero and use http://localhost:5173 in your browser." >&2
      echo "" >&2
    fi

    return 0
  else
    echo "ERROR: mkcert -install failed." >&2
    echo "" >&2
    echo "mkcert output (for debugging):" >&2
    echo "$output" >&2
    echo "" >&2
    exit 1
  fi
}



found_keypair() {
  [ -f "$CERT_PATH" ] && [ -f "$KEY_PATH" ]
}

gen_keypair() {
  ensure_dir "$(dirname "$CERT_PATH")"
  ensure_dir "$(dirname "$KEY_PATH")"

  hostname=$(extract_hostname "$GRAM_SITE_URL")

  echo "Generating cert and key for $hostname..."
  mkcert \
    -cert-file "$CERT_PATH" -key-file "$KEY_PATH" "$hostname"

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
