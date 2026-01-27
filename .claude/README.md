# Claude Code Configuration for Gram

This directory contains Claude Code configuration, commands, and agents for the Gram project.

## Directory Structure

```
.claude/
├── README.md              # This file
├── settings.json          # Project-wide permissions and settings
├── settings.local.json    # Personal settings (gitignored)
├── commands/              # Slash commands for common workflows
│   ├── pr.md              # Create pull requests
│   ├── review.md          # Code review
│   ├── test.md            # Generate tests
│   ├── refactor.md        # Refactor code
│   ├── docs.md            # Generate documentation
│   ├── debug.md           # Debug issues
│   └── feature.md         # Implement features
├── agents/                # Specialized subagents
│   ├── code-simplifier.md # Simplify complex code
│   ├── security-check.md  # Security auditing
│   └── verify.md          # Verify changes
└── hooks/
    └── hooks-setup.md     # Hook configuration guide
```

## Quick Start

### Using Commands

Invoke commands with the `/` prefix in Claude Code:

| Command | Description |
|---------|-------------|
| `/pr` | Create a pull request with proper formatting |
| `/review` | Review code for bugs, security, and quality |
| `/test` | Generate tests for a file or function |
| `/refactor` | Refactor code while maintaining functionality |
| `/docs` | Generate or update documentation |
| `/debug` | Investigate and fix a bug |
| `/feature` | Implement a feature from plan to PR |

**Examples:**
```
/review server/internal/auth/impl.go
/test GetUserByID
/debug "API returns 500 on login"
/feature "Add rate limiting to API endpoints"
```

### Using Agents

Agents are specialized assistants for specific tasks. Reference them in your prompts:

```
Use the code-simplifier agent to clean up this file.
Run a security-check on the auth package.
Use verify to check my changes are ready for PR.
```

## Configuration

### settings.json

Pre-approved permissions for common operations:

- **Read operations**: File reading, searching, git inspection
- **Mise commands**: Build, test, lint, generate code
- **Package managers**: pnpm, go
- **Infrastructure (read-only)**: kubectl get/describe, terraform plan

Denied by default:
- Destructive git operations
- Infrastructure modifications
- Reading sensitive config files

### settings.local.json

Create this file for personal settings that shouldn't be committed:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": { "tool_name": "Edit", "file_path": "*.go" },
        "command": "gofmt -w $FILE_PATH"
      }
    ]
  }
}
```

## Project Context

This configuration is designed for the Gram project, an MCP Cloud Platform with:

- **Backend**: Go 1.25 with Goa framework, PostgreSQL, ClickHouse, Temporal
- **Frontend**: React 19, TypeScript, TanStack Query, Tailwind, Moonshine
- **Infrastructure**: Kubernetes, Terraform, Docker

Key development commands:
```bash
./zero                              # Full local setup
mise start:server --dev-single-process  # Start server
mise start:dashboard                # Start frontend
mise lint:server                    # Lint Go code
mise lint:client                    # Lint TypeScript
mise test:server                    # Run Go tests
mise gen:goa-server                 # Regenerate API code
mise gen:sqlc-server                # Regenerate SQL code
mise db:diff <name>                 # Create migration
```

## Contributing

### Adding a New Command

1. Create a new `.md` file in `commands/`
2. Add frontmatter with description:
   ```yaml
   ---
   description: Short description of what the command does
   ---
   ```
3. Document the command's process and expected output
4. Test the command manually before committing

### Adding a New Agent

1. Create a new `.md` file in `agents/`
2. Define the agent's mission and guidelines
3. Include Gram-specific patterns and constraints
4. Document expected output format

### Updating Permissions

Edit `settings.json` to add new permissions. Follow the principle of least privilege:
- Only add permissions that are commonly needed
- Prefer read-only operations
- Infrastructure commands should be limited to validation/dry-run

## Troubleshooting

### Command Not Found
Ensure the command file exists in `.claude/commands/` and has valid frontmatter.

### Permission Denied
Check `settings.json` for the required permission. Add it if safe, or approve manually.

### Agent Not Working
Verify the agent file has clear instructions and follows the expected format.

## Resources

- [Claude Code Documentation](https://docs.anthropic.com/en/docs/claude-code)
- [Gram CLAUDE.md](../CLAUDE.md) - Project coding guidelines
- [Gram CONTRIBUTING.md](../CONTRIBUTING.md) - Contribution guidelines
