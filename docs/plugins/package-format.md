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
├── marketplace.json              # Copilot marketplace manifest (root — see below)
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
├── <plugin-slug>/                     # Claude plugin (one per plugin)
│   ├── .claude-plugin/plugin.json
│   └── .mcp.json
│
├── cursor-plugins/                    # All Cursor plugins (marketplace pluginRoot)
│   ├── <org-slug>-observability-cursor/   # Cursor observability plugin
│   │   ├── .cursor-plugin/plugin.json
│   │   ├── mcp.json
│   │   └── hooks/
│   │       ├── hooks.json
│   │       └── hook.sh
│   │
│   └── <plugin-slug>-cursor/              # Cursor plugin (one per plugin)
│       ├── .cursor-plugin/plugin.json
│       └── mcp.json
│
├── <plugin-slug>-codex/               # Codex plugin (one per plugin)
│   ├── .codex-plugin/plugin.json
│   └── .mcp.json
│
├── <org-slug>-observability-openclaw/ # OpenClaw observability plugin
│   ├── openclaw.plugin.json
│   ├── package.json
│   ├── index.js                       # plugin module; proxies typed hooks to agenthooks
│   ├── speakeasy.json
│   └── hooks/
│       ├── bootstrap.sh
│       └── bootstrap.ps1
│
├── <org-slug>-observability-copilot/  # Copilot observability plugin
│   ├── plugin.json                    # at the package root, not a vendor dir
│   ├── speakeasy.json
│   └── hooks/
│       ├── hooks.json
│       ├── bootstrap.sh
│       └── bootstrap.ps1
│
└── agent-plugins/<plugin-slug>/       # Portable Agent Plugins 1.0 package
    ├── .cursor-plugin/plugin.json     # Native Cursor overlay
    ├── .codex-plugin/plugin.json      # Native Codex overlay
    ├── .mcp.json                      # Native Codex MCP config
    ├── plugin.json
    ├── mcp.json
    └── skills/
```

Cursor plugins are grouped under the `cursor-plugins/` subdirectory, declared via
`metadata.pluginRoot` in the Cursor `marketplace.json`. Plugin `source` values are
then resolved relative to that root (bare names, no `./` prefix).

Compatible plugins are also published under `agent-plugins/` without changing the
native Cursor or Codex marketplace entries.

## Marketplace manifests

Each platform has a top-level `marketplace.json` that lists all plugins in the repo.

**Claude** (`.claude-plugin/marketplace.json`):

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

**Cursor** (`.cursor-plugin/marketplace.json`) — plugins live under `cursor-plugins/`,
declared via `metadata.pluginRoot`; `source` values are bare names relative to that root:

```json
{
  "name": "<org-slug>-gram",
  "owner": { "name": "Org Name", "email": "" },
  "metadata": { "pluginRoot": "cursor-plugins" },
  "plugins": [
    {
      "name": "<plugin-slug>-cursor",
      "source": "<plugin-slug>-cursor",
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

Directory: `cursor-plugins/<plugin-slug>-cursor/`

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

Directory: `cursor-plugins/<org-slug>-observability-cursor/`

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

### Copilot observability

Directory: `<org-slug>-observability-copilot/`

Copilot is an [Agent Plugins 1.0](https://github.com/agentplugins/agent-plugins-spec) client, so `plugin.json` sits at the package **root** rather than in a vendor subdirectory:

```json
{
  "name": "<org-slug>-observability-copilot",
  "version": "0.<hooks-generator-version>.<publish>",
  "description": "Speakeasy observability hooks for Org Name. ..."
}
```

**`hooks/hooks.json`** — Copilot's own camelCase event names, one entry per event:

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "bash": "bash \"$COPILOT_PLUGIN_ROOT/hooks/bootstrap.sh\" --config=\"$COPILOT_PLUGIN_ROOT/speakeasy.json\" agenthooks run --provider=copilot --timeout=60s",
        "powershell": "& \"$env:COPILOT_PLUGIN_ROOT/hooks/bootstrap.ps1\" \"--config=$env:COPILOT_PLUGIN_ROOT/speakeasy.json\" agenthooks run --provider=copilot --timeout=60s; exit $LASTEXITCODE",
        "timeoutSec": 60
      }
    ]
  }
}
```

Registered events: `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `preToolUse`, `postToolUse`, `postToolUseFailure`, `permissionRequest`, `agentStop`, `subagentStop`, `notification`.

Four differences from the other dialects, each load-bearing:

| Field                 | Copilot                | Why                                                                                                                                                                                                                                                                             |
| --------------------- | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `matcher`             | **absent**             | An empty matcher is a validation error that discards this plugin's entire hook config. Absent means match-all — the semantic the other dialects' `"matcher": ""` was reaching for.                                                                                              |
| `timeoutSec`          | seconds                | Copilot's own default is 30s. Claude and Cursor spell the same field `timeout`.                                                                                                                                                                                                 |
| `bash` / `powershell` | native per-entry split | Copilot runs the `powershell` value as PowerShell directly, so unlike Codex there is no base64 `-EncodedCommand` wrapping. Without it, a Windows machine with no bash fails `preToolUse` — which is fail-closed, so every tool call is denied rather than merely untelemetered. |
| `failClosed`          | **absent**             | Copilot fixes the posture per event: `preToolUse` is fail-closed on any non-timeout error, everything else fails open.                                                                                                                                                          |

The hook config ships at `hooks/hooks.json` **only**. Copilot parses both `<root>/hooks.json` and `<root>/hooks/hooks.json`, so shipping both registers every hook twice.

