# Gram Hooks

Forward editor hooks to Gram for analytics, monitoring, and compliance tracking.

## Overview

This directory contains Gram's hook implementations that can be packaged for different editors and IDEs:

- **Claude Code Plugin**: Native plugin for Claude Code and Claude Desktop
- **Standalone Installation**: Traditional script-based installation for Claude Code
- **Future**: Cursor plugin, VSCode extension, etc.

## Quick Start

### Option 1: Claude Code Plugin (Recommended)

```bash
claude plugin install gram-hooks
```

See [plugin-claude/README.md](./plugin-claude/README.md) for details.

### Option 2: Standalone Installation

```bash
# Remote installation
curl -fsSL https://raw.githubusercontent.com/gram-ai/gram/main/hooks/install.sh | bash

# Local installation (from this repo)
./hooks/install.sh
```

## Directory Structure

```
hooks/
├── core/                       # Shared core implementation (reusable)
│   ├── common.sh              # Common utilities and API client
│   ├── pre_tool_use.sh        # PreToolUse hook handler
│   ├── post_tool_use.sh       # PostToolUse hook handler
│   └── post_tool_use_failure.sh
│
├── plugin-claude/              # Claude Code plugin packaging
│   ├── .claude-plugin/
│   │   └── plugin.json        # Plugin manifest
│   ├── scripts/               # Hook scripts
│   ├── hooks/
│   │   └── hooks.json         # Hook event configuration
│   └── README.md              # Plugin-specific docs
│
├── install.sh                  # Standalone installer script
└── README.md                   # This file
```

## Core Module

The `core/` directory contains the shared implementation that powers all packaging methods:

- **common.sh**: Environment validation, JSON formatting, Gram API client
- **session_start.sh**: Handler for SessionStart events (interactive setup)
- **pre_tool_use.sh**: Handler for PreToolUse events
- **post_tool_use.sh**: Handler for PostToolUse events
- **post_tool_use_failure.sh**: Handler for PostToolUseFailure events

These scripts are designed to be:
1. Executable standalone
2. Packaged as a Claude plugin
3. Adapted for other editors (Cursor, VSCode, etc.)

## Configuration

All packaging methods require:

**Required:**
- `GRAM_API_KEY`: Your Gram API key

**Optional:**
- `GRAM_PROJECT`: Project name (defaults to "default")
- `GRAM_SERVER_URL`: Server URL (defaults to "https://app.getgram.ai")

### First-Time Setup

**Claude Code Plugin:**
1. Run `/gram login` after installation
2. Follow the instructions to get your API key
3. Add the key to your shell profile and restart Claude Code

**Standalone Installation:**
Set these environment variables in your shell profile before use:
```bash
export GRAM_API_KEY="your-api-key-here"
export GRAM_PROJECT="default"  # optional
```

## Adding Support for New Editors

To package these hooks for a new editor:

1. Create a new directory: `hooks/<editor-name>/`
2. Copy/symlink the core scripts from `hooks/core/`
3. Add editor-specific configuration files
4. Update the core scripts if needed (send PRs!)
5. Document the installation process

Example for Cursor:

```bash
mkdir -p hooks/cursor-plugin
cd hooks/cursor-plugin
# Link to core scripts
ln -s ../core/*.sh .
# Add Cursor-specific manifest
# ...
```

## Development

### Testing Changes

When modifying core scripts:

```bash
# Test with Claude Code plugin
claude --plugin-dir ./hooks/plugin-claude

# Test with standalone installation
./hooks/install.sh
```

## License

MIT
