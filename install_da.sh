#!/usr/bin/env bash
#
# Install the Speakeasy device agent (speakeasyd + speakeasy) on this Mac and
# register it as a LaunchAgent, then drop a root-owned managed.json identity.
#
# Secrets are NEVER hardcoded. Provide the org token via the environment:
#
#   SPEAKEASY_ORG_TOKEN=gram_live_... ./install-speakeasy-agent.sh
#
# Optional overrides (all have sensible defaults):
#   SPEAKEASY_EMAIL        (default: your login user @ the org domain — see below)
#   SPEAKEASY_ORG_SLUG     (default: speakeasy-team)
#   SPEAKEASY_ORG_NAME     (default: Speakeasy)
#   SPEAKEASY_AUTO_UPDATE  (default: notify   — one of: disabled|notify|automatic)
#
# The release version may be passed as the first argument, via SPEAKEASY_VERSION,
# or left to the default below (with or without a leading "v"):
#
#   ./install-speakeasy-agent.sh 0.1.0-rc.6
#   SPEAKEASY_VERSION=0.1.0-rc.6 ./install-speakeasy-agent.sh
#
set -euo pipefail
# Precedence: positional arg > SPEAKEASY_VERSION > default. Strip any leading "v".
VERSION="${1:-${SPEAKEASY_VERSION:-0.1.0-rc.5}}"
VERSION="${VERSION#v}"
BASE="https://storage.googleapis.com/speakeasy-device-agent-releases-prod/v${VERSION}"
MANAGED_DIR="/Library/Application Support/Speakeasy"
MANAGED_FILE="${MANAGED_DIR}/managed.json"
ORG_SLUG="${SPEAKEASY_ORG_SLUG:-speakeasy-team}"
ORG_NAME="${SPEAKEASY_ORG_NAME:-Speakeasy}"
AUTO_UPDATE="${SPEAKEASY_AUTO_UPDATE:-notify}"
log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
# --- sanity checks -----------------------------------------------------------
[[ "$(uname -s)" == "Darwin" ]] || die "This script targets macOS only."
[[ -n "${SPEAKEASY_ORG_TOKEN:-}" ]] || die "Set SPEAKEASY_ORG_TOKEN in the environment (treat it as a secret; do not commit it)."
# The agent runs as the logged-in user; managed.json must be readable by them.
LOGIN_USER="$(stat -f%Su /dev/console)"
LOGIN_GROUP="$(id -gn "$LOGIN_USER")"
EMAIL="${SPEAKEASY_EMAIL:-}"
[[ -n "$EMAIL" ]] || die "Set SPEAKEASY_EMAIL to the enrolled work email (e.g. you@speakeasy.com)."
# --- detect arch -------------------------------------------------------------
case "$(uname -m)" in
  arm64) ARCH="darwin_arm64" ;;
  x86_64) ARCH="darwin_amd64" ;;
  *) die "Unsupported architecture: $(uname -m)" ;;
esac
log "Platform: ${ARCH}, version ${VERSION}"
# --- download ----------------------------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
log "Downloading speakeasyd + speakeasy…"
curl -fSL -o "$TMP/speakeasyd" "$BASE/speakeasyd_${VERSION}_${ARCH}"
curl -fSL -o "$TMP/speakeasy"  "$BASE/speakeasy_${VERSION}_${ARCH}"
# --- install into PATH -------------------------------------------------------
# NOTE: the CLI is installed as `speakeasyg` to avoid colliding with an
# existing `speakeasy` binary already on this machine. The daemon keeps its
# name (`speakeasyd`).
log "Installing into /usr/local/bin (sudo)…"
chmod +x "$TMP/speakeasyd" "$TMP/speakeasy"
sudo mkdir -p /usr/local/bin
sudo mv "$TMP/speakeasyd" /usr/local/bin/speakeasyd
sudo mv "$TMP/speakeasy"  /usr/local/bin/speakeasyg
# --- write managed.json (root-owned, group-readable by the agent user) -------
log "Writing ${MANAGED_FILE}…"
sudo mkdir -p "$MANAGED_DIR"
sudo chmod 0755 "$MANAGED_DIR"
# Build JSON in a temp file with python3 so the token is properly escaped and
# never appears on a command line / in ps output.
JSON_TMP="$(mktemp)"
SPEAKEASY_ORG_TOKEN="$SPEAKEASY_ORG_TOKEN" \
EMAIL="$EMAIL" ORG_SLUG="$ORG_SLUG" ORG_NAME="$ORG_NAME" AUTO_UPDATE="$AUTO_UPDATE" \
python3 - "$JSON_TMP" <<'PY'
import json, os, sys
doc = {
    "v": 1,
    "email": os.environ["EMAIL"],
    "org_token": os.environ["SPEAKEASY_ORG_TOKEN"],
    "org_slug": os.environ["ORG_SLUG"],
    "org_name": os.environ["ORG_NAME"],
    "auto_update": os.environ["AUTO_UPDATE"],
}
with open(sys.argv[1], "w") as f:
    json.dump(doc, f, indent=2)
    f.write("\n")
PY
sudo cp "$JSON_TMP" "$MANAGED_FILE"
rm -f "$JSON_TMP"
# root owner, login user's group, 0640 -> owner rw, group (agent user) r, others none
sudo chown "root:${LOGIN_GROUP}" "$MANAGED_FILE"
sudo chmod 0640 "$MANAGED_FILE"
# --- register + start the service --------------------------------------------
log "Registering the LaunchAgent…"
# Remove any previous incarnation first so re-runs are idempotent. The freshly
# installed speakeasyd manages the same service; stop+uninstall clears a stale
# ~/Library/LaunchAgents/com.speakeasy.daemon.plist. Errors are ignored when
# nothing is installed yet.
speakeasyd -service stop      2>/dev/null || true
speakeasyd -service uninstall 2>/dev/null || true
speakeasyd -service install
speakeasyd -service start
# --- verify ------------------------------------------------------------------
log "Verifying…"
speakeasyg config path || true
speakeasyg status
log "Done. The agent should show as \"Provisioned by IT\" once it reads managed.json."