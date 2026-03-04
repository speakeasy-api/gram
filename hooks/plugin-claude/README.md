# Gram Hooks Plugin for Claude Code

Forward Claude Code hooks to Gram for analytics, monitoring, and compliance tracking.

## What This Plugin Does

This plugin captures tool use events from Claude Code and forwards them to your Gram instance, enabling:

- **Analytics**: Track which tools are being used and when
- **Monitoring**: Monitor AI agent behavior in production
- **Compliance**: Maintain audit logs of AI operations
- **Debugging**: Understand tool execution patterns and failures

## Installation

### Via Claude Code Plugin System

```bash
# Install the plugin
claude plugin install gram-hooks

# Enable it (if not auto-enabled)
claude plugin enable gram-hooks
```

### Local Development

```bash
# Clone the Gram repository
git clone https://github.com/gram-ai/gram.git
cd gram

# Run Claude Code with the plugin
claude --plugin-dir ./hooks/plugin-claude
```

## Configuration

The plugin requires the following environment variables:

### Required

- `GRAM_API_KEY`: Your Gram API key (get one at https://app.getgram.ai)

### Optional

- `GRAM_PROJECT`: Project name for organizing hooks (defaults to "default")

### First-Time Setup

After installing the plugin, run:

```
/gram login
```

This will:
1. Open https://app.getgram.ai/settings/api-keys in your browser
2. Show you how to configure your `GRAM_API_KEY` environment variable
3. Provide instructions for persisting the key across sessions

**To persist your API key**, add these to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):

```bash
export GRAM_API_KEY="your-api-key-here"
export GRAM_PROJECT="my-project"  # optional
```

Then reload your shell:

```bash
source ~/.zshrc  # or ~/.bashrc
```

And restart Claude Code.

## How It Works

This plugin provides:

### Slash Commands

- **`/gram login`**: Set up your Gram API key (opens browser to get key)

### Hook Handlers

The plugin registers handlers for four Claude Code hook events:

1. **SessionStart**: Notifies you if GRAM_API_KEY is not configured
2. **PreToolUse**: Called before a tool executes (can approve/deny)
3. **PostToolUse**: Called after successful tool execution
4. **PostToolUseFailure**: Called when a tool execution fails

Hook events are forwarded to your Gram server at `https://app.getgram.ai/rpc/hooks.*` (or your custom `GRAM_SERVER_URL`).

## Troubleshooting

### Missing GRAM_API_KEY

If `GRAM_API_KEY` is not set:
1. You'll see a notification on session start
2. Run `/gram login` to get your API key
3. Follow the instructions to configure it in your shell profile
4. Restart Claude Code

### API Connection Issues

The plugin connects to `http://localhost:8080` by default. Ensure your Gram server is running locally or modify the URL in the hook scripts.

## Development

The plugin is structured for reusability:

```
hooks/
├── core/                    # Shared core implementation
│   ├── common.sh           # Common utilities
│   ├── pre_tool_use.sh     # PreToolUse handler
│   ├── post_tool_use.sh    # PostToolUse handler
│   └── post_tool_use_failure.sh
│
├── plugin-claude/           # Claude Code plugin packaging
│   ├── .claude-plugin/
│   │   └── plugin.json     # Plugin manifest
│   ├── scripts/            # Hook scripts
│   └── hooks/
│       └── hooks.json      # Hook configuration
│
└── install.sh              # Standalone installer
```

This structure allows the same core scripts to be:
- Packaged as a Claude Code plugin
- Installed standalone via `install.sh`
- Adapted for other editors (Cursor, etc.)

## License

MIT
