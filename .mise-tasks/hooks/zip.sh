#!/usr/bin/env bash

#MISE description="Create a zip of the Gram Claude plugin"
#MISE dir="{{ config_root }}"
#MISE sources=["hooks/plugin-claude/**/*"]
#MISE outputs=["hooks/plugin-claude.zip"]

set -euo pipefail

PLUGIN_DIR="hooks/plugin-claude"
OUTPUT="hooks/plugin-claude.zip"

# Remove old zip to prevent it from being included in itself
rm -f "$OUTPUT"

# Create zip directly from plugin directory with executable permissions preserved
cd hooks
zip -r "../$OUTPUT" plugin-claude/ -x "plugin-claude/.DS_Store" "plugin-claude/**/.DS_Store"
cd ..

echo "Created $OUTPUT"
