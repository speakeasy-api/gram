import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { InstallSteps, type InstallStep } from "@/components/install-steps";
import { Text } from "@/components/ui/Text";
import { useFetcher } from "@/contexts/Fetcher";
import type { ClientFamily } from "@gram/client/models/components/recordinstallintentrequestbody.js";
import {
  invalidateAllPlatformMCPPackageStatus,
  usePlatformMCPPackageStatus,
} from "@gram/client/react-query/platformMCPPackageStatus.js";
import { useRepairPlatformMCPPackageMutation } from "@gram/client/react-query/repairPlatformMCPPackage.js";
import { useQueryClient } from "@tanstack/react-query";
import { Download, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { downloadResponse } from "../plugins/downloadPluginPackage";

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
    description: "Install from the marketplace or a Codex ZIP",
  },
  {
    id: "cursor",
    label: "Cursor",
    description: "Import the marketplace or use a Cursor ZIP",
  },
  {
    id: "opencode",
    label: "OpenCode",
    description: "Extract the OpenCode package into your config directory",
  },
];

export type PlatformMCPInstallMethod = "marketplace" | "download" | "manual";
type PackagePlatform = "claude" | "cursor" | "codex" | "opencode";

function packagePlatform(client: ClientFamily): PackagePlatform {
  if (client === "claude_code" || client === "claude_cowork") return "claude";
  return client;
}

