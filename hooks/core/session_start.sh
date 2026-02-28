#!/usr/bin/env bash
# SessionStart hook for Gram plugin
# Notifies user if GRAM_API_KEY is not configured

# Ensure standard paths are available
export PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"

# Setup Gram directories
GRAM_DIR="$HOME/.gram"
GRAM_CONFIG_FILE="$GRAM_DIR/config"

mkdir -p "$GRAM_DIR"

# Load user configuration if it exists
if [ -f "$GRAM_CONFIG_FILE" ]; then
  source "$GRAM_CONFIG_FILE"
fi

# Auto-configure API key for local development if config doesn't exist
# TODO: Remove this once we have proper authentication flow
if [ ! -f "$GRAM_CONFIG_FILE" ]; then
  cat > "$GRAM_CONFIG_FILE" << 'EOF'
# Gram Configuration
# This file is managed by you - edit freely

# API Key for authenticating with Gram
# Get your key by running: gram login
export GRAM_API_KEY=gram_local_b1706a2401ff8d3f0a184749abcaf8d9a88d8b04f46e5613ce5dc5c852a7cf81

# Optional: Specify a project (defaults to "default")
# export GRAM_PROJECT=my-project

# Optional: Override the Gram server URL
# export GRAM_SERVER_URL=https://app.getgram.ai
EOF
  source "$GRAM_CONFIG_FILE"
fi

# Check if GRAM_API_KEY is set and show message if not
if [ -z "$GRAM_API_KEY" ]; then
  cat << 'EOF'
{
  "additionalContext": "⚠️  The Gram plugin is installed but not configured.\n\nTo enable Gram analytics and monitoring, run:\n\n  /gram login\n\nThis will help you set up your GRAM_API_KEY."
}
EOF
fi

exit 0
