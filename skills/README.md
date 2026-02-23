# Gram Agent Skills

Agent skills for [Claude Code](https://claude.com/claude-code) that provide guided workflows for deploying APIs, functions, and MCP servers with the [Gram CLI](https://docs.getgram.ai).

## Installation

```bash
claude skills install --path /path/to/gram/skills
```

Or install directly from the repository:

```bash
claude skills install --url https://github.com/gram-ai/gram --subdir skills
```

## Skills

| Skill | Description |
|---|---|
| **gram-context** | Foundation skill — Gram CLI reference, authentication, and configuration |
| **deploy-openapi** | Deploy OpenAPI v3 specs to Gram (stage+push and upload workflows) |
| **deploy-functions** | Deploy Gram Functions zip files to the platform |
| **install-mcp-server** | Install Gram toolsets as MCP servers in Claude Code, Claude Desktop, Cursor, or Gemini CLI |
| **check-deployment-status** | Monitor, inspect, and debug Gram deployments |
| **write-gram-function** | Author serverless tools with the `@gram-ai/functions` TypeScript SDK |

## How It Works

When you ask Claude Code to perform a Gram-related task (e.g., "deploy my OpenAPI spec to Gram"), the relevant skill activates automatically and guides the workflow — running the right CLI commands, validating inputs, and troubleshooting errors.

## Prerequisites

- [Gram CLI](https://docs.getgram.ai) installed (`gram --version`)
- Authenticated with Gram (`gram auth` or `GRAM_API_KEY` env var)
