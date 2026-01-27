# Claude Code Hooks Setup

Hooks allow you to run shell commands in response to Claude Code events. This document explains how to configure hooks for the Gram project.

## Available Hook Types

| Hook | Trigger | Use Case |
|------|---------|----------|
| `PreToolUse` | Before a tool runs | Validate, transform inputs |
| `PostToolUse` | After a tool completes | Auto-format, notify |
| `Notification` | On specific events | Alerts, logging |
| `Stop` | When Claude stops | Verification, cleanup |

## Recommended Hooks for Gram

### 1. Auto-Format Go Code (PostToolUse)

Automatically format Go files after edits:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "*.go"
        },
        "command": "gofmt -w $FILE_PATH"
      }
    ]
  }
}
```

### 2. Auto-Format TypeScript Code (PostToolUse)

Automatically format TypeScript files after edits:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "*.ts"
        },
        "command": "npx prettier --write $FILE_PATH"
      },
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "*.tsx"
        },
        "command": "npx prettier --write $FILE_PATH"
      }
    ]
  }
}
```

### 3. Lint on Stop (Stop Hook)

Run linters when Claude finishes a task:

```json
{
  "hooks": {
    "Stop": [
      {
        "command": "mise lint:server 2>&1 | head -50",
        "condition": {
          "files_changed": "*.go"
        }
      },
      {
        "command": "mise lint:client 2>&1 | head -50",
        "condition": {
          "files_changed": ["*.ts", "*.tsx"]
        }
      }
    ]
  }
}
```

### 4. Verify Generated Code (PostToolUse)

Ensure generated code is regenerated when design files change:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "server/design/*.go"
        },
        "command": "echo '⚠️  Design file changed. Remember to run: mise gen:goa-server'"
      },
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "**/queries.sql"
        },
        "command": "echo '⚠️  Query file changed. Remember to run: mise gen:sqlc-server'"
      }
    ]
  }
}
```

### 5. Schema Change Reminder (PostToolUse)

Remind to generate migrations when schema changes:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": {
          "tool_name": "Edit",
          "file_path": "server/database/schema.sql"
        },
        "command": "echo '⚠️  Schema changed. Run: mise db:diff <migration-name>'"
      }
    ]
  }
}
```

## Full Configuration Example

Create or edit `~/.claude/settings.json` (global) or `.claude/settings.local.json` (project):

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": { "tool_name": "Edit", "file_path": "*.go" },
        "command": "gofmt -w $FILE_PATH"
      },
      {
        "matcher": { "tool_name": "Edit", "file_path": "*.ts" },
        "command": "npx prettier --write $FILE_PATH"
      },
      {
        "matcher": { "tool_name": "Edit", "file_path": "*.tsx" },
        "command": "npx prettier --write $FILE_PATH"
      },
      {
        "matcher": { "tool_name": "Edit", "file_path": "server/design/*.go" },
        "command": "echo '⚠️  Run: mise gen:goa-server'"
      },
      {
        "matcher": { "tool_name": "Edit", "file_path": "**/queries.sql" },
        "command": "echo '⚠️  Run: mise gen:sqlc-server'"
      },
      {
        "matcher": { "tool_name": "Edit", "file_path": "server/database/schema.sql" },
        "command": "echo '⚠️  Run: mise db:diff <name>'"
      }
    ],
    "Stop": [
      {
        "command": "echo '✅ Session complete. Remember to run linters before committing.'"
      }
    ]
  }
}
```

## Environment Variables Available in Hooks

| Variable | Description |
|----------|-------------|
| `$FILE_PATH` | Path to the affected file |
| `$TOOL_NAME` | Name of the tool that triggered the hook |
| `$WORKING_DIR` | Current working directory |

## Important Notes

1. **Local Settings**: Use `.claude/settings.local.json` for personal hooks that shouldn't be committed
2. **Performance**: Keep hook commands fast (<1 second) to avoid slowing down Claude
3. **Error Handling**: Hook failures are reported but don't block Claude's operation
4. **Security**: Never put secrets in hook commands; use environment variables instead

## Testing Hooks

1. Add the hook configuration
2. Make a small edit to a matching file
3. Check if the hook command executed
4. Adjust as needed

## Troubleshooting

- **Hook not running**: Check file path matcher syntax
- **Command failing**: Test the command manually first
- **Slow performance**: Simplify the command or add conditions
