#!/usr/bin/env bash

#MISE description="Install SSL certs for local development."
#MISE hide=true
#USAGE flag "--extra-hosts <hosts>" env="GRAM_SSL_EXTRA_HOSTS" default="" help="Comma-separated additional certificate DNS names and IP addresses."

set -euo pipefail

ROOT_DIR="$(git rev-parse --show-toplevel)"
CERT_PATH=${CERT_PATH:-"$ROOT_DIR"/local/ssl/certs/local.pem}
KEY_PATH=${KEY_PATH:-"$ROOT_DIR"/local/ssl/keys/local-key.pem}

extract_hostname() {
  local url="$1"
  # Remove protocol
  url="${url#*://}"
  # Remove everything after the first slash (path)
  url="${url%%/*}"
  # IPv6 authorities carry brackets; certificate IP SANs do not.
  case "$url" in
    \[*\]*)
      url="${url#\[}"
      url="${url%%\]*}"
      ;;
    *) url="${url%%:*}" ;;
  esac
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
    [hH][tT][tT][pP][sS]://*) return 0 ;;
    *) return 1 ;;
  esac
}

validate_devidp_url() {
  [ -n "${GRAM_DEVIDP_EXTERNAL_URL:-}" ] || return 0
  check_command node
  node --input-type=module - "$GRAM_DEVIDP_EXTERNAL_URL" <<'NODE'
import { BlockList, isIP } from "node:net";
const raw = process.argv[2];
let url;
try {
  if (!/^https?:\/\//i.test(raw)) throw new Error("invalid scheme");
  url = new URL(raw);
  if (!url.hostname || url.username || url.password) throw new Error("invalid authority");
} catch {
  console.error("GRAM_DEVIDP_EXTERNAL_URL must be an absolute HTTP(S) URL without credentials.");
  process.exit(1);
}
if (raw.includes("?") || raw.includes("#")) {
  console.error("GRAM_DEVIDP_EXTERNAL_URL must not contain a query or fragment.");
  process.exit(1);
}
const host = url.hostname.replace(/^\[|\]$/g, "");
const loopback = new BlockList();
loopback.addSubnet("127.0.0.0", 8, "ipv4");
loopback.addAddress("::1", "ipv6");
const family = isIP(host);
const isLoopback = host === "localhost" || (family !== 0 && loopback.check(host, family === 4 ? "ipv4" : "ipv6"));
if (url.protocol === "http:" && !isLoopback) {
  console.error("GRAM_DEVIDP_EXTERNAL_URL must use HTTPS unless its host is loopback.");
  process.exit(1);
}
NODE
}

detect_tls() {
  is_https "$GRAM_SERVER_URL" || is_https "$GRAM_SITE_URL" || is_https "${GRAM_DEVIDP_EXTERNAL_URL:-}"
}

show_urls() {
  printf "\t%s: %s\n" \
    GRAM_SERVER_URL "$GRAM_SERVER_URL" \
    GRAM_SITE_URL "$GRAM_SITE_URL" \
    GRAM_DEVIDP_EXTERNAL_URL "${GRAM_DEVIDP_EXTERNAL_URL:-}" >&2
}

turn_off_tls() {
  echo "No https endpoints: skipping TLS"
  show_urls

  mise set --file mise.local.toml GRAM_SSL_CERT_FILE=
  mise set --file mise.local.toml GRAM_SSL_KEY_FILE=
  mise set --file mise.local.toml GRAM_HTTP_SCHEME=http
}

turn_on_tls() {
  echo "Found https endpoints: enabling TLS"
  show_urls

  mise set --file mise.local.toml GRAM_SSL_CERT_FILE="{{config_root}}/local/ssl/certs/local.pem"
  mise set --file mise.local.toml GRAM_SSL_KEY_FILE="{{config_root}}/local/ssl/keys/local-key.pem"
  mise set --file mise.local.toml GRAM_HTTP_SCHEME=https
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
      echo "  mise set --file mise.local.toml GRAM_DEVIDP_EXTERNAL_URL=http://localhost:${GRAM_DEVIDP_PORT:-35291}" >&2
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

extra_cert_names() {
  local extras="${usage_extra_hosts-${GRAM_SSL_EXTRA_HOSTS:-}}"
  [ -n "$extras" ] || return 0
  check_command node
  node --input-type=module - "$extras" <<'NODE'
import { isIP } from "node:net";
import { domainToASCII } from "node:url";
const names = process.argv[2].split(",").map((name) => name.trim()).filter(Boolean);
const hostname = /^(?:\*\.)?[0-9a-z_-](?:[0-9a-z._-]*[0-9a-z_-])?$/i;
for (const [index, name] of names.entries()) {
  if (isIP(name) !== 0) continue;
  const ascii = name.startsWith("*.") ? `*.${domainToASCII(name.slice(2))}` : domainToASCII(name);
  if (/[\/\s:@]/.test(name) || name.startsWith("-") || !hostname.test(ascii)) {
    console.error(`Invalid GRAM_SSL_EXTRA_HOSTS entry ${index + 1}: use a DNS name or bare IP address, without a scheme, path, whitespace, or port.`);
    process.exit(1);
  }
}
if (names.length) console.log(names.join("\n"));
NODE
}

cert_names() {
  local site_host server_host devidp_host extra_hosts
  site_host="$(extract_hostname "$GRAM_SITE_URL")"
  server_host="$(extract_hostname "$GRAM_SERVER_URL")"
  devidp_host="$(extract_hostname "${GRAM_DEVIDP_EXTERNAL_URL:-}")"
  extra_hosts="$(extra_cert_names)" || return 1

  # host.docker.internal lets local assistant runtime containers dial the
  # server over TLS through Docker's host gateway alias.
  {
    printf "%s\n" \
      "$site_host" \
      "$server_host" \
      "$devidp_host" \
      "localhost" \
      "host.docker.internal" \
      "127.0.0.1" \
      "::1"
    printf "%s\n" "$extra_hosts"
  } | awk '{ gsub(/^[[:space:]]+|[[:space:]]+$/, "") } NF && !seen[$0]++'
}

gen_keypair() {
  ensure_dir "$(dirname "$CERT_PATH")"
  ensure_dir "$(dirname "$KEY_PATH")"

  names=()
  while IFS= read -r name; do
    names+=("$name")
  done <<< "$1"

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
  local hosts
  validate_devidp_url
  if ! detect_tls; then turn_off_tls && exit 0; fi

  check_command mkcert
  hosts="$(cert_names)"
  turn_on_tls
  configure_linux_browser_trust
  trust_local_ca
  gen_keypair "$hosts"
}

main "$@"
