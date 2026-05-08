#!/bin/bash
# Gram Claude Code MDM Bootstrap — Jamf Pro
#
# Upload this script to Jamf once. It never needs updating.
# All logic lives at the Gram server URL below and updates automatically.
#
# Jamf parameters:
#   $4 = GRAM_API_KEY (required) — create at https://app.getgram.ai/<org>/api-keys with Hooks scope
#
# Exit codes: 0=success 1=config error

set -euo pipefail

GRAM_API_KEY="${4:-}"
GRAM_INSTALL_SCRIPT="https://app.getgram.ai/rpc/mdm.getInstallScript"

[[ -n "$GRAM_API_KEY" ]] || { echo "ERROR: \$4 GRAM_API_KEY required" >&2; exit 1; }

CONSOLE_USER=$(stat -f '%Su' /dev/console 2>/dev/null || true)
[[ "$CONSOLE_USER" =~ ^(root|loginwindow|)$ ]] && { echo "ERROR: no console user logged in" >&2; exit 1; }

USER_UID=$(id -u "$CONSOLE_USER")
USER_HOME=$(/usr/sbin/dscl . -read "/Users/$CONSOLE_USER" NFSHomeDirectory | awk '{print $2}')

echo "Applying Gram settings for $CONSOLE_USER..."

WRAPPER=$(mktemp)
cat > "$WRAPPER" <<WEOF
#!/bin/bash
export HOME="$USER_HOME"
curl -fsSL '$GRAM_INSTALL_SCRIPT' | bash -s -- '$GRAM_API_KEY'
WEOF
chmod +x "$WRAPPER"

/bin/launchctl asuser "$USER_UID" "$WRAPPER"
rm -f "$WRAPPER"

echo "Done."
