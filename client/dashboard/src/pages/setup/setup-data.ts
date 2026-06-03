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
          "Add your private marketplace to managed settings so every developer in your org gets the observability plugin automatically. The OTEL env block also pushes a Gram API key to every install so tool traffic lands in your dashboard. If you already have managed settings, merge this block into the existing JSON.",
        screenshot: {
          src: "/setup/claude-managed-settings-editor.png",
          alt: "Claude Code Managed settings JSON editor dialog with Update settings button",
          caption: 'Paste in the JSON below and click "Update settings"',
        },
        code: `{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "https://app.getgram.ai/rpc/hooks.otel",
    "OTEL_EXPORTER_OTLP_HEADERS": "Gram-Project=default,Gram-Key={{GRAM_API_KEY}}",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/json",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp"
  },
  "extraKnownMarketplaces": {
    "{{GRAM_MARKETPLACE_NAME}}": {
      "autoUpdate": true,
      "source": {
        "source": "git",
        "url": "{{GRAM_MARKETPLACE_URL}}"
      }
    }
  },
  "plugins": {
    "required": ["{{GRAM_CLAUDE_PLUGIN_NAME}}@{{GRAM_MARKETPLACE_NAME}}"]
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
        title: "Register the Gram plugin marketplace",
        description:
          "Register your org's private marketplace with Codex. The repo URL points at the GitHub repository Gram just published for you.",
        code: `codex plugin marketplace add {{GRAM_REPO_URL}}`,
        language: "bash",
        helpLink: {
          url: "https://developers.openai.com/codex/plugins/build",
          linkLabel: "Codex Plugin docs",
          sentence: "See {LINK} for how marketplaces work in Codex",
        },
      },
      {
        title: "Enable hooks and the plugin in ~/.codex/config.toml",
        description:
          "Hooks live behind a feature flag and the plugin must be explicitly enabled. Add the following to ~/.codex/config.toml — the plugin and marketplace names are filled in from your published repo.",
        code: `features.hooks = true
features.plugin_hooks = true

[plugins."{{GRAM_CODEX_PLUGIN_NAME}}@{{GRAM_MARKETPLACE_NAME}}"]
enabled = true`,
        language: "toml",
      },
      {
        title: "Approve hooks in Codex",
        description:
          "After restarting Codex, open Settings → Hooks and enable each hook listed under the observability plugin. Codex requires manual approval for each hook event before it will fire.",
        helpLink: {
          url: "https://developers.openai.com/codex/hooks",
          linkLabel: "Codex Hooks docs",
          sentence: "See {LINK} for details on hook approval",
        },
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
        title: "Open your Cursor team dashboard",
        description:
          "Sign in to cursor.com/dashboard as a team admin. The marketplace you import here syncs to every team member automatically — no per-user install needed.",
        helpLink: {
          url: "https://cursor.com/dashboard",
          linkLabel: "cursor.com/dashboard",
          sentence: "Go to {LINK} to manage your team's plugins",
        },
      },
      {
        title: "Import the Gram marketplace",
        description:
          "In the Cursor team dashboard, navigate to Settings → Plugins → Import and paste your private marketplace repo URL below. Cursor reads the plugin manifest from the repo and makes its plugins available to your team.",
        code: `{{GRAM_REPO_URL}}`,
        language: "text",
      },
      {
        title: "Mark the observability plugin as required",
        description:
          "In Cursor's team marketplace settings, mark the observability plugin (slug below) as required so tool events flow to Gram for every team member without per-user setup.",
        code: `{{GRAM_CURSOR_PLUGIN_NAME}}`,
        language: "text",
      },
    ],
  },
];
