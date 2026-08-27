import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { InstallSteps, type InstallStep } from "@/components/install-steps";
import { Text } from "@/components/ui/Text";
import type { ClientFamily } from "@gram/client/models/components/recordinstallintentrequestbody.js";
import { useEffect, useMemo, useRef, useState } from "react";

type PlatformMCPInstallClient = {
  id: ClientFamily;
  label: string;
  description: string;
};

const PLATFORM_MCP_INSTALL_CLIENTS: PlatformMCPInstallClient[] = [
  {
    id: "claude_code",
    label: "Claude Code",
    description: "Install in your Claude Code environment",
  },
  {
    id: "claude_cowork",
    label: "Claude Cowork",
    description: "Make the plugin available, then install it for your account",
  },
  {
    id: "codex",
    label: "OpenAI Codex",
    description: "Add the marketplace, then install for your account",
  },
  {
    id: "cursor",
    label: "Cursor",
    description: "Import the marketplace into your Cursor team",
  },
  {
    id: "opencode",
    label: "OpenCode",
    description: "Copy the OpenCode package out of the public repository",
  },
  {
    id: "other",
    label: "Other agent",
    description: "Point any MCP-capable agent at the remote endpoint",
  },
];

export type PlatformMCPInstallMethod = "marketplace" | "manual";

const PLATFORM_MCP_PLUGIN_NAME = "platform-mcp";

// Platform MCP ships from one public repository for every organization. It
// carries no credentials and no organization identity — access is decided at
// OAuth time — so the marketplace coordinates are fixed rather than resolved
// per organization.
const PUBLIC_MARKETPLACE_REPO_URL =
  "https://github.com/speakeasy-api/platform-mcp";
const PUBLIC_MARKETPLACE_NAME = "speakeasy";

// Whether a reviewed plugin package exists for this agent at all. Both packaged
// install routes are closed without one, leaving the remote MCP configuration.
function supportsPackages(client: ClientFamily): boolean {
  return client !== "other";
}

type PlatformMCPInstallWalkthroughProps = {
  initialClient: ClientFamily;
  initialMethod?: PlatformMCPInstallMethod;
  mcpUrl: string;
  allowClientSelection?: boolean;
  allowMethodSelection?: boolean;
  onInstructionIntent?: () => void;
};

function manualConfig(client: ClientFamily, mcpUrl: string): string {
  if (client === "codex") {
    return `[mcp_servers.${PLATFORM_MCP_PLUGIN_NAME}]\nurl = "${mcpUrl}"`;
  }
  if (client === "opencode") {
    return JSON.stringify(
      {
        mcp: {
          [PLATFORM_MCP_PLUGIN_NAME]: {
            type: "remote",
            url: mcpUrl,
            enabled: true,
          },
        },
      },
      null,
      2,
    );
  }
  return JSON.stringify(
    {
      mcpServers: {
        [PLATFORM_MCP_PLUGIN_NAME]: {
          type: "http",
          url: mcpUrl,
        },
      },
    },
    null,
    2,
  );
}

