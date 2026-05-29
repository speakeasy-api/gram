import type { AgentPlatform } from "./types";

export const AGENT_PLATFORMS: AgentPlatform[] = [
  {
    id: "claude",
    name: "Claude Code",
    description: "Anthropic CLI & IDE agent",
    icon: "claude",
    connected: false,
    setupSteps: [
      {
        title: "Plan check",
        description:
          "Managed Settings are only available on Teams and Enterprise Claude plans. Confirm your plan so we can pick the right setup flow.",
        helpLink: {
          url: "https://claude.ai/admin-settings/billing",
          linkLabel: "Claude.ai",
          sentence: "Visit {LINK} to check your plan",
        },
        eligibility: {
          question: "Do you have a Teams or Enterprise Claude plan?",
          blockedTitle: "Per-user setup flow coming soon",
          blockedDescription:
            "Without a Teams or Enterprise plan, Managed Settings can't be applied centrally. We're building a per-user setup flow for this case — for now, skip Claude Code or upgrade your plan to continue.",
        },
      },
      {
        title: "Open Claude Code managed settings",
        description:
          "Sign in as an org admin and open the Managed Settings (settings.json) editor. This applies the policy to every developer in your org.",
        screenshot: {
          src: "/setup/claude-managed-settings.png",
          alt: "Claude Code Managed settings panel in claude.ai admin with a Manage button",
          caption:
            "Find the Managed settings (settings.json) row in claude.ai admin and click Manage to open the editor.",
        },
        helpLink: {
          url: "https://claude.ai/admin-settings/claude-code",
          linkLabel: "Claude.ai Managed Settings",
          sentence: "Open {LINK} to get started",
        },
      },
      {
        title: "Update Managed settings on Claude.ai",
        description:
          "Update your managed settings to the following. Note: if you already have managed settings set, please combine the snippets to ensure that your existing configuration continues to persist.",
        screenshot: {
          src: "/setup/claude-managed-settings-editor.png",
          alt: "Claude Code Managed settings JSON editor dialog with Update settings button",
          caption: 'Paste in the JSON below and click "Update settings"',
        },
        code: `{
  "channelsEnabled": true,
  "enabledPlugins": {
    "gram-hooks@gram": true
  },
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "https://app.getgram.ai/rpc/hooks.otel",
    "OTEL_EXPORTER_OTLP_HEADERS": "Gram-Project=default,Gram-Key={{GRAM_API_KEY}}",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/json",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp"
  },
  "extraKnownMarketplaces": {
    "gram": {
      "autoUpdate": true,
      "source": {
        "source": "git",
        "url": "https://github.com/speakeasy-api/gram.git"
      }
    }
  }
}`,
        language: "json",
        requiresApiKey: true,
      },
    ],
  },
  {
    id: "codex",
    name: "OpenAI Codex",
    description: "OpenAI CLI agent",
    icon: "codex",
    connected: false,
    setupSteps: [
      {
        title: "Install the Speakeasy wrapper",
        description: "Add Speakeasy as a wrapper around Codex CLI invocations.",
        code: `npm install -g @speakeasy-api/codex-hooks@latest`,
        language: "bash",
      },
      {
        title: "Configure environment",
        description:
          "Set environment variables for Codex to route through Speakeasy.",
        code: `export SPEAKEASY_API_KEY="sk_live_speakeasy_xxxxxxxxxxxxx"\nexport SPEAKEASY_CODEX_HOOK=true`,
        language: "bash",
      },
      {
        title: "Verify the connection",
        description:
          "Run a Codex session and confirm traffic appears in the Speakeasy dashboard.",
      },
    ],
  },
  {
    id: "cursor",
    name: "Cursor",
    description: "AI-powered code editor",
    icon: "cursor",
    connected: false,
    setupSteps: [
      {
        title: "Open Cursor settings",
        description:
          "Navigate to Cursor Settings > MCP and add a new global MCP server.",
        code: `{\n  "mcpServers": {\n    "speakeasy": {\n      "command": "npx",\n      "args": ["-y", "@speakeasy-api/hooks@latest"]\n    }\n  }\n}`,
        language: "json",
      },
      {
        title: "Add your API key",
        description:
          "Set the Speakeasy API key in your environment or Cursor settings.",
        code: `export SPEAKEASY_API_KEY="sk_live_speakeasy_xxxxxxxxxxxxx"`,
        language: "bash",
      },
      {
        title: "Verify the connection",
        description:
          "Open Cursor and confirm the Speakeasy MCP server appears as connected.",
      },
    ],
  },
];
