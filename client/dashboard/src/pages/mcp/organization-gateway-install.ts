import {
  ACTIVE_AGENT_PROVIDER_IDS,
  type AgentProviderId,
} from "@/components/agent-providers/agent-providers";
import {
  claudeCodeAddCommand,
  codexAddCommand,
  codexLoginCommand,
  cursorInstallLink,
  openCodeConfig,
} from "@/lib/mcp-client-install";
import { toAgentPlatform } from "@/pages/setup/setup-data";
import type { AgentPlatform, PlatformSetupStep } from "@/pages/setup/types";

// Per-agent install steps for the Organization Gateway, in the setup flow's
// shape (pages/setup/setup-data.ts) so both pages read and render alike: the
// setup flow's agents in its order, a plan check where the agent has an org
// path, and steps rendered by PlatformSetupStepBody.

function endpointField(url: string): PlatformSetupStep["fields"] {
  return [{ label: "Endpoint", value: url }];
}

export function organizationGatewayInstallPlatforms(
  name: string,
  url: string,
): AgentPlatform[] {
  const steps: Partial<Record<AgentProviderId, PlatformSetupStep[]>> = {
    claude: [
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
    ],
    "claude-cowork": [
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
    codex: [
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
    ],
    cursor: [
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
    ],
    opencode: [
      {
        title: "Add the gateway",
        description:
          "Merge this into ~/.config/opencode/opencode.json for every project, or .opencode/opencode.json for one. opencode signs you in the first time it connects.",
        code: openCodeConfig(name, url),
        language: "json",
      },
    ],
  };

  // The setup flow's agents, in its order, for those the gateway has steps
  // for; then the agents setup does not cover.
  const setupAgents = ACTIVE_AGENT_PROVIDER_IDS.setup.flatMap((id) => {
    const agentSteps = steps[id];
    if (!agentSteps) return [];
    const agent = toAgentPlatform(id, agentSteps);
    // Connectors reach Claude on the web and desktop, not only Cowork.
    return id === "claude-cowork"
      ? [
          {
            ...agent,
            description: "Claude on the web and desktop, including Cowork",
          },
        ]
      : [agent];
  });

  return [
    ...setupAgents,
    {
      id: "chatgpt",
      name: "ChatGPT",
      description: "ChatGPT on the web",
      icon: "chatgpt",
      connected: false,
      setupSteps: [
        {
          title: "Create an app",
          description:
            "On the web, turn on developer mode under Settings › Apps › Advanced settings, then create an app with the endpoint as its URL and OAuth as its authentication.",
          fields: endpointField(url),
        },
      ],
    },
    toAgentPlatform("other", [
      {
        title: "Add the endpoint",
        description:
          "Any client that supports remote MCP servers: add the endpoint as a Streamable HTTP server and sign in when asked.",
        fields: endpointField(url),
      },
    ]),
  ];
}
