---
cwd: ../..
---

# Plugins — Package Format

This doc describes the exact files Gram generates for each supported platform when a plugin is published or downloaded as a ZIP.

## Repository layout (published to GitHub)

When plugins are published, all platform configs land in a single repo. The root contains per-platform subdirectories plus marketplace manifests:

```
<project-slug>-plugins/
├── README.md
│
├── .claude-plugin/
│   └── marketplace.json          # Claude marketplace manifest
│
├── .cursor-plugin/
│   └── marketplace.json          # Cursor marketplace manifest
│
├── .agents/plugins/
│   └── marketplace.json          # Codex marketplace manifest
│
├── <org-slug>-observability/          # Claude observability plugin
│   ├── .claude-plugin/plugin.json
│   ├── .mcp.json
│   └── hooks/
│       ├── hooks.json
│       └── hook.sh
│
├── <org-slug>-observability-cursor/   # Cursor observability plugin
│   ├── .cursor-plugin/plugin.json
│   ├── mcp.json
│   └── hooks/
│       ├── hooks.json
│       └── hook.sh
│
├── <plugin-slug>/                     # Claude plugin (one per plugin)
│   ├── .claude-plugin/plugin.json
│   └── .mcp.json
│
├── <plugin-slug>-cursor/              # Cursor plugin (one per plugin)
│   ├── .cursor-plugin/plugin.json
│   └── mcp.json
│
└── <plugin-slug>-codex/               # Codex plugin (one per plugin)
    ├── .codex-plugin/plugin.json
    ├── .mcp.json
    └── plugin.json
```

## Marketplace manifests

Each platform has a top-level `marketplace.json` that lists all plugins in the repo.

**Claude / Cursor** (`marketplace.json`):

```json
{
  "name": "<org-slug>-gram",
  "owner": { "name": "Org Name", "email": "" },
  "plugins": [
    {
      "name": "<plugin-slug>",
      "source": "./<plugin-slug>",
      "description": "Plugin description"
    }
  ]
}
```

**Codex** (`.agents/plugins/marketplace.json`):

```json
{
  "name": "<org-slug>-gram",
  "interface": {
    "displayName": "Org Name Plugins",
    "shortDescription": ""
  },
  "plugins": [
    {
      "name": "<plugin-slug>-codex",
      "source": { "source": "local", "path": "./<plugin-slug>-codex" },
      "policy": {
        "installation": "AVAILABLE",
        "authentication": "NONE"
      }
    }
  ]
}
```

The `authentication` field in Codex is `"REQUIRED"` for private servers and `"NONE"` for public ones.

## Claude plugin

Directory: `<plugin-slug>/`

### `.claude-plugin/plugin.json`

```json
{
  "name": "<plugin-slug>",
  "description": "Plugin description",
  "version": "1.0.0",
  "author": "Org Name",
  "userConfig": [
    {
      "variableName": "MY_ENV_VAR",
      "displayName": "My API Key",
      "type": "string",
      "description": "My API Key"
    }
  ]
}
```

The `userConfig` array is populated only for public servers that require user-supplied env vars. Private servers with a Gram API key have no `userConfig` (the key is embedded directly in the MCP config headers).

### `.mcp.json`

```json
{
  "mcpServers": {
    "Display Name": {
      "type": "http",
      "url": "https://app.getgram.ai/mcp/<toolset-mcp-slug>",
      "headers": {
        "Authorization": "Bearer gsk_..."
      }
    }
  }
}
```

For public servers, headers are omitted and an env var reference is used for authentication:

```json
{
  "mcpServers": {
    "Display Name": {
      "type": "http",
      "url": "https://app.getgram.ai/mcp/<toolset-mcp-slug>",
      "headers": {
        "Authorization": "Bearer ${GRAM_API_KEY}"
      }
    }
  }
}
```

## Cursor plugin

Directory: `<plugin-slug>-cursor/`

### `.cursor-plugin/plugin.json`

```json
{
  "name": "<plugin-slug>-cursor",
  "displayName": "Plugin Name",
  "description": "Plugin description",
  "version": "1.0.0",
  "author": "Org Name"
}
```

### `mcp.json`

