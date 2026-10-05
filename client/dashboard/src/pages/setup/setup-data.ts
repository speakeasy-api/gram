import { getCursorInstallCommand } from "@/lib/cursor-install-command";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
import {
  AGENT_PROVIDERS,
  ACTIVE_AGENT_PROVIDER_IDS,
  COMING_SOON_AGENT_PROVIDER_IDS,
  type AgentProviderId,
} from "@/components/agent-providers/agent-providers";
import { agentEgressHosts, getServerURL } from "@/lib/utils";
import type { AgentPlatform } from "./types";

// Claude Code reads the same keys from Managed Settings (org rollout) and from
// a user's ~/.claude/settings.json (personal plans), so both paths share it.
const claudeCodeSettingsJSON = (origin: string) => `{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "${origin}/otel",
    "OTEL_EXPORTER_OTLP_HEADERS": "Gram-Project={{GRAM_PROJECT_SLUG}},Gram-Key={{GRAM_API_KEY}}",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "FORCE_AUTOUPDATE_PLUGINS": "1"
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
}`;

// Setup copy names the host the reader is on (app.getgram.ai or
// ai.speakeasy.com): every platform host serves the OTLP and hooks endpoints.
const setupAgentPlatforms = (
  origin: string,
  egressHosts: string[],
): Array<{
  id: AgentProviderId;
  setupSteps: AgentPlatform["setupSteps"];
}> => [
  {
    id: "claude",
    setupSteps: [
      {
        title: "Plan check",
        description:
          "Claude.ai server-managed settings require Team or Enterprise and an Owner or Primary Owner. Endpoint-managed policy files and MDM are separate mechanisms. Confirm your plan to choose a setup flow.",
        helpLink: {
          url: "https://claude.ai/admin-settings/billing",
          linkLabel: "Claude.ai",
          sentence: "Visit {LINK} to check your plan",
        },
        eligibility: {
          question: "Do you have a Team or Enterprise Claude plan?",
          personalSteps: [
            {
              title: "Add the settings to each developer's Claude Code",
              description: [
                "For personal setup, each developer merges this block into ",
                {
                  code: "~/.claude/settings.json",
                  fallback: "~/.claude/settings.json",
                },
                `, preserving existing values. It registers the marketplace, enables the plugin, and configures logs, metrics, and beta traces for export. Higher-precedence policy can override user settings. The API key and token-bearing marketplace URL are secrets: share privately and never commit them. ${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}`,
              ],
              code: claudeCodeSettingsJSON(origin),
              language: "json",
              requiresApiKey: true,
              helpLink: {
                url: "https://code.claude.com/docs/en/settings#settings-precedence",
                linkLabel: "Claude Code settings precedence",
                sentence: "Review {LINK} before merging",
              },
            },
            {
              title: "Start a new Claude Code session and verify",
              description:
                "Start a new session to load the marketplace and plugin. Submit a prompt and run a tool, then confirm claude_code.session.count, claude_code.user_prompt, and trace spans separately in Speakeasy. Traces are beta; valid settings alone do not prove delivery. For later plugin updates, use /reload-plugins or start a new session.",
              helpLink: {
                url: "https://code.claude.com/docs/en/monitoring-usage#traces-beta",
                linkLabel: "Claude Code monitoring",
                sentence: "See {LINK} for verification and beta traces",
              },
            },
          ],
        },
      },
      {
        title: "Open Claude Code managed settings",
        description:
          "Sign in as an Owner or Primary Owner, then open Admin settings → Claude Code → Managed settings. Non-Owner admins cannot edit these settings. Server-managed policy applies to eligible signed-in sessions connecting directly to api.anthropic.com; third-party provider variables or a non-default ANTHROPIC_BASE_URL skip the settings fetch.",
        screenshot: {
          src: "/setup/claude-managed-settings.png",
          alt: "Claude Code Managed settings panel in claude.ai admin with a Manage button",
          caption:
            "Find the Managed settings (settings.json) row in claude.ai admin and click Manage to open the editor.",
        },
        helpLink: {
          url: "https://code.claude.com/docs/en/server-managed-settings#configure-server-managed-settings",
          linkLabel: "Claude.ai server-managed settings guide",
          sentence: "Open {LINK} to get started",
        },
      },
      {
        title: "Update Managed settings on Claude.ai",
        description:
          "Merge this block into existing server-managed JSON to register the marketplace, enable the plugin, and configure logs, metrics, and beta trace export for eligible sessions. Treat both the API key and token-bearing marketplace URL as secrets; never commit or distribute them publicly. Settings are fetched at the next startup or hourly poll, not instantly.",
        screenshot: {
          src: "/setup/claude-managed-settings-editor.png",
          alt: "Claude Code Managed settings JSON editor dialog with Update settings button",
          caption: 'Paste in the JSON below and click "Update settings"',
        },
        code: claudeCodeSettingsJSON(origin),
        language: "json",
        requiresApiKey: true,
      },
      {
        title: "Verify in a new Claude Code session",
        description:
          "Start a new eligible signed-in session and confirm the marketplace and plugin load. Submit a prompt and run a tool, then check claude_code.session.count, claude_code.user_prompt, and beta trace spans separately in Speakeasy. Auto-update is not immediate; use /reload-plugins or a new session to load updated plugins.",
        helpLink: {
          url: "https://code.claude.com/docs/en/monitoring-usage#quick-start",
          linkLabel: "Claude Code monitoring guide",
          sentence: "Use the {LINK} to verify exports",
        },
      },
    ],
  },
  {
    id: "claude-cowork",
    setupSteps: [
      {
        title: "Plan check",
        description:
          "Organization plugin settings require Team or Enterprise and an Owner or Primary Owner. Personal plugin installation requires Pro or Max, not Free.",
        helpLink: {
          url: "https://claude.ai/admin-settings/billing",
          linkLabel: "Claude.ai",
          sentence: "Visit {LINK} to check your plan",
        },
        eligibility: {
          question: "Do you have a Team or Enterprise Claude plan?",
          personalSteps: [
            {
              title: "Download the observability plugin",
              description:
                "On Pro or Max, each person can add a personal marketplace, including a private GitHub repository, or upload a plugin ZIP. For a private marketplace, connect GitHub when prompted and grant the Claude GitHub App repository access. These are personal installations, not organization-required rollout. This ZIP alternative requires a Speakeasy org admin and enabled project observability to download; recipients need not be admins. It embeds a hooks credential: distribute privately, never commit or upload publicly, and coordinate rotation and replacement. A new download does not revoke keys in older copies.",
              download: {
                platform: "claude",
                label: "Download for Claude Cowork",
              },
            },
            {
              title: "Upload it in Claude",
              description: `On Pro or Max, open Customize → Plugins → Add → Upload plugin and select the ZIP. After a new publish, download a fresh ZIP, replace the uploaded plugin, and start a new Cowork session. Reusing the old ZIP does not update it. Marketplace installs update from their source and can use Sync automatically; they do not use this ZIP replacement flow. ${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}`,
              helpLink: {
                url: "https://claude.com/docs/plugins/overview#find-and-add-a-plugin",
                linkLabel: "Install plugins in Cowork",
                sentence: "See {LINK} for details",
              },
            },
            {
              title: "Verify Cowork hooks in a new session",
              description:
                "Run a tool in a Cowork session, not ordinary chat, where hooks are ignored. Confirm real events arrive in Speakeasy: successful upload does not prove hook execution, bootstrap binary downloads, credentials, or network access. The ZIP embeds hooks authentication, not connector sign-in. Native Cowork OTEL monitoring is a separate Team/Enterprise feature, not included on Pro/Max. Verify hook behavior on the surface and version you use.",
              helpLink: {
                url: "https://claude.com/docs/plugins/platform-support",
                linkLabel: "Plugin platform support",
                sentence: "Check {LINK} for hook support and limitations",
              },
            },
          ],
        },
      },
      {
        title: "Open Organization settings on Claude.ai",
        description:
          "Sign in to claude.ai as an Owner or Primary Owner and navigate to Organization settings → Plugins & skills → Marketplaces → Add plugins → Sync from GitHub. Cowork syncs through its own GitHub App.",
        helpLink: {
          url: "https://claude.ai/",
          linkLabel: "claude.ai",
          sentence: "Sign in to {LINK} as an Owner or Primary Owner",
        },
      },
      {
        title: "Click Add plugins → Sync from GitHub",
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
          "Organization marketplaces on github.com require a private or internal repository. The importer needs accepted GitHub repository access and permission to grant the Claude GitHub App access; ask the repository owner if needed. Select the repo below. Organization members do not each need a GitHub invitation for this rollout.",
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
        description: [
          "After the repo syncs, your plugins appear in a table on Claude.ai. Find ",
          {
            code: "{{GRAM_CLAUDE_PLUGIN_NAME}}",
            fallback: "the observability plugin",
          },
          ` in the plugin list and set Default access → Required. That pre-installs it for org members and prevents disabling or removal. Required installation does not prove hook execution or event delivery; verify both in a new Cowork session. ${PERSONAL_ACCOUNT_GOVERNANCE_NOTE}`,
        ],
        screenshot: {
          src: "/setup/claude-cowork-set-required.png",
          alt: "Claude.ai plugin access dropdown showing four options (Available to install, Installed by default, Not available, Required) with Required selected",
          caption:
            'Open the Default access dropdown on the observability plugin row and select "Required".',
        },
      },
      {
        title: "Enable OTEL export",
        description:
          "Native monitoring requires Team or Enterprise and Claude Desktop 1.1.4173 or later. Open Admin settings → Cowork → Monitoring and enter the values below. This is separate from plugin hooks; verify the endpoint, project, and headers against actual received events.",
        fields: [
          {
            label: "OTLP endpoint",
            value: `${origin}/rpc/hooks.otel`,
          },
          { label: "OTLP protocol", value: "http/json" },
          {
            label: "OTLP headers",
            value: "Gram-Project=default,Gram-Key={{GRAM_API_KEY}}",
            requiresApiKey: true,
          },
        ],
        afterFields:
          "Save the settings and start a new Cowork session to verify receipt. The native exporter automatically allowlists the collector hostname.",
        helpLink: {
          url: "https://claude.com/docs/cowork/monitoring#setup",
          linkLabel: "Cowork monitoring setup",
          sentence: "See {LINK} for native export requirements",
        },
        requiresApiKey: true,
      },
      {
        title: "Verify hook and bootstrap network access",
        description:
          "Native OTEL automatically allowlists its collector hostname; it does not require a manual egress exception. Hook scripts and bootstrap binary downloads are separate traffic. If they are blocked, review Admin settings → Capabilities → Network egress and allow only the destinations they need, including the hook endpoints below.",
        fields: egressHosts.map((host) => ({
          label: "Additional allowed domains",
          value: host,
        })),
        afterFields:
          "Start a new Cowork session, run a tool, and confirm hook events and native monitoring separately in Speakeasy. A successful plugin install or a single allowlisted domain does not prove bootstrap downloads and hooks can run.",
      },
    ],
  },
  {
    id: "codex",
    setupSteps: [
      {
        title: "Deploy the Speakeasy device agent via MDM",
        description:
          "Codex is instrumented centrally by the Speakeasy device agent — this covers the Codex CLI and Codex mode in the ChatGPT desktop app, which OpenAI merged the standalone Codex app into. Chat and Work modes in that same app are not covered here; they are captured through the OpenAI Compliance API integration instead. Roll the agent out through your MDM (Jamf, Iru (formerly Kandji), Intune, ...) using the Fleet (MDM) path, then select Codex as a managed platform. The agent installs the observability plugin and configures authenticated OpenTelemetry logs, metrics, and traces for every managed developer with no per-user setup. Restart Codex after the first policy sync.",
        helpLink: {
          url: "{{GRAM_DEVICE_AGENT_URL}}",
          linkLabel: "device agent setup",
          sentence:
            "Follow the Fleet (MDM) walkthrough on the {LINK} page, then hand the profile to your MDM admin.",
        },
      },
    ],
  },
  {
    id: "cursor",
    setupSteps: [
      {
        title: "Plan check",
        description:
          "Team marketplaces are only available on Cursor Teams and Enterprise plans. Confirm your plan so we can pick the right setup flow.",
        helpLink: {
          url: "https://cursor.com/dashboard",
          linkLabel: "cursor.com/dashboard",
          sentence: "Visit {LINK} to check your plan",
        },
        eligibility: {
          question: "Do you have a Cursor Teams or Enterprise plan?",
          personalSteps: [
            {
              title: "Download the observability plugin",
              description:
                "Individual Cursor plans use a local plugin instead of a team marketplace. Downloading requires a Speakeasy org admin and enabled project observability; recipients need not be admins. The ZIP embeds a hooks credential: distribute privately, never commit or upload publicly, and coordinate rotation and replacement. A new download does not revoke keys in older copies.",
              download: {
                platform: "cursor",
                label: "Download for Cursor",
              },
            },
            {
              title: "Unzip it into Cursor's local plugins folder",
              description:
                "On macOS/Linux, use Bash with unzip and Python 3 installed; this is not a native PowerShell command. Windows also needs a working Bash hook runtime and verified paths. After publishing, download a fresh ZIP and use its actual filename and location (browsers may add a suffix). Replace the local files, then reload; rerunning against an old ZIP does not update it.",
              code: getCursorInstallCommand({
                pluginName: "{{GRAM_CURSOR_PLUGIN_NAME}}",
                archiveName: "observability-cursor.zip",
                requireHooks: true,
              }),
              language: "bash",
            },
            {
              title: "Reload Cursor",
              description:
                "Run Developer: Reload Window, then open Customize from the sidebar and check the expected plugin components. For a team-managed account, ask an admin to enable Allow Local Plugin Imports at Dashboard → Settings → Security & Identity → Marketplace and Plugins (off by default on Enterprise). An installed marketplace plugin with the same name takes precedence over the local copy. Run a tool and confirm hook events arrive in Speakeasy; discovery alone does not prove delivery.",
              helpLink: {
                url: "https://cursor.com/docs/plugins#test-plugins-locally",
                linkLabel: "Cursor plugins docs",
                sentence: "See the {LINK} for local plugins",
              },
            },
          ],
        },
      },
      {
        title: "Open your Cursor team dashboard",
        description:
          "Sign in to cursor.com/dashboard as a team admin. Importing a marketplace makes plugins available; choose an installation mode separately to control installation for the intended team audience.",
        helpLink: {
          url: "https://cursor.com/dashboard",
          linkLabel: "cursor.com/dashboard",
          sentence: "Go to {LINK} to manage your team's plugins",
        },
      },
      {
        title: "Import the Speakeasy marketplace",
        description:
          "Navigate to Dashboard → Plugins & MCPs → Team Marketplaces → Add Marketplace → Import from Repo and paste the private repository URL below. The importing account needs repository access. Cursor reads the manifest and makes plugins available to your team.",
        code: `{{GRAM_REPO_URL}}`,
        language: "text",
      },
      {
        title: "Mark the observability plugin as required",
        description:
          "Set the observability plugin (slug below) to Required for the intended audience: it stays installed and cannot be uninstalled. Default On installs by default but allows opt-out; Default Off requires users to install it. Required does not guarantee working hooks or telemetry: run a tool and verify real events in Speakeasy.",
        code: `{{GRAM_CURSOR_PLUGIN_NAME}}`,
        language: "text",
        helpLink: {
          url: "https://cursor.com/docs/plugins#plugin-installation-modes",
          linkLabel: "Cursor installation modes",
          sentence: "Review {LINK} for rollout controls",
        },
      },
      {
        title: "Keep the marketplace up to date",
        description:
          "For GitHub imports, Enable Auto Refresh requires the Cursor GitHub App installed on the repository. Otherwise, click Refresh manually after publishing. Verify the refreshed plugin and hook events rather than assuming publication immediately updates every session.",
        helpLink: {
          url: "https://cursor.com/docs/plugins#keep-plugins-up-to-date",
          linkLabel: "Cursor marketplace updates",
          sentence: "See {LINK} for refresh requirements",
        },
      },
    ],
  },
  {
    id: "pi",
    setupSteps: [
      {
        title: "Install the speakeasy-hooks binary",
        description:
          "Pi has no plugin marketplace and no hook configuration — observability is a Pi extension — so the speakeasy-hooks CLI renders it straight into your repo. Install the binary first.",
        code: `curl -fsSL https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.sh | sh`,
        language: "bash",
      },
      {
        title: "Render the extension into your repo",
        description:
          "Run this from the repo you use Pi in. It writes .pi/extensions/speakeasy-observability/index.ts and speakeasy.json, which map Pi's lifecycle events to Speakeasy's dashboard. Pi loads project-local extensions only after you trust the project, so answer its trust prompt on first start.",
        code: `GRAM_HOOKS_ORG_KEY="{{GRAM_API_KEY}}" \\
speakeasy-hooks install --provider=pi --dir=. --project={{GRAM_PROJECT_SLUG}}`,
        language: "bash",
        requiresApiKey: true,
      },
    ],
  },
  {
    id: "opencode",
    setupSteps: [
      {
        title: "Install the speakeasy-hooks binary",
        description:
          "opencode has no plugin marketplace, so the observability plugin is rendered straight into your repo by the speakeasy-hooks CLI. Install the binary first.",
        code: `curl -fsSL https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.sh | sh`,
        language: "bash",
      },
      {
        title: "Render the plugin into your repo",
        description:
          "Run this from the repo you use opencode in. It writes .opencode/plugin/agenthooks.ts and speakeasy.json, which map opencode's events to Speakeasy's dashboard.",
        code: `GRAM_HOOKS_ORG_KEY="{{GRAM_API_KEY}}" \\
speakeasy-hooks install --provider=opencode --dir=. --project={{GRAM_PROJECT_SLUG}}`,
        language: "bash",
        requiresApiKey: true,
      },
    ],
  },
];

