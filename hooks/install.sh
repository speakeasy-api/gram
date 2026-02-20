#!/usr/bin/env bash
# Install Gram Claude hooks to user's global Claude config
# Usage:
#   Remote: curl -fsSL https://raw.githubusercontent.com/gram-ai/gram/main/hooks/install.sh | bash
#   Local:  ./hooks/install.sh
set -euo pipefail

GITHUB_REPO="gram-ai/gram"
GITHUB_BRANCH="main"
HOOKS_BASE_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/${GITHUB_BRANCH}/hooks"
CLAUDE_HOOKS_DIR="$HOME/.claude/hooks"

# Check if running from the repo (local) or downloaded (remote)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || echo "")"
LOCAL_HOOKS_DIR=""
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/pre_tool_use.sh" ]; then
  LOCAL_HOOKS_DIR="$SCRIPT_DIR"
fi

echo "Installing Gram Claude hooks..."
echo ""

# Create hooks directory if it doesn't exist
mkdir -p "$CLAUDE_HOOKS_DIR"

# Install each hook script
for hook_name in pre_tool_use post_tool_use post_tool_use_failure; do
  hook_file="${hook_name}.sh"
  echo "  Installing $hook_file"

  if [ -n "$LOCAL_HOOKS_DIR" ]; then
    # Local installation - copy from repo
    cp "$LOCAL_HOOKS_DIR/$hook_file" "$CLAUDE_HOOKS_DIR/$hook_file"
  else
    # Remote installation - download from GitHub
    if command -v curl &> /dev/null; then
      curl -fsSL "${HOOKS_BASE_URL}/${hook_file}" -o "$CLAUDE_HOOKS_DIR/$hook_file"
    elif command -v wget &> /dev/null; then
      wget -q "${HOOKS_BASE_URL}/${hook_file}" -O "$CLAUDE_HOOKS_DIR/$hook_file"
    else
      echo "Error: neither curl nor wget found. Please install one of them."
      exit 1
    fi
  fi

  chmod +x "$CLAUDE_HOOKS_DIR/$hook_file"
done

echo ""
echo "✓ Hooks installed to $CLAUDE_HOOKS_DIR"
echo ""

# Check if GRAM_API_KEY is set
if [ -z "$GRAM_API_KEY" ]; then
  echo "⚠️  GRAM_API_KEY environment variable is not set"
  echo ""
  echo "To enable hook forwarding to Gram:"
  echo "  1. Visit https://app.getgram.ai to get your API key"
  echo "  2. Set the GRAM_API_KEY environment variable in your shell profile:"
  echo "     export GRAM_API_KEY=your-api-key-here"
  echo ""
else
  echo "✓ GRAM_API_KEY is set"
  echo ""
fi

# Configure Claude settings
CLAUDE_SETTINGS="$HOME/.claude/settings.json"
SETTINGS_DIR="$(dirname "$CLAUDE_SETTINGS")"

# Create .claude directory if it doesn't exist
mkdir -p "$SETTINGS_DIR"

# Check if settings.json exists
if [ ! -f "$CLAUDE_SETTINGS" ]; then
  echo "Creating $CLAUDE_SETTINGS..."
  echo '{}' > "$CLAUDE_SETTINGS"
fi

# Check if jq is available for JSON manipulation
if command -v jq &> /dev/null; then
  echo "Configuring hooks in $CLAUDE_SETTINGS..."

  # Create the hooks configuration
  HOOKS_CONFIG=$(cat << 'EOF'
{
  "PreToolUse": [
    {
      "hooks": [
        {
          "type": "command",
          "command": "~/.claude/hooks/pre_tool_use.sh",
          "timeout": 10
        }
      ]
    }
  ],
  "PostToolUse": [
    {
      "hooks": [
        {
          "type": "command",
          "command": "~/.claude/hooks/post_tool_use.sh",
          "timeout": 10
        }
      ]
    }
  ],
  "PostToolUseFailure": [
    {
      "hooks": [
        {
          "type": "command",
          "command": "~/.claude/hooks/post_tool_use_failure.sh",
          "timeout": 10
        }
      ]
    }
  ]
}
EOF
)

  # Merge hooks into existing settings
  TMP_FILE=$(mktemp)
  jq --argjson hooks "$HOOKS_CONFIG" '.hooks = $hooks' "$CLAUDE_SETTINGS" > "$TMP_FILE"
  mv "$TMP_FILE" "$CLAUDE_SETTINGS"

  echo "✓ Hooks configured in $CLAUDE_SETTINGS"
  echo ""
  echo "Next steps:"
  echo "  1. Restart Claude Code for changes to take effect"
else
  echo "⚠️  jq not found - cannot automatically update settings"
  echo ""
  echo "Please manually add this to $CLAUDE_SETTINGS:"
  echo ""
  cat << 'EOF'
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/pre_tool_use.sh",
            "timeout": 10
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/post_tool_use.sh",
            "timeout": 10
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "~/.claude/hooks/post_tool_use_failure.sh",
            "timeout": 10
          }
        ]
      }
    ]
  }
EOF
  echo ""
  echo "Then restart Claude Code for changes to take effect"
fi
