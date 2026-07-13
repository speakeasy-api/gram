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



run_privileged() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    return 1
  fi
}

install_certutil() {
  # Best-effort install of certutil (NSS tools) using the available package manager.
  if command -v apt-get >/dev/null 2>&1; then
    run_privileged apt-get update -qq || true
    run_privileged apt-get install -y -qq libnss3-tools
  elif command -v dnf >/dev/null 2>&1; then
    run_privileged dnf install -y nss-tools
  elif command -v yum >/dev/null 2>&1; then
    run_privileged yum install -y nss-tools
  elif command -v pacman >/dev/null 2>&1; then
    run_privileged pacman -S --noconfirm nss
  elif command -v zypper >/dev/null 2>&1; then
    run_privileged zypper install -y mozilla-nss-tools
  elif command -v apk >/dev/null 2>&1; then
    run_privileged apk add --no-cache nss-tools
  else
    return 1
  fi
}

browser_trusts_local_ca() {
  # True when the mkcert root CA is already registered in the browser NSS store.
  # mkcert installs it under the nickname "mkcert development CA <serial>".
  command -v certutil >/dev/null 2>&1 || return 1
  local nssdb="$HOME/.pki/nssdb"
  { [ -e "$nssdb/cert9.db" ] || [ -e "$nssdb/cert8.db" ]; } || return 1
  certutil -L -d "sql:$nssdb" 2>/dev/null | grep -q "mkcert development CA"
}

configure_linux_browser_trust() {
  # Chromium/Chrome and Firefox on Linux validate certificates against the NSS
  # database (~/.pki/nssdb), not the system trust store. `mkcert -install`
  # silently skips this store when `certutil` is missing, leaving browsers to
  # distrust the local cert. Ensure certutil and the NSS db exist so the
  # subsequent `mkcert -install` can register the root CA for browsers.
  [ "$(uname -s)" = "Linux" ] || return 0

  # Only run the browser-trust setup if the CA isn't already trusted.
  if browser_trusts_local_ca; then
    echo "Local CA already trusted by browsers (NSS store): skipping browser trust setup." >&2
    return 0
  fi

  if ! command -v certutil >/dev/null 2>&1; then
    echo "certutil (NSS tools) not found: attempting to install for browser trust..." >&2
    if ! install_certutil >/dev/null 2>&1 || ! command -v certutil >/dev/null 2>&1; then
      echo "WARN: could not install certutil automatically." >&2
      echo "      Browsers (Chrome/Chromium/Firefox) may still distrust the local cert." >&2
      echo "      Install it manually, then re-run './zero' or 'mise run zero:tls':" >&2
      echo "        Debian/Ubuntu: sudo apt-get install libnss3-tools" >&2
      echo "        Fedora/RHEL:   sudo dnf install nss-tools" >&2
      echo "        Arch:          sudo pacman -S nss" >&2
      echo "        Alpine:        sudo apk add nss-tools" >&2
      return 0
    fi
  fi

  # mkcert only installs into NSS databases that already exist; create the
  # shared user db if it's missing so the CA gets registered for browsers.
  nssdb="$HOME/.pki/nssdb"
  if [ ! -e "$nssdb/cert9.db" ] && [ ! -e "$nssdb/cert8.db" ]; then
    ensure_dir "$nssdb"
    certutil -d "sql:$nssdb" -N --empty-password >/dev/null 2>&1 \
      || echo "WARN: failed to initialize NSS database at $nssdb" >&2
  fi
}

found_keypair() {
  [ -f "$CERT_PATH" ] && [ -f "$KEY_PATH" ]
}

cert_names() {
  local site_host server_host
  site_host="$(extract_hostname "$GRAM_SITE_URL")"
  server_host="$(extract_hostname "$GRAM_SERVER_URL")"

  # host.docker.internal lets local assistant runtime containers dial the
  # server over TLS through Docker's host gateway alias.
  printf "%s\n" \
    "$site_host" \
    "$server_host" \
    "localhost" \
    "host.docker.internal" \
    "127.0.0.1" \
    "::1" \
    | awk 'NF && !seen[$0]++'
}

gen_keypair() {
  ensure_dir "$(dirname "$CERT_PATH")"
  ensure_dir "$(dirname "$KEY_PATH")"

  names=()
  while IFS= read -r name; do
    names+=("$name")
  done < <(cert_names)

  echo "Generating cert and key for ${names[*]}..."
  mkcert \
    -cert-file "$CERT_PATH" -key-file "$KEY_PATH" "${names[@]}"

  root_ca="$(mkcert -CAROOT)/rootCA.pem"
  if [ ! -f "$root_ca" ]; then
    echo "WARN: root CA not found at $root_ca" >&2
  else
    mise set --file mise.local.toml NODE_EXTRA_CA_CERTS="$root_ca"
    # Mounted into local assistant runtime containers so runners trust the
    # local server certificate.
    mise set --file mise.local.toml GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE="$root_ca"
  fi

  echo "  cert => $CERT_PATH"
  echo "  key  => $KEY_PATH"
}

main() {
  if ! detect_tls; then turn_off_tls && exit 0; fi

  check_command mkcert
  turn_on_tls
  configure_linux_browser_trust
  trust_local_ca
  gen_keypair
}

main "$@"
