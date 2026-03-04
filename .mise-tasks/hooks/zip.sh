#!/usr/bin/env bash

#MISE description="Create a zip of the Gram Claude plugin"
#MISE dir="{{ config_root }}"
#MISE sources=["hooks/plugin-claude/**/*", "hooks/core/**/*"]
#MISE outputs=["hooks/plugin-claude.zip"]

set -euo pipefail

PLUGIN_DIR="hooks/plugin-claude"
CORE_DIR="hooks/core"
OUTPUT="hooks/plugin-claude.zip"
TEMP_DIR=$(mktemp -d)

# Copy plugin-claude directory to temp
rsync -rL "$PLUGIN_DIR/" "$TEMP_DIR/plugin-claude/"

# Copy core scripts into the plugin under scripts/core/
mkdir -p "$TEMP_DIR/plugin-claude/scripts/core"
rsync -rL "$CORE_DIR/" "$TEMP_DIR/plugin-claude/scripts/core/"

# Create zip from temp directory with executable permissions preserved
cd "$TEMP_DIR"
zip -r "$OLDPWD/$OUTPUT" plugin-claude/ -x "plugin-claude/.DS_Store" "plugin-claude/**/.DS_Store"
cd "$OLDPWD"

# Clean up temp directory
rm -rf "$TEMP_DIR"

echo "Created $OUTPUT"
