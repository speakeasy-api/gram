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
  "enabledPlugins": {
    "{{GRAM_CLAUDE_PLUGIN_NAME}}@{{GRAM_MARKETPLACE_NAME}}": true
  }
}`,
        language: "json",
        requiresApiKey: true,
      },
    ],
  },
  {
    id: "claude-cowork",
    name: "Claude Cowork",
    description: "Autonomous AI desktop assistant",
    icon: "claude",
    connected: false,
    setupSteps: [
      {
        title: "Open Organization settings on Claude.ai",
        description:
          "Sign in to claude.ai as an org admin and navigate to Organization settings → Plugins. Cowork talks directly to GitHub via its own GitHub App — there's no marketplace URL to register.",
        helpLink: {
          url: "https://claude.ai/",
          linkLabel: "claude.ai",
          sentence: "Sign in to {LINK} as an org admin",
        },
      },
      {
        title: "Click Add plugin → Sync from GitHub",
        description:
          'Open the "Add plugins" dialog and pick "Sync from GitHub". The other two options (Anthropic sources, Upload .zip) aren\'t needed — your repo is the source of truth.',
        screenshot: {
          src: "/setup/claude-cowork-add-plugins.png",
          alt: "Claude.ai Add plugins dialog with three options: Browse Anthropic sources, Sync from GitHub (highlighted), Upload a file",
          caption: 'Pick "Sync from GitHub".',
        },
      },
      {
        title: "Install Claude's GitHub App, then select your repo",
        description:
          "Only private and internal repos appear here. If your repo isn't listed, click \"Install the Claude GitHub app\" at the bottom of the picker, grant access to your org's repo, then return — it'll show up. Select the repo slug below to finish.",
        screenshot: {
          src: "/setup/claude-cowork-sync-from-github.png",
          alt: 'Claude.ai "Sync from GitHub" picker showing "No repositories found" and an "Install the Claude GitHub app" link at the bottom',
          caption:
            'If you see "No repositories found", install the Claude GitHub App on your repo via the link at the bottom.',
        },
        code: `{{GRAM_REPO_OWNER}}/{{GRAM_REPO_NAME}}`,
        language: "text",
        helpLink: {
          url: "https://support.claude.com/en/articles/13837433-manage-claude-cowork-plugins-for-your-organization",
          linkLabel: "Cowork setup guide",
          sentence: "See the {LINK} for GitHub App installation details",
        },
      },
      {
        title: "Mark the observability plugin as Required",
        description:
          "After the repo syncs, your plugins appear in a table on Claude.ai. Find the observability plugin row (slug below), open its Default access dropdown, and set it to Required. That pre-installs it for every org member and prevents them from disabling it — so tool events flow to Gram without per-user opt-in.",
        screenshot: {
          src: "/setup/claude-cowork-set-required.png",
          alt: "Claude.ai plugin access dropdown showing four options (Available to install, Installed by default, Not available, Required) with Required selected",
          caption:
            'Open the Default access dropdown on the observability plugin row and select "Required".',
        },
        code: `{{GRAM_CLAUDE_PLUGIN_NAME}}`,
        language: "text",
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
        code: `codex plugin marketplace add {{GRAM_MARKETPLACE_URL}}`,
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
  {
    id: "copilot",
    name: "GitHub Copilot",
    description: "Microsoft / GitHub AI pair programmer",
    icon: "copilot",
    connected: false,
    available: false,
    setupSteps: [],
  },
  {
    id: "gemini",
    name: "Gemini",
    description: "Google's coding assistant",
    icon: "gemini",
    connected: false,
    available: false,
    setupSteps: [],
  },
  {
    id: "glean",
    name: "Glean",
    description: "Enterprise work assistant",
    icon: "glean",
    connected: false,
    available: false,
    setupSteps: [],
  },
  {
    id: "bedrock",
    name: "AWS Bedrock",
    description: "Amazon foundation-model gateway",
    icon: "bedrock",
    connected: false,
    available: false,
    setupSteps: [],
  },
];
