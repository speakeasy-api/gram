# Cursor Hooks for Gram

This directory contains the Cursor hooks configuration for sending hook events to Gram.

## Installation

To use these hooks with Cursor, copy the `hooks.json` file to your Cursor hooks directory:

### Project-level (recommended)
```bash
mkdir -p .cursor
cp hooks/cursor/hooks.json .cursor/hooks.json
cp hooks/cursor/send_hook.sh .cursor/send_hook.sh
chmod +x .cursor/send_hook.sh
```

### User-level (global)
```bash
mkdir -p ~/.cursor
cp hooks/cursor/hooks.json ~/.cursor/hooks.json
cp hooks/cursor/send_hook.sh ~/.cursor/send_hook.sh
chmod +x ~/.cursor/send_hook.sh
```

## Configuration

By default, hooks will be sent to `https://app.getgram.ai`. To use a different server URL (e.g., for local development), set the `GRAM_HOOKS_SERVER_URL` environment variable:

```bash
export GRAM_HOOKS_SERVER_URL="http://localhost:8080"
```

## Supported Hooks

The configuration includes all Cursor hook types:

### Agent Hooks (Cmd+K / Agent Chat)
- `sessionStart` / `sessionEnd` - Session lifecycle management
- `preToolUse` / `postToolUse` / `postToolUseFailure` - Generic tool use hooks
- `subagentStart` / `subagentStop` - Subagent (Task tool) lifecycle
- `beforeShellExecution` / `afterShellExecution` - Shell command control
- `beforeMCPExecution` / `afterMCPExecution` - MCP tool usage control
- `beforeReadFile` / `afterFileEdit` - File access and edit control
- `beforeSubmitPrompt` - Prompt validation before submission
- `preCompact` - Context window compaction observation
- `stop` - Agent completion handling
- `afterAgentResponse` / `afterAgentThought` - Response and thinking tracking

### Tab Hooks (Inline Completions)
- `beforeTabFileRead` - Control file access for Tab
- `afterTabFileEdit` - Post-process Tab edits

## How It Works

When Cursor triggers a hook event, it calls the `send_hook.sh` script which:
1. Receives the hook event data via stdin (JSON format)
2. Sends it to the Gram hooks endpoint at `/rpc/hooks.cursor`
3. Returns the response to Cursor

The Gram server processes these hooks and can:
- Allow or deny actions (for permission-based hooks)
- Inject additional context or environment variables
- Log and track agent behavior for observability

## Development

To test the hooks locally:

1. Start the Gram server locally:
   ```bash
   mise start:server --dev-single-process
   ```

2. Set the hooks server URL:
   ```bash
   export GRAM_HOOKS_SERVER_URL="http://localhost:8080"
   ```

3. Test a hook event:
   ```bash
   echo '{"hook_event_name":"sessionStart","conversation_id":"test-123","model":"claude-3-5-sonnet"}' | ./send_hook.sh
   ```

## See Also

- [Cursor Hooks Documentation](https://cursor.com/docs/hooks)
- Claude Code hooks: `hooks/plugin-claude/hooks/hooks.json`
- Backend implementation: `server/internal/hooks/impl.go`
