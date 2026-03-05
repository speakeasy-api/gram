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

1. Opens the Gram dashboard and returns an API key for Claude to save

**After getting your API key:**

The API key will be saved to your Claude Code settings file (`~/.claude/settings.json`) in the `env` section. This makes it available to all Claude Code sessions automatically.

---

## Implementation

```bash
#!/usr/bin/env bash

COMMAND="${1:-help}"

case "$COMMAND" in
  login)
    # Execute the login script from the plugin root
    bash "$CLAUDE_PLUGIN_ROOT/skills/gram/scripts/login.sh"
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
