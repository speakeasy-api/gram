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

1. Add it to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):
   ```bash
   export GRAM_API_KEY="your-api-key-here"
   export GRAM_PROJECT="default"  # optional
   ```

2. Reload your shell:
   ```bash
   source ~/.zshrc  # or ~/.bashrc
   ```

3. Restart Claude Code to use the new configuration

---

## Implementation

```bash
#!/usr/bin/env bash

COMMAND="${1:-help}"

case "$COMMAND" in
  login)
    # Setup Gram directories
    GRAM_DIR="$HOME/.gram"
    GRAM_CONFIG_FILE="$GRAM_DIR/config"

    mkdir -p "$GRAM_DIR"

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
    read -p "Enter your Gram API key: " -r GRAM_API_KEY

    if [ -z "$GRAM_API_KEY" ]; then
      echo "❌ No API key provided. Configuration cancelled."
      exit 1
    fi

    # Optional: Prompt for project
    read -p "Enter your Gram project name (default: default): " -r GRAM_PROJECT
    if [ -z "$GRAM_PROJECT" ]; then
      GRAM_PROJECT="default"
    fi

    # Write configuration file
    cat > "$GRAM_CONFIG_FILE" << EOF
# Gram Configuration
# This file is managed by the Gram plugin

# API Key for authenticating with Gram
export GRAM_API_KEY=$GRAM_API_KEY

# Project name
export GRAM_PROJECT=$GRAM_PROJECT

# Optional: Override the Gram server URL
# export GRAM_SERVER_URL=https://app.getgram.ai
EOF

    echo ""
    echo "✅ Configuration saved to $GRAM_CONFIG_FILE"
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