function packageFilename(client: ClientFamily): string {
  return `speakeasy-aicp-platform-mcp-${packagePlatform(client)}.zip`;
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
    return `[mcp_servers.speakeasy-aicp-platform-mcp]\nurl = "${mcpUrl}"`;
  }
  if (client === "opencode") {
    return JSON.stringify(
      {
        mcp: {
          "speakeasy-aicp-platform-mcp": {
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
        "speakeasy-aicp-platform-mcp": {
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

function marketplaceSteps(
  client: ClientFamily,
  marketplaceName: string,
  marketplaceUrl: string,
  repoUrl: string,
): InstallStep[] {
  if (client === "claude_cowork") {
    return [
      {
        title: "Open Claude Cowork plugin settings",
        description:
          "Sign in to Claude.ai with your organization-admin account and open Organization Settings → Plugins. These controls are needed to add a private GitHub plugin source.",
      },
      {
        title: "Sync your organization marketplace from GitHub",
        description:
          "Choose Add plugin → Sync from GitHub. Authorize the Claude GitHub App for the canonical marketplace repository, then select this exact repository.",
        code: repoUrl,
        language: "text",
      },
      {
        title: "Make the plugin available to your account",
        description:
          "Find Speakeasy AICP Platform MCP and choose an availability policy that lets your admin account install it. Do not mark it Required unless your organization has separately decided to deploy it more broadly.",
        code: "speakeasy-aicp-platform-mcp",
        language: "text",
      },
      {
        title: "Install it for yourself and complete OAuth",
        description:
          "Install Speakeasy AICP Platform MCP for your own Claude account, open a new Cowork session, and complete AI Control Plane browser authorization when you first use the MCP.",
      },
    ];
  }

  if (client === "cursor") {
    return [
      {
        title: "Import the organization marketplace into Cursor",
        description:
          "Open the Cursor dashboard for the team you administer, go to Settings → Plugins → Import, and paste the canonical marketplace repository URL.",
        code: repoUrl,
        language: "text",
      },
      {
        title: "Install Platform MCP for your account",
        description:
          "Find Speakeasy AICP Platform MCP in the imported marketplace and install it for your Cursor account. Do not require it for the whole team unless your organization has made that separate rollout decision.",
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
        title: "Add the organization marketplace to Codex",
        description:
          "Run this in the Codex CLI profile where you want Platform MCP. This package contains no Observability hooks or hook approvals.",
        code: `codex plugin marketplace add ${marketplaceUrl}`,
      },
      {
        title: "Install the Platform MCP package",
        description:
          "Open /plugins, find platform-mcp-codex in the organization marketplace, and install it for your account. ChatGPT Codex and the Codex CLI must each be verified separately; the Codex IDE extension remains a manual recovery path until certified.",
        code: "codex /plugins",
      },
      {
        title: "Restart Codex and complete OAuth",
        description:
          "Start a new Codex session, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
      },
    ];
  }

  return [
    {
      title: "Add the marketplace to your Claude Code install",
      description:
        "Run this command in the Claude Code environment where you want to use Platform MCP. It registers the organization marketplace for your local Claude Code profile.",
      code: `/plugin marketplace add ${marketplaceUrl}`,
    },
    {
      title: "Install Platform MCP for your profile",
      description:
        "Install only the Platform MCP package into your Claude Code profile. This does not install it for other organization members.",
      code: `/plugin install speakeasy-aicp-platform-mcp@${marketplaceName}`,
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
  const queryClient = useQueryClient();
  const { fetch: authFetch } = useFetcher();
  const [client, setClient] = useState<ClientFamily>(initialClient);
  const [method, setMethod] = useState<PlatformMCPInstallMethod>(
    initialMethod ?? "marketplace",
  );
  const [isDownloading, setIsDownloading] = useState(false);
  const explicitMethodRef = useRef(false);
  const status = usePlatformMCPPackageStatus(undefined, undefined, {
    refetchInterval: 5_000,
  });
  const repair = useRepairPlatformMCPPackageMutation({
    onSuccess: async () => {
      await invalidateAllPlatformMCPPackageStatus(queryClient);
      toast.success(
        "Platform MCP package is ready in the organization marketplace",
      );
    },
    onError: () => {
      toast.error("Could not publish the Platform MCP package");
    },
  });

  useEffect(() => {
    setClient(initialClient);
    explicitMethodRef.current = false;
  }, [initialClient]);

  useEffect(() => {
    explicitMethodRef.current = false;
  }, [initialMethod]);

  const packageStatus = status.data;
  const supportsMarketplace = client !== "opencode";
  const marketplaceReady =
    supportsMarketplace &&
    packageStatus?.freshness === "current" &&
    !!packageStatus.marketplaceName &&
    !!packageStatus.marketplaceUrl &&
    !!packageStatus.repoUrl;

  useEffect(() => {
    if (status.isLoading || explicitMethodRef.current) return;

    if (initialMethod === "manual") {
      setMethod("manual");
    } else if (
      initialMethod === "download" &&
      packageStatus?.directDownloadAvailable
    ) {
      setMethod("download");
    } else if (
      supportsMarketplace &&
      initialMethod === "marketplace" &&
      (marketplaceReady || packageStatus?.repairAllowed)
    ) {
      setMethod("marketplace");
    } else if (marketplaceReady) {
      setMethod("marketplace");
    } else if (packageStatus?.directDownloadAvailable) {
      setMethod("download");
    } else {
      setMethod("manual");
    }
  }, [
    initialMethod,
    marketplaceReady,
    packageStatus?.directDownloadAvailable,
    packageStatus?.repairAllowed,
    status.isLoading,
    supportsMarketplace,
  ]);

  const steps = useMemo(() => {
    if (method === "marketplace") {
      if (
        packageStatus?.marketplaceName &&
        packageStatus.marketplaceUrl &&
        packageStatus.repoUrl
      ) {
        return marketplaceSteps(
          client,
          packageStatus.marketplaceName,
          packageStatus.marketplaceUrl,
          packageStatus.repoUrl,
        );
      }
      return [];
    }
    if (method === "download") {
      const filename = packageFilename(client);
      const extractStep: InstallStep = (() => {
        switch (client) {
          case "claude_cowork":
            return {
              title: "Upload the package to Claude Cowork",
              description:
                "In Organization Settings → Plugins, choose Add plugin → Upload a file. Make the uploaded plugin available to your admin account, then install it for yourself.",
            };
          case "claude_code":
            return {
              title: "Extract and load the Claude plugin",
              description:
                "Extract the ZIP to a stable directory you control and launch Claude Code with that directory as a local plugin.",
              code: `claude --plugin-dir /path/to/${filename.replace(/\.zip$/, "")}`,
            };
          case "cursor":
            return {
              title: "Import the extracted Cursor plugin",
              description:
                "Extract the ZIP, then import its directory from Cursor Settings → Plugins. This is a native Cursor plugin package, not an MCP-only deeplink.",
            };
          case "codex":
            return {
              title: "Install the extracted Codex plugin",
              description:
                "Extract the ZIP into the Codex plugin location used by your CLI profile, then install or enable platform-mcp-codex from /plugins. This package contains no Observability hooks or approvals.",
            };
          case "opencode":
            return {
              title: "Extract into your OpenCode config directory",
              description:
                "Extract the ZIP contents directly into ~/.config/opencode for your account, or into .opencode for one repository. The package installs both the loader and reviewed skill path.",
            };
        }
      })();
      return [
        {
          title: `Download ${filename}`,
          description: `Download the credential-free ${PLATFORM_MCP_INSTALL_CLIENTS.find((item) => item.id === client)?.label ?? "agent"} Platform MCP package from the Speakeasy AI Control Plane.`,
        },
        extractStep,
        {
          title: "Update by replacing the extracted package",
          description:
            "Direct downloads do not auto-update. Download the new ZIP, replace the previous package, and avoid keeping duplicate marketplace and ZIP installations enabled.",
        },
        {
          title: "Restart your client and complete OAuth",
          description:
            "Open a new session under your account, use Platform MCP, and complete AI Control Plane browser authorization when prompted.",
        },
      ];
    }
    return manualSteps(client, mcpUrl);
  }, [client, mcpUrl, method, packageStatus]);

  const download = async () => {
    setIsDownloading(true);
    onInstructionIntent?.();
    try {
      const platform = packagePlatform(client);
      const response = await authFetch(
        `/rpc/plugins.downloadPlatformMCPPlugin?platform=${platform}`,
        {},
      );
      if (!response.ok) throw new Error("download failed");
      await downloadResponse(response, packageFilename(client));
    } catch {
      toast.error("Could not download the Platform MCP package");
    } finally {
      setIsDownloading(false);
    }
  };

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

      {packageStatus?.admission === "indeterminate" && (
        <Alert variant="warning">
          <div>
            <AlertTitle>Package availability is temporarily unknown</AlertTitle>
            <AlertDescription>
              Marketplace repair and direct download are disabled until the AI
              Control Plane can confirm organization admission. Existing runtime
              authorization is unaffected.
            </AlertDescription>
          </div>
        </Alert>
      )}

      {supportsMarketplace &&
        packageStatus?.repairAllowed &&
        !marketplaceReady && (
          <Alert>
            <div>
              <AlertTitle>Prepare the canonical marketplace</AlertTitle>
              <AlertDescription>
                Publish or repair the credential-free Platform Plugin in the
                organization&apos;s default-project marketplace before
                installing it.
              </AlertDescription>
            </div>
            <Button
              size="sm"
              disabled={repair.isPending}
              onClick={() =>
                repair.mutate({
                  security: { sessionHeaderGramSession: "" },
                })
              }
            >
              <Button.LeftIcon>
                <RefreshCw className="size-3" />
              </Button.LeftIcon>
              <Button.Text>
                {repair.isPending ? "Publishing…" : "Publish package"}
              </Button.Text>
            </Button>
          </Alert>
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
              disabled={!marketplaceReady}
              onClick={() => {
                explicitMethodRef.current = true;
                setMethod("marketplace");
              }}
            >
              GitHub installation (preferred)
            </Button>
            <Button
              size="sm"
              variant={method === "download" ? "primary" : "secondary"}
              disabled={!packageStatus?.directDownloadAvailable}
              onClick={() => {
                explicitMethodRef.current = true;
                setMethod("download");
              }}
            >
              Direct{" "}
              {
                PLATFORM_MCP_INSTALL_CLIENTS.find((item) => item.id === client)
                  ?.label
              }{" "}
              ZIP
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
              This recovery route connects only the remote MCP. Use a certified
              plugin package to install the reviewed catalog workflow skill too.
            </AlertDescription>
          </div>
        </Alert>
      )}

      {steps.length > 0 && (
        <InstallSteps steps={steps} onCopy={onInstructionIntent} />
      )}

      {method === "download" && (
        <Button disabled={isDownloading} onClick={() => void download()}>
          <Button.LeftIcon>
            <Download className="size-3" />
          </Button.LeftIcon>
          <Button.Text>
            {isDownloading
              ? "Downloading…"
              : `Download ${PLATFORM_MCP_INSTALL_CLIENTS.find((item) => item.id === client)?.label ?? "agent"} ZIP`}
          </Button.Text>
        </Button>
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