function toAgentPlatform(
  id: AgentProviderId,
  setupSteps: AgentPlatform["setupSteps"],
  available = true,
): AgentPlatform {
  const provider = AGENT_PROVIDERS[id];
  return {
    id,
    name: provider.name,
    description: provider.description,
    icon: provider.iconSource,
    connected: false,
    available,
    setupSteps,
  };
}

/** The setup platforms, with copy-paste values for the given server's host. */
export function getAgentPlatforms(
  serverURL: string = getServerURL(),
): AgentPlatform[] {
  const server = new URL(serverURL, window.location.origin);
  const platforms = setupAgentPlatforms(
    server.origin,
    agentEgressHosts(server.href),
  );
  return [
    ...ACTIVE_AGENT_PROVIDER_IDS.setup.map((id) =>
      toAgentPlatform(
        id,
        platforms.find((platform) => platform.id === id)!.setupSteps,
      ),
    ),
    ...COMING_SOON_AGENT_PROVIDER_IDS.map((id) =>
      toAgentPlatform(id, [] as AgentPlatform["setupSteps"], false),
    ),
  ];
}

/**
 * The steps a platform shows given the answer to its plan question: the org
 * rollout until answered "no", then the per-user path for personal plans.
 */
export function platformSteps(
  platform: AgentPlatform,
  eligible: boolean | null | undefined,
): AgentPlatform["setupSteps"] {
  const [gate] = platform.setupSteps;
  if (eligible === false && gate?.eligibility) {
    return [gate, ...gate.eligibility.personalSteps];
  }
  return platform.setupSteps;
}
