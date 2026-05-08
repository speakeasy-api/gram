#!/bin/bash
# Gram Claude Code MDM Deploy Script — Example
#
# This is an example of what the generateDeployScript endpoint returns. In practice,
# download the script via the Gram dashboard or API — it will have a real API key embedded.
#
# Compatible with: Jamf Pro, Kandji, Mosyle, Addigy, SimpleMDM, and any MDM
# that supports arbitrary shell script policies.
#
# Recommended policy trigger: "Login" or "Recurring check-in" (idempotent — safe to re-run).
# Only dependency: curl (always present on macOS).

set -euo pipefail

GRAM_API_KEY="<GRAM_API_KEY>"
GRAM_APPLY_SCRIPT="https://app.getgram.ai/rpc/mdm.getInstallScript"

CONSOLE_USER=$(stat -f '%Su' /dev/console 2>/dev/null || true)
[[ "$CONSOLE_USER" =~ ^(root|loginwindow|)$ ]] && { echo "Gram: no console user logged in, skipping" >&2; exit 0; }

USER_UID=$(id -u "$CONSOLE_USER")
USER_HOME=$(/usr/sbin/dscl . -read "/Users/$CONSOLE_USER" NFSHomeDirectory | awk '{print $2}')

echo "Gram: applying settings for $CONSOLE_USER..."

WRAPPER=$(mktemp)
trap 'rm -f "$WRAPPER"' EXIT
cat > "$WRAPPER" <<WEOF
#!/bin/bash
export HOME="$USER_HOME"
curl -fsSL "$GRAM_APPLY_SCRIPT" | bash -s -- "$GRAM_API_KEY"
WEOF
chmod +x "$WRAPPER"

/bin/launchctl asuser "$USER_UID" "$WRAPPER"
echo "Gram: done."
