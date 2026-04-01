---
name: install-mcp-server
description: >-
  Use when installing a Gram toolset as an MCP server in an AI client. Triggers on
  "install mcp", "gram install", "connect to gram", "gram toolset", "mcp server".
license: Apache-2.0
---

# Install Gram MCP Server

Guide for installing Gram toolsets as MCP (Model Context Protocol) servers in AI clients.

## When to Use

- Connecting an AI client (Claude Code, Claude Desktop, Cursor, Gemini CLI) to a Gram toolset
- Setting up MCP server access for a deployed Gram project
- Configuring API key substitution for team/CI environments

## Prerequisites

- Gram CLI installed and authenticated (`gram whoami` to verify)
- A deployed Gram toolset (deploy first with **deploy-openapi** or **deploy-functions**)
- The target AI client installed on the machine

## Inputs

| Input | Description | Required |
|---|---|---|
| Client | `claude-code`, `claude-desktop`, `cursor`, or `gemini-cli` | Yes |
| Toolset slug OR MCP URL | Identifier for what to install | Yes (one of) |
| Scope | `project` or `user` (where applicable) | No |
| Env var name | For API key substitution | No |

## Outputs

| Output | Description |
|---|---|
| MCP config entry | Written to the client's configuration |
| `.mcpb` file | For Claude Desktop only (saved to Downloads) |

## Supported Clients

| Client | Command | Scope Support | Notes |
|---|---|---|---|
| Claude Code | `gram install claude-code` | `project` / `user` (default) | Native HTTP MCP support |
| Claude Desktop | `gram install claude-desktop` | N/A | Generates `.mcpb` file |
| Cursor | `gram install cursor` | N/A | Opens browser-based install |
| Gemini CLI | `gram install gemini-cli` | `project` / `user` (default) | Requires `gemini` command |

## Command

### By Toolset Slug (Recommended)

```bash
gram install claude-code --toolset my-api
```

The CLI looks up the MCP URL automatically from the toolset slug.

### By MCP URL (Manual)

```bash
gram install claude-code --mcp-url https://mcp.getgram.ai/org/project/environment
```

Use this when you have the URL directly or need to point to a specific environment.

> `--toolset` and `--mcp-url` are mutually exclusive. Use one or the other.

## Common Options

All `install` subcommands share these flags:

| Flag | Description | Default |
|---|---|---|
| `--toolset` | Toolset slug to install | — |
| `--mcp-url` | Direct MCP server URL | — |
| `--name` | Display name for the MCP server | Derived from toolset/URL |
| `--api-key` | API key to use (`$GRAM_API_KEY`) | From profile |
| `--project` | Target project slug (`$GRAM_PROJECT`) | From profile |
| `--header` | HTTP header name for the API key | `Authorization` |
| `--env-var` | Env var name for key substitution | — |

### Scope (Claude Code and Gemini CLI only)

| Flag | Description | Default |
|---|---|---|
| `--scope project` | Writes to `.mcp.json` in current directory | — |
| `--scope user` | Writes to user-level config | `user` |

### Output Directory (Claude Desktop only)

| Flag | Description | Default |
|---|---|---|
| `--output-dir` | Directory to save `.mcpb` file | Downloads folder |

## Decision Framework

| Scenario | Recommendation |
|---|---|
| Personal dev setup | `--scope user` (default) |
| Shared project repo | `--scope project` + `--env-var MCP_API_KEY` |
| CI/CD environment | `--env-var` with secrets injection |
| Custom auth header | `--header X-Api-Key` |
| Non-default MCP endpoint | `--mcp-url <url>` |

## Examples

### Claude Code — Project Scope with Env Var

```bash
gram install claude-code \
  --toolset my-api \
  --scope project \
  --env-var GRAM_MCP_KEY
```

This writes to `.mcp.json` using `${GRAM_MCP_KEY}` substitution instead of hardcoding the key.

### Claude Desktop

```bash
gram install claude-desktop --toolset my-api
```

Generates a `.mcpb` file in your Downloads folder. Open it to install in Claude Desktop.

### Cursor

```bash
gram install cursor --toolset my-api
```

Opens a browser-based installation flow.

### Gemini CLI — User Scope

```bash
gram install gemini-cli --toolset my-api --scope user
```

### Custom Header Name

```bash
gram install claude-code \
  --toolset my-api \
  --header X-Api-Key
```

## What NOT to Do

- Do not use both `--toolset` and `--mcp-url` — they are mutually exclusive
- Do not hardcode API keys in shared repos — use `--env-var` for team setups
- Do not use `--scope project` without `--env-var` if the repo is public
- Do not forget to deploy the toolset first — `gram install` connects to an existing deployment

## Troubleshooting

| Problem | Solution |
|---|---|
| "toolset not found" | Verify the toolset is deployed: `gram status` |
| MCP server not connecting | Check `gram whoami` — API key may be expired |
| Wrong tools appearing | Verify you're installing the correct toolset slug |
| Claude Code: "server not found" | Check `.mcp.json` or `~/.claude/settings.local.json` for the entry |
| Env var not resolving | Ensure the env var is set in your shell before launching the client |
| Custom header not working | Verify the server expects the header name you specified |

## Related Skills

- **gram-context** — CLI reference and authentication
- **deploy-openapi** — Deploy an OpenAPI spec before installing
- **deploy-functions** — Deploy functions before installing
