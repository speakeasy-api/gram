import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import {
  claudeCodeAddCommand,
  codexAddCommand,
  codexLoginCommand,
  cursorInstallLink,
  openCodeConfig,
} from "@/lib/mcp-client-install";
import type { AgentPlatform, PlatformSetupStep } from "@/pages/setup/types";

// Per-agent install steps for the Team Gateway, in the setup flow's shape
// (pages/setup/setup-data.ts) so both pages read and render alike: the same
// agent names, icons and order, a plan check where the agent has an org path,
// and steps rendered by PlatformSetupStepBody.

function platform(
  id: string,
  provider: { name: string; description: string; iconSource: string },
  setupSteps: PlatformSetupStep[],
): AgentPlatform {
  return {
    id,
    name: provider.name,
    description: provider.description,
    icon: provider.iconSource,
    connected: false,
    setupSteps,
  };
}

function endpointField(url: string): PlatformSetupStep["fields"] {
  return [{ label: "Endpoint", value: url }];
}

export function teamGatewayInstallPlatforms(
  name: string,
  url: string,
): AgentPlatform[] {
  return [
    platform("claude", AGENT_PROVIDERS.claude, [
      {
        title: "Add the gateway",
        description: "Run this in your terminal.",
        code: claudeCodeAddCommand(name, url, { multiline: true }),
        language: "bash",
      },
      {
        title: "Sign in",
        description:
          "Open /mcp in Claude Code, select the gateway and choose Authenticate. Restarting alone may not open the sign-in page.",
      },
    ]),
    platform(
      "claude-cowork",
      {
        ...AGENT_PROVIDERS["claude-cowork"],
        description: "Claude on the web and desktop, including Cowork",
      },
      [
        {
          title: "Plan check",
          description:
            "Organization connectors require Team or Enterprise and an Owner or Primary Owner. On Pro or Max, each person adds the connector themselves.",
          helpLink: {
            url: "https://claude.ai/admin-settings/billing",
            linkLabel: "Claude.ai",
            sentence: "Visit {LINK} to check your plan",
          },
          eligibility: {
            question: "Do you have a Team or Enterprise Claude plan?",
            personalSteps: [
              {
                title: "Add a custom connector",
                description:
                  "In Claude on the web or desktop, open Settings › Connectors, choose Add custom connector and paste the endpoint. Then choose Connect to sign in.",
                fields: endpointField(url),
                helpLink: {
                  url: "https://claude.ai/settings/connectors",
                  linkLabel: "Claude connector settings",
                  sentence: "Open {LINK}",
                },
              },
            ],
          },
        },
        {
          title: "Add the connector for your organization",
          description:
            "An Owner opens Organization settings › Connectors, chooses Add › Custom › Web and pastes the endpoint.",
          fields: endpointField(url),
        },
        {
          title: "Each person connects",
          description:
            "Everyone opens Settings › Connectors, finds the gateway and chooses Connect to sign in with their Speakeasy account.",
        },
      ],
    ),
    platform("codex", AGENT_PROVIDERS.codex, [
      {
        title: "Add the gateway",
        description: "Run this in your terminal.",
        code: codexAddCommand(name, url, { multiline: true }),
        language: "bash",
      },
      {
        title: "Sign in",
        description: "Codex does not sign in when you add a server. Run this.",
        code: codexLoginCommand(name),
        language: "bash",
      },
    ]),
    platform("cursor", AGENT_PROVIDERS.cursor, [
      {
        title: "Add the gateway",
        description:
          "Opens Cursor and adds the gateway to your MCP settings in one click.",
        helpLink: {
          url: cursorInstallLink(name, url),
          linkLabel: "Add to Cursor",
          sentence: "{LINK}",
        },
      },
      {
        title: "Sign in",
        description:
          "In Cursor Settings › Tools & MCP, sign in to the gateway when Cursor asks.",
      },
    ]),
    platform("opencode", AGENT_PROVIDERS.opencode, [
      {
        title: "Add the gateway",
        description:
          "Merge this into ~/.config/opencode/opencode.json for every project, or .opencode/opencode.json for one. opencode signs you in the first time it connects.",
        code: openCodeConfig(name, url),
        language: "json",
      },
    ]),
    platform(
      "chatgpt",
      {
        name: "ChatGPT",
        description: "ChatGPT on the web",
        iconSource: "chatgpt",
      },
      [
        {
          title: "Create an app",
          description:
            "On the web, turn on developer mode under Settings › Apps › Advanced settings, then create an app with the endpoint as its URL and OAuth as its authentication.",
          fields: endpointField(url),
        },
      ],
    ),
    platform("other", AGENT_PROVIDERS.other, [
      {
        title: "Add the endpoint",
        description:
          "Any client that supports remote MCP servers: add the endpoint as a Streamable HTTP server and sign in when asked.",
        fields: endpointField(url),
      },
    ]),
  ];
}