function manualSteps(client: ClientFamily, mcpUrl: string): InstallStep[] {
  switch (client) {
    case "claude_code":
      return [
        {
          title: "Open your Claude Code MCP configuration",
          description:
            "Use this recovery configuration in the Claude Code profile where you want to use Platform MCP.",
          code: manualConfig(client, mcpUrl),
          language: "json",
        },
        {
          title: "Restart your Claude Code session",
          description:
            "Open a new Claude Code session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    case "claude_cowork":
      return [
        {
          title: "Create a custom connector for your Claude account",
          description:
            "In Claude Cowork, add a custom remote MCP connector using this endpoint configuration. This recovery route does not install the reviewed skill.",
          code: manualConfig(client, mcpUrl),
          language: "json",
        },
        {
          title: "Open a new Cowork session",
          description:
            "Use the connector from your account and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    case "codex":
      return [
        {
          title: "Add Platform MCP to your Codex config",
          description:
            "Merge this block into ~/.codex/config.toml for the Codex CLI profile you use. This is an MCP-only recovery route.",
          code: manualConfig(client, mcpUrl),
          language: "toml",
        },
        {
          title: "Restart Codex and complete OAuth",
          description:
            "Start a new Codex CLI session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    case "cursor":
      return [
        {
          title: "Add Platform MCP to your Cursor profile",
          description:
            "Open Cursor Settings → Tools & MCP and add the remote server using this configuration. This MCP-only route does not install the reviewed skill.",
          code: manualConfig(client, mcpUrl),
          language: "json",
        },
        {
          title: "Restart Cursor and complete OAuth",
          description:
            "Open a new Cursor agent session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    case "other":
      return [
        {
          title: "Add Platform MCP as a remote MCP server",
          description:
            "Add this remote server to your agent's MCP configuration. Most agents use this shape; if yours expects a different one, keep the URL and match its own format. Platform MCP speaks streamable HTTP with OAuth.",
          code: manualConfig(client, mcpUrl),
          language: "json",
        },
        {
          title: "Restart your agent and complete OAuth",
          description:
            "Start a new session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    case "opencode":
      return [
        {
          title: "Add Platform MCP to your OpenCode config",
          description:
            "Merge this remote MCP entry into ~/.config/opencode/opencode.json for your account, or .opencode/opencode.json for one repository. This does not install the reviewed skill.",
          code: manualConfig(client, mcpUrl),
          language: "json",
        },
        {
          title: "Restart OpenCode and complete OAuth",
          description:
            "Start a new OpenCode session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
  }
}

function marketplaceSteps(client: ClientFamily): InstallStep[] {
  if (client === "claude_cowork") {
    return [
      {
        title: "Open Claude Cowork plugin settings",
        description:
          "Sign in to Claude.ai with your organization-admin account and open Organization Settings → Plugins.",
      },
      {
        title: "Sync the Speakeasy marketplace from GitHub",
        description:
          "Choose Add plugin → Sync from GitHub and select this repository. It is public, so no GitHub App authorization is required.",
        code: PUBLIC_MARKETPLACE_REPO_URL,
        language: "text",
      },
      {
        title: "Make the plugin available to your account",
        description:
          "Find Platform MCP and choose an availability policy that lets your admin account install it. Do not mark it Required unless your organization has separately decided to deploy it more broadly.",
        code: PLATFORM_MCP_PLUGIN_NAME,
        language: "text",
      },
      {
        title: "Install it for yourself and complete OAuth",
        description:
          "Install Platform MCP for your own Claude account, open a new Cowork session, and complete AI Control Plane browser authorization when you first use the MCP.",
      },
    ];
  }

  if (client === "cursor") {
    return [
      {
        title: "Import the Speakeasy marketplace into Cursor",
        description:
          "Open the Cursor dashboard for the team you administer, go to Settings → Plugins → Import, and paste this repository URL.",
        code: PUBLIC_MARKETPLACE_REPO_URL,
        language: "text",
      },
      {
        title: "Install Platform MCP for your account",
        description:
          "Find Platform MCP in the imported marketplace and install it for your Cursor account. Do not require it for the whole team unless your organization has made that separate rollout decision.",
        code: "platform-mcp-cursor",
        language: "text",
      },
      {
        title: "Restart Cursor and complete OAuth",
        description:
          "Open a new Cursor agent session, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
      },
    ];
  }

  if (client === "codex") {
    return [
      {
        title: "Add the Speakeasy marketplace to Codex",
        description:
          "Run this in the Codex CLI profile where you want Platform MCP. This package contains no Observability hooks or hook approvals.",
        code: `codex plugin marketplace add ${PUBLIC_MARKETPLACE_REPO_URL}`,
      },
      {
        title: "Install the Platform MCP package",
        description:
          "Open /plugins, find platform-mcp-codex in the Speakeasy marketplace, and install it for your account. ChatGPT Codex and the Codex CLI must each be verified separately; the Codex IDE extension remains a manual recovery path until certified.",
        code: "codex /plugins",
      },
      {
        title: "Restart Codex and complete OAuth",
        description:
          "Start a new Codex session, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
      },
    ];
  }

  if (client === "opencode") {
    return [
      {
        title: "Clone the Speakeasy marketplace repository",
        description:
          "OpenCode has no marketplace importer, so copy the package out of the public repository instead.",
        code: `git clone ${PUBLIC_MARKETPLACE_REPO_URL}`,
      },
      {
        title: "Copy the OpenCode package into your config directory",
        description:
          "Copy opencode-plugins/platform-mcp into ~/.config/opencode for your account, or into .opencode for one repository. This installs both the loader and the reviewed skill path.",
        code: `cp -R platform-mcp/opencode-plugins/${PLATFORM_MCP_PLUGIN_NAME}/. ~/.config/opencode/`,
      },
      {
        title: "Restart OpenCode and complete OAuth",
        description:
          "Start a new OpenCode session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
      },
    ];
  }

  return [
    {
      title: "Add the marketplace to your Claude Code install",
      description:
        "Run this command in the Claude Code environment where you want to use Platform MCP. It registers the Speakeasy marketplace for your local Claude Code profile.",
      code: `/plugin marketplace add ${PUBLIC_MARKETPLACE_REPO_URL}`,
    },
    {
      title: "Install Platform MCP for your profile",
      description:
        "Install only the Platform MCP package into your Claude Code profile. This does not install it for other organization members.",
      code: `/plugin install ${PLATFORM_MCP_PLUGIN_NAME}@${PUBLIC_MARKETPLACE_NAME}`,
    },
    {
      title: "Restart your Claude Code session and complete OAuth",
      description:
        "Open a new Claude Code session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
    },
  ];
}

export function PlatformMCPInstallWalkthrough({
  initialClient,
  initialMethod,
  mcpUrl,
  allowClientSelection = false,
  allowMethodSelection = true,
  onInstructionIntent,
}: PlatformMCPInstallWalkthroughProps): JSX.Element {
  const [client, setClient] = useState<ClientFamily>(initialClient);
  const [selectedMethod, setMethod] = useState<PlatformMCPInstallMethod>(
    initialMethod ?? "marketplace",
  );
  const explicitMethodRef = useRef(false);

  useEffect(() => {
    setClient(initialClient);
    explicitMethodRef.current = false;
  }, [initialClient]);

  useEffect(() => {
    explicitMethodRef.current = false;
  }, [initialMethod]);

  // An agent with no reviewed package has exactly one route, so the marketplace
  // method stays unreachable for it however the state got there: on an explicit
  // initialMethod, or in the render before the effect below settles. Deriving
  // the method closes both at once instead of guarding each place one is read.
  const method = supportsPackages(client) ? selectedMethod : "manual";

  // The public marketplace is always installable, so the preferred route no
  // longer depends on any per-organization package state.
  useEffect(() => {
    if (explicitMethodRef.current) return;
    setMethod(initialMethod === "manual" ? "manual" : "marketplace");
  }, [initialMethod]);

  const steps = useMemo(() => {
    if (method === "marketplace") {
      return marketplaceSteps(client);
    }
    return manualSteps(client, mcpUrl);
  }, [client, mcpUrl, method]);

  return (
    <div className="space-y-5">
      {allowClientSelection && (
        <div>
          <Text small className="mb-2 font-medium">
            Agent
          </Text>
          <div className="grid gap-2 sm:grid-cols-2">
            {PLATFORM_MCP_INSTALL_CLIENTS.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => setClient(item.id)}
                className={`border p-3 text-left ${client === item.id ? "border-foreground bg-muted/40" : "border-border"}`}
              >
                <div className="text-sm font-medium">{item.label}</div>
                <div className="text-muted-foreground mt-1 text-xs">
                  {item.description}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      {allowMethodSelection && (
        <div>
          <Text small className="mb-2 font-medium">
            Install method
          </Text>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant={method === "marketplace" ? "primary" : "secondary"}
              disabled={!supportsPackages(client)}
              onClick={() => {
                explicitMethodRef.current = true;
                setMethod("marketplace");
              }}
            >
              Marketplace install (preferred)
            </Button>
            <Button
              size="sm"
              variant={method === "manual" ? "primary" : "secondary"}
              onClick={() => {
                explicitMethodRef.current = true;
                setMethod("manual");
              }}
            >
              Manual recovery
            </Button>
          </div>
        </div>
      )}

      {method === "manual" && (
        <Alert variant="warning">
          <div>
            <AlertTitle>MCP only—reviewed skills are not installed</AlertTitle>
            <AlertDescription>
              {supportsPackages(client)
                ? "This recovery route connects only the remote MCP. Use a certified plugin package to install the reviewed catalog workflow skill too."
                : "This agent has no certified plugin package, so only the remote MCP is connected. Pick a certified agent if you also want the reviewed catalog workflow skill."}
            </AlertDescription>
          </div>
        </Alert>
      )}

      {steps.length > 0 && (
        <InstallSteps steps={steps} onCopy={onInstructionIntent} />
      )}

      <Alert>
        <div>
          <AlertTitle>Waiting for live authorization</AlertTitle>
          <AlertDescription>
            Installing, downloading, or copying instructions records intent
            only. Setup advances after the AI Control Plane observes a current
            authorized Platform MCP connection.
          </AlertDescription>
        </div>
      </Alert>
    </div>
  );
}