```json
{
  "mcpServers": {
    "Display Name": {
      "url": "https://app.getgram.ai/mcp/<toolset-mcp-slug>",
      "headers": {
        "Authorization": "Bearer gsk_..."
      }
    }
  }
}
```

## Codex plugin

Directory: `<plugin-slug>-codex/`

### `.codex-plugin/plugin.json`

```json
{
  "name": "<plugin-slug>-codex",
  "version": "1.0.0",
  "description": "Plugin description",
  "interface": "mcp",
  "mcpServers": ["Display Name"]
}
```

### `.mcp.json`

```json
{
  "mcpServers": {
    "Display Name": {
      "type": "http",
      "url": "https://app.getgram.ai/mcp/<toolset-mcp-slug>",
      "bearer_token_env_var": "GRAM_API_KEY"
    }
  }
}
```

Codex always uses `bearer_token_env_var` (a reference to an env var) rather than embedding the key directly.

### `plugin.json` (marketplace metadata)

```json
{
  "name": "<plugin-slug>-codex",
  "description": "Plugin description"
}
```

## Observability plugin

The observability plugin is included in every publish (once per org per platform) and ships **before** any MCP server plugins in the marketplace.

### Claude observability

Directory: `<org-slug>-observability/`

**`.claude-plugin/plugin.json`**

```json
{
  "name": "<org-slug>-observability",
  "description": "Required: Gram observability hooks for Org Name.",
  "version": "1.0.0",
  "author": "Org Name"
}
```

**`hooks/hooks.json`** — registered events:

```json
{
  "hooks": {
    "PreToolUse": [{ "type": "command", "command": "hooks/hook.sh" }],
    "PostToolUse": [{ "type": "command", "command": "hooks/hook.sh" }],
    "PostToolUseFailure": [{ "type": "command", "command": "hooks/hook.sh" }],
    "SessionStart": [{ "type": "command", "command": "hooks/hook.sh" }],
    "SessionEnd": [{ "type": "command", "command": "hooks/hook.sh" }],
    "UserPromptSubmit": [{ "type": "command", "command": "hooks/hook.sh" }],
    "Stop": [{ "type": "command", "command": "hooks/hook.sh" }],
    "Notification": [{ "type": "command", "command": "hooks/hook.sh" }]
  }
}
```

**`hooks/hook.sh`** — forwards event JSON to Gram:

```bash
#!/usr/bin/env bash
curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <hooks-api-key>" \
  -d @- \
  "https://app.getgram.ai/rpc/hooks.claude"
```

### Cursor observability

Directory: `<org-slug>-observability-cursor/`

Cursor's hook events differ from Claude's — they use camelCase and Cursor-specific names:

```json
{
  "hooks": {
    "beforeSubmitPrompt":   [...],
    "stop":                 [...],
    "afterAgentResponse":   [...],
    "afterAgentThought":    [...],
    "preToolUse":           [...],
    "postToolUse":          [...],
    "postToolUseFailure":   [...],
    "beforeMCPExecution":   [...],
    "afterMCPExecution":    [...]
  }
}
```

Cursor's hook script posts to `/rpc/hooks.cursor` with an additional `Gram-Project` header (Cursor's endpoint requires it):

```bash
#!/usr/bin/env bash
curl -s -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <hooks-api-key>" \
  -H "Gram-Project: <project-slug>" \
  -d @- \
  "https://app.getgram.ai/rpc/hooks.cursor"
```

## README

The auto-generated `README.md` contains:

- Per-platform installation instructions (Claude, Cursor, Codex)
- A table of all plugins with server counts and descriptions
- A note that the observability plugin must be installed alongside MCP plugins
- A notice that the repo is read-only and auto-managed by Gram

## Single-plugin ZIP download

`downloadPluginPackage` returns a ZIP containing only the files for one plugin on one platform. The ZIP structure mirrors the subdirectory in the published repo (e.g. for Claude, it contains `<plugin-slug>/.claude-plugin/plugin.json` and `<plugin-slug>/.mcp.json`).

`downloadObservabilityPlugin` returns the observability ZIP for a single platform (Claude or Cursor), minting a fresh hooks-scoped API key each time.