**Surface support.** Hooks run in Copilot CLI only. VS Code and the Copilot app load the plugin — MCP servers and skills work there — but never fire its hooks. Copilot's cloud agent reads hooks from `.github/hooks/*.json` in the repository and is not targeted by this package.

**Hook chain ordering.** Copilot short-circuits the hook chain on the first deny, so a customer hook that denies before Gram's entry suppresses Gram's telemetry for that call.

### OpenClaw observability

Directory: `<org-slug>-observability-openclaw/`

OpenClaw has no marketplace track: the observability package is the only OpenClaw output a publish produces, and it is a native OpenClaw plugin rather than a hooks-config package. `openclaw.plugin.json` declares the plugin and `package.json` points OpenClaw at the module:

```json
{
  "id": "speakeasy-observability",
  "name": "Speakeasy Observability",
  "description": "Speakeasy observability hooks for OpenClaw.",
  "activation": { "onStartup": true }
}
```

`index.js` is plain JavaScript, not TypeScript: OpenClaw package installs reject a build step. It subscribes the typed plugin hooks and proxies each frame to `agenthooks serve --provider=openclaw`, returning the reply verbatim as the hook handler's return value so `before_tool_call` denials reach OpenClaw intact.

**Conversation-scope hooks require opt-in.** `before_agent_run`, `llm_output` and `agent_end` only fire when the customer sets `plugins.entries.speakeasy-observability.hooks.allowConversationAccess: true`. Without it the package still reports tool calls and session lifecycle, so a half-configured install looks partly working.

**The shim owns its own deadlines.** OpenClaw applies no default hook timeout, so the module enforces them: 10s for the blocking gates (`before_tool_call`, `before_agent_run`) and 30s for observe-only hooks. `--timeout=60s` bounds the serve process, not a handler.

See the [OpenClaw install runbook](../runbooks/openclaw-install.md) for the customer-facing install and the model-auth modes that determine coverage.

### Copilot marketplace manifest

Path: **`marketplace.json` at the repo root** — not a vendor subdirectory.

Copilot probes four locations in order: `marketplace.json`, `.plugin/marketplace.json`, `.github/plugin/marketplace.json`, `.claude-plugin/marketplace.json`. The last one is Claude's, and Copilot happily loads Claude-shaped packages (it reads `.claude-plugin/plugin.json` too) — so **without a root manifest Copilot falls through to Claude's entries and installs the Claude packages**. Those packages' `hooks/hooks.json` is Claude dialect, whose `"matcher": ""` is a validation error that discards the plugin's entire hook config: skills and MCP would appear to work while telemetry was silently dead. The root file claims the highest-priority slot before that can happen.

```json
{
  "name": "<marketplace-name>",
  "owner": { "name": "Org Name", "email": "it@org.example" },
  "plugins": [
    {
      "name": "<org-slug>-observability-copilot",
      "displayName": "Org Name Observability",
      "source": "./<org-slug>-observability-copilot",
      "description": "Required: Speakeasy observability hooks for Org Name."
    },
    {
      "name": "<plugin-slug>",
      "displayName": "Plugin Name",
      "source": "./agent-plugins/<plugin-slug>",
      "description": "..."
    }
  ]
}
```

Three constraints, each verified against Copilot CLI 1.0.80:

| Constraint                                                     | Why                                                                                                                                                                                                                    |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `owner` is required                                            | A manifest without it fails `copilot plugin marketplace add` outright with `Invalid marketplace.json: owner: Required`.                                                                                                |
| Entry `name` must equal the package's own `plugin.json` `name` | Copilot installs to `~/.copilot/installed-plugins/<marketplace>/<name>/` and resolves the plugin by that name. A mismatch installs cleanly and is then never loaded.                                                   |
| Only Agent-Plugins-compatible plugins get an entry             | Feature plugins reach Copilot solely through `agent-plugins/<plugin-slug>/`, which the generator omits for plugins failing the portability gate. Listing one anyway would point at a directory that was never written. |

Copilot will not install a plugin that is absent from the manifest, even if its files are already present under `installed-plugins/` and it is listed in `enabledPlugins` — the manifest is the authority, not the filesystem.

### MCP servers and skills on Copilot

MCP servers and skills reach Copilot through the platform-neutral Agent Plugins 1.0 package under `agent-plugins/<plugin-slug>/`, not through a Copilot-specific package. That package carries `plugin.json`, `mcp.json` and `skills/`, is credential-free, and installs with `copilot plugin install OWNER/REPO:agent-plugins/<plugin-slug>` or from an extracted ZIP via `copilot --plugin-dir`. Plugins that fail the portability gate (a server needing a Gram credential, environment-backed headers, or a non-HTTPS off-loopback URL) are omitted from that directory and are therefore not installable on Copilot.

## README

The auto-generated `README.md` contains:

- Per-platform installation instructions (Claude, Cursor, Codex)
- A table of all plugins with server counts and descriptions
- A note that the observability plugin must be installed alongside MCP plugins
- A notice that the repo is read-only and auto-managed by Gram

## Single-plugin ZIP download

`downloadPluginPackage` returns a ZIP containing only the files for one plugin on one platform. Native ZIPs mirror their platform package layout. The `agent-plugin` platform returns a credential-free Agent Plugins 1.0 package rooted at `plugin.json`.

`downloadObservabilityPlugin` returns the observability ZIP for a single platform — `claude`, `cursor`, `codex`, `opencode` or `copilot` — minting a fresh hooks-scoped API key each time. The ZIP is rooted at the package files themselves (no per-platform subdirectory), so `copilot --plugin-dir <extracted>` works directly.
