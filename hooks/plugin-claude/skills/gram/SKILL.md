---
name: gram
description: Manage Gram plugin configuration and authentication
disable-model-invocation: false
---

# Gram Plugin Management

This skill provides commands for managing the Gram plugin configuration.

## Available Commands

### `/gram login`

Opens the Gram API keys page in your browser and provides instructions for setting up your GRAM_API_KEY.

**Usage:**
```
/gram login
```

**What it does:**
1. Opens https://app.getgram.ai/settings/api-keys in your browser
2. Shows you how to configure your GRAM_API_KEY environment variable
3. Provides instructions for persisting the key across sessions

**After getting your API key:**

The API key will be saved to your Claude Code settings file (`~/.claude/settings.json`) in the `env` section. This makes it available to all Claude Code sessions automatically.

---

## Implementation

```bash
#!/usr/bin/env bash

COMMAND="${1:-help}"

case "$COMMAND" in
  login)
    # Setup Claude settings directory
    CLAUDE_DIR="$HOME/.claude"
    CLAUDE_SETTINGS="$CLAUDE_DIR/settings.json"

    mkdir -p "$CLAUDE_DIR"

    # Open the API keys page
    SETUP_URL="https://app.getgram.ai/settings/api-keys"

    if command -v open >/dev/null 2>&1; then
      open "$SETUP_URL" 2>/dev/null &
    elif command -v xdg-open >/dev/null 2>&1; then
      xdg-open "$SETUP_URL" 2>/dev/null &
    elif command -v wslview >/dev/null 2>&1; then
      wslview "$SETUP_URL" 2>/dev/null &
    fi

    cat << 'EOF'
🔑 **Opening Gram API Keys Page**

A browser window has been opened to: https://app.getgram.ai/settings/api-keys

**Next Steps:**

1. Copy your API key from the Gram dashboard
2. Paste it below when prompted

EOF

    # Prompt for API key
    read -p "Enter your Gram API key: " -r GRAM_API_KEY2

    if [ -z "$GRAM_API_KEY2" ]; then
      echo "❌ No API key provided. Configuration cancelled."
      exit 1
    fi

    # Optional: Prompt for project
    read -p "Enter your Gram project name (default: default): " -r GRAM_PROJECT
    if [ -z "$GRAM_PROJECT" ]; then
      GRAM_PROJECT="default"
    fi

    # Create or update settings.json with env variables
    if [ ! -f "$CLAUDE_SETTINGS" ]; then
      cat > "$CLAUDE_SETTINGS" << EOF
{
  "env": {
    "GRAM_API_KEY2": "$GRAM_API_KEY",
    "GRAM_PROJECT": "$GRAM_PROJECT"
  }
}
EOF
    else
      # Use jq to update existing settings.json if available, otherwise manual update
      if command -v jq >/dev/null 2>&1; then
        TMP_FILE=$(mktemp)
        jq --arg key "$GRAM_API_KEY" --arg project "$GRAM_PROJECT" \
          '.env.GRAM_API_KEY = $key | .env.GRAM_PROJECT = $project' \
          "$CLAUDE_SETTINGS" > "$TMP_FILE" && mv "$TMP_FILE" "$CLAUDE_SETTINGS"
      else
        echo ""
        echo "⚠️  Please manually add the following to $CLAUDE_SETTINGS:"
        echo ""
        echo '  "env": {'
        echo '    "GRAM_API_KEY2": "'"$GRAM_API_KEY"'",'
        echo '    "GRAM_PROJECT": "'"$GRAM_PROJECT"'"'
        echo '  }'
        echo ""
      fi
    fi

    echo ""
    echo "✅ Configuration saved to $CLAUDE_SETTINGS"
    echo ""
    echo "The Gram plugin will now automatically track:"
    echo "- Tool usage patterns"
    echo "- Performance analytics"
    echo "- Compliance and audit logs"
    echo ""
    echo "Learn more: https://getgram.ai/docs"
    ;;

  help|*)
    cat << 'EOF'
**Gram Plugin Commands**

Available commands:
- `/gram login` - Set up your Gram API key

For more information, visit: https://getgram.ai/docs
EOF
    ;;
esac
```
