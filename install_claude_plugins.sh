#!/usr/bin/env bash
#
# MDM-friendly: distribute a Claude (Cowork / Claude Code) plugin marketplace +
# plugins by writing the console user's ~/.claude/settings.json declaratively.
# Cowork and Claude Code share this store; Cowork regenerates ~/.claude/plugins/*
# (known_marketplaces.json, installed_plugins.json, cloned marketplaces/) from it
# on next launch — so we only write settings.json.
#
# Runs as root (Jamf/Kandji/Intune deploy); all writes land as the logged-in user.
# Does NOT touch managed-settings.json (enterprise/org policy) — user scope only.
#
# Configure via env (defaults below):
#   MARKETPLACE_KEY    logical name used in enabledPlugins keys (default: company-plugins)
#   MARKETPLACE_SRC    a git URL (https://….git) OR a GitHub owner/repo
#                      (default: https://app.getgram.ai/marketplace/REPLACE_ME.git)
#   PLUGINS            space-separated plugin names to enable     (default: example-plugin)
#
set -euo pipefail

MARKETPLACE_KEY="${MARKETPLACE_KEY:-company-plugins}"
MARKETPLACE_SRC="${MARKETPLACE_SRC:-https://app.getgram.ai/marketplace/REPLACE_ME.git}"
PLUGINS="${PLUGINS:-example-plugin}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

[[ "$(uname -s)" == "Darwin" ]] || die "macOS only."

# --- resolve the logged-in console user (NOT root) ---------------------------
CONSOLE_USER="$(stat -f%Su /dev/console)"
[[ -n "$CONSOLE_USER" && "$CONSOLE_USER" != "root" ]] \
  || die "No GUI user logged in; cannot target a per-user ~/.claude. Re-run when a user is at the console."
USER_HOME="$(dscl . -read "/Users/$CONSOLE_USER" NFSHomeDirectory | awk '{print $2}')"
USER_GROUP="$(id -gn "$CONSOLE_USER")"
CLAUDE_DIR="$USER_HOME/.claude"
SETTINGS="$CLAUDE_DIR/settings.json"

log "Target user: $CONSOLE_USER ($USER_HOME)"
log "Marketplace: $MARKETPLACE_KEY -> github:$MARKETPLACE_REPO"
log "Plugins:     $PLUGINS"

# --- ensure ~/.claude exists, owned by the user ------------------------------
sudo -u "$CONSOLE_USER" mkdir -p "$CLAUDE_DIR"

# --- merge our marketplace + plugins into existing settings.json -------------
# python3 ships with macOS; merge so we never clobber the user's other settings.
MARKETPLACE_KEY="$MARKETPLACE_KEY" MARKETPLACE_REPO="$MARKETPLACE_REPO" \
PLUGINS="$PLUGINS" SETTINGS="$SETTINGS" \
sudo -u "$CONSOLE_USER" /usr/bin/python3 - <<'PY'
import json, os

path = os.environ["SETTINGS"]
key  = os.environ["MARKETPLACE_KEY"]
repo = os.environ["MARKETPLACE_REPO"]
plugins = os.environ["PLUGINS"].split()

try:
    with open(path) as f:
        cfg = json.load(f)
    if not isinstance(cfg, dict):
        cfg = {}
except (FileNotFoundError, json.JSONDecodeError):
    cfg = {}

mkts = cfg.setdefault("extraKnownMarketplaces", {})
mkts[key] = {"source": {"source": "github", "repo": repo}, "autoUpdate": True}

enabled = cfg.setdefault("enabledPlugins", {})
for p in plugins:
    enabled[f"{p}@{key}"] = True

tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(cfg, f, indent=2)
    f.write("\n")
os.replace(tmp, path)
print(f"wrote {path}")
PY

# --- ownership/permissions (sudo -u already owns them; assert for safety) -----
chown "$CONSOLE_USER:$USER_GROUP" "$CLAUDE_DIR" "$SETTINGS"
chmod 700 "$CLAUDE_DIR"
chmod 600 "$SETTINGS"

log "Done. Marketplace + plugins enabled in user scope; they activate on next Claude launch."
log "Verify (as the user):  claude plugin list   — or open /plugin > Installed."
