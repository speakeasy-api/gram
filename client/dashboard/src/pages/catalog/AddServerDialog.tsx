import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { cn, getServerURL } from "@/lib/utils";
import type { Server } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import type { ExternalMCPRemote } from "@gram/client/models/components";
import { Button, Dialog, Input, Stack } from "@speakeasy-api/moonshine";
import {
  AlertCircle,
  ArrowRight,
  Check,
  Circle,
  ExternalLink,
  Loader2,
  MessageCircle,
  Plug,
  Plus,
  Server as ServerIcon,
  Settings,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import {
  type ConfigurePhase,
  type CompletePhase,
  type DeployingPhase,
  type ErrorPhase,
  type ExternalMcpReleaseWorkflow,
  type SelectRemotesPhase,
  type ServerToolsetStatus,
  useExternalMcpReleaseWorkflow,
} from "./useExternalMcpReleaseWorkflow";

/** Friendly display names and descriptions for known remote endpoints */
const REMOTE_DISPLAY_INFO: Record<
  string,
  { name: string; description: string }
> = {
  // Salesforce Industry Clouds
  "insurance-cloud": {
    name: "Insurance Cloud",
    description: "Policy management, claims processing, and underwriting",
  },
  "health-cloud": {
    name: "Health Cloud",
    description: "Patient care coordination and healthcare management",
  },
  "consumer-goods-cloud": {
    name: "Consumer Goods Cloud",
    description: "Retail execution, trade promotion, and field operations",
  },
  "manufacturing-cloud": {
    name: "Manufacturing Cloud",
    description: "Sales agreements, account forecasting, and production",
  },
  "automotive-cloud": {
    name: "Automotive Cloud",
    description: "Vehicle sales, service, and driver engagement",
  },
  "communications-cloud": {
    name: "Communications Cloud",
    description: "Order management and telecom service configuration",
  },
  "media-cloud": {
    name: "Media Cloud",
    description: "Ad sales, content distribution, and subscriber management",
  },
  "financial-services-cloud": {
    name: "Financial Services Cloud",
    description: "Wealth management, banking, and financial planning",
  },
  "nonprofit-cloud": {
    name: "Nonprofit Cloud",
    description: "Fundraising, grants, and program management",
  },
  "education-cloud": {
    name: "Education Cloud",
    description: "Student lifecycle, admissions, and learning management",
  },
  "public-sector": {
    name: "Public Sector",
    description: "Government services, permits, and case management",
  },
  "energy-utilities-cloud": {
    name: "Energy & Utilities Cloud",
    description: "Meter data, field service, and customer programs",
  },
  "loyalty-management": {
    name: "Loyalty Management",
    description: "Points, rewards, and member engagement programs",
  },
  "pricing-ngp": {
    name: "Industries Pricing",
    description: "Dynamic pricing, quotes, and product configuration",
  },
  "rebate-management": {
    name: "Rebate Management",
    description: "Rebate programs, calculations, and payouts",
  },
  "document-generation": {
    name: "Document Generation",
    description: "Automated document creation and templates",
  },
  omnistudio: {
    name: "OmniStudio",
    description: "Guided flows, data integration, and UI components",
  },
  core: {
    name: "Salesforce Core",
    description: "Standard CRM objects and platform features",
  },
  // Salesforce Platform APIs
  "sobject-all": {
    name: "SObject All",
    description: "Full CRUD access to all Salesforce objects",
  },
  "sobject-reads": {
    name: "SObject Reads",
    description: "Read-only access to Salesforce objects",
  },
  "sobject-mutations": {
    name: "SObject Mutations",
    description: "Create and update Salesforce records",
  },
  "sobject-deletes": {
    name: "SObject Deletes",
    description: "Delete Salesforce records",
  },
  "invocable-actions": {
    name: "Invocable Actions",
    description: "Execute Flows, Apex actions, and quick actions",
  },
  invocable_actions: {
    name: "Invocable Actions",
    description: "Execute Flows, Apex actions, and quick actions",
  },
  "salesforce-api-context": {
    name: "API Context",
    description: "Org info, user details, and API limits",
  },
  "data-cloud-queries": {
    name: "Data Cloud Queries",
    description: "Query unified customer profiles and segments",
  },
  "tableau-next": {
    name: "Tableau Next",
    description: "Analytics, dashboards, and data visualization",
  },
  "revenue-cloud": {
    name: "Revenue Cloud",
    description: "CPQ, billing, and subscription management",
  },
};

/** Get friendly display info for a remote URL */
function getRemoteDisplayInfo(url: string): {
  name: string;
  description: string;
} {
  try {
    const parsedUrl = new URL(url);
    const pathParts = parsedUrl.pathname.split("/").filter(Boolean);
    const endpoint = pathParts[pathParts.length - 1] || "";

    // Check for known endpoints
    const info = REMOTE_DISPLAY_INFO[endpoint.toLowerCase()];
    if (info) return info;

    // Fallback: format the endpoint name nicely
    const formattedName = endpoint
      .split("-")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");

    return {
      name: formattedName || endpoint,
      description: parsedUrl.host,
    };
  } catch {
    return { name: url, description: "" };
  }
}

export interface AddServerDialogProps {
  servers: Server[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onServersAdded?: () => void;
  projectSlug?: string;
}

/**
 * Hook to fetch server details (including remotes) for all servers.
 * This enriches the server objects with remote endpoint data from the registry.
 */
function useEnrichedServers(servers: Server[], open: boolean) {
  const client = useSdkClient();
  const [enrichedServers, setEnrichedServers] = useState<Server[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Track the server specifiers to avoid re-fetching for the same servers
  const fetchedForRef = useRef<string | null>(null);
  const serversKey = servers.map((s) => s.registrySpecifier).join(",");

  // Fetch details when dialog opens
  useEffect(() => {
    if (!open) {
      // Reset when dialog closes
      setEnrichedServers([]);
      setIsLoading(false);
      setError(null);
      fetchedForRef.current = null;
      return;
    }

    if (servers.length === 0) {
      setEnrichedServers([]);
      return;
    }

    // Skip if we've already fetched for these servers
    if (fetchedForRef.current === serversKey) {
      return;
    }

    const fetchServerDetails = async () => {
      setIsLoading(true);
      setError(null);
      fetchedForRef.current = serversKey;

      try {
        const enriched = await Promise.all(
          servers.map(async (server) => {
            // Skip if server already has remotes populated
            if (server.remotes && server.remotes.length > 0) {
              return server;
            }

            try {
              if (!server.registryId) {
                return server;
              }
              const details = await client.mcpRegistries.getServerDetails({
                registryId: server.registryId,
                serverSpecifier: server.registrySpecifier,
              });

              // Merge remotes from details into the server object
              return {
                ...server,
                remotes: details.remotes as ExternalMCPRemote[] | undefined,
              };
            } catch (err) {
              // If we can't fetch details for a specific server, just use original
              console.warn(
                `Failed to fetch details for ${server.registrySpecifier}:`,
                err,
              );
              return server;
            }
          }),
        );

        setEnrichedServers(enriched);
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch details",
        );
        // Fall back to original servers
        setEnrichedServers(servers);
      } finally {
        setIsLoading(false);
      }
    };

    fetchServerDetails();
  }, [open, serversKey, servers, client]);

  return { enrichedServers, isLoading, error };
}

export function AddServerDialog({
  servers,
  open,
  onOpenChange,
  onServersAdded,
  projectSlug,
}: AddServerDialogProps) {
  // Fetch server details (including remotes) when dialog opens
  const {
    enrichedServers,
    isLoading: isLoadingDetails,
    error: detailsError,
  } = useEnrichedServers(servers, open);

  // Use enriched servers (with remotes) for the workflow
  const releaseState = useExternalMcpReleaseWorkflow({
    servers: enrichedServers,
    projectSlug,
  });

  // Reset when dialog closes
  useEffect(() => {
    if (!open) {
      releaseState.reset();
    }
  }, [open]);

  // Clean up Radix body scroll-lock on unmount (e.g. when navigating away mid-dialog)
  useEffect(() => {
    return () => {
      document.body.style.removeProperty("pointer-events");
    };
  }, []);

  // Notify parent when all toolsets are done
  const allToolsetsDone =
    releaseState.phase === "complete" &&
    releaseState.toolsetStatuses.length > 0 &&
    releaseState.toolsetStatuses.every(
      (s) => s.status === "completed" || s.status === "failed",
    );
  const prevAllDoneRef = useRef(false);
  useEffect(() => {
    if (allToolsetsDone && !prevAllDoneRef.current) {
      prevAllDoneRef.current = true;
      onServersAdded?.();
    }
    if (!allToolsetsDone) {
      prevAllDoneRef.current = false;
    }
  }, [allToolsetsDone]);

  if (servers.length === 0) return null;

  // Show loading state while fetching server details
  if (isLoadingDetails) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <Dialog.Content className="gap-2">
          <Dialog.Header>
            <Dialog.Title>Loading...</Dialog.Title>
            <Dialog.Description>Fetching server details...</Dialog.Description>
          </Dialog.Header>
          <div className="flex items-center justify-center gap-2 py-8">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
            <Type muted>Loading server configuration...</Type>
          </div>
        </Dialog.Content>
      </Dialog>
    );
  }

  // Show error state if details fetch failed
  if (detailsError) {
    return (
      <Dialog open={open} onOpenChange={onOpenChange}>
        <Dialog.Content className="gap-2">
          <Dialog.Header>
            <Dialog.Title>Error</Dialog.Title>
            <Dialog.Description>
              Failed to load server details
            </Dialog.Description>
          </Dialog.Header>
          <div className="py-4">
            <div className="border-destructive/30 bg-destructive/5 flex items-start gap-3 rounded-lg border p-3">
              <AlertCircle className="text-destructive mt-0.5 h-5 w-5 shrink-0" />
              <Type small className="text-destructive/80">
                {detailsError}
              </Type>
            </div>
          </div>
          <Dialog.Footer>
            <Button variant="tertiary" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    );
  }

  // Don't unmount while the dialog is open or closing — Radix needs the DOM to animate
  if (enrichedServers.length === 0 && !open) return null;

  const isSingle = enrichedServers.length === 1;
  // Check if we came from multi-remote flow (some servers have selectedRemotes)
  const hasConfiguredMultiRemote =
    releaseState.phase === "configure" &&
    releaseState.serverConfigs.some((c) => c.selectedRemotes);
  // Check if all servers are already added (for title/description)
  // Multi-remote servers with selectedRemotes are always new deployments
  const allAlreadyAdded =
    releaseState.phase === "configure" &&
    releaseState.existingSpecifiers.size > 0 &&
    releaseState.serverConfigs.every(
      (c) =>
        !c.selectedRemotes &&
        releaseState.existingSpecifiers.has(c.server.registrySpecifier),
    );
  const title = (() => {
    if (releaseState.phase === "complete") return "Added to Project";
    if (releaseState.phase === "error") return "Deployment Error";
    if (releaseState.phase === "selectRemotes") {
      const config =
        releaseState.multiRemoteConfigs[releaseState.currentServerIndex];
      return `Configure ${config?.server.title ?? config?.server.registrySpecifier ?? "Server"}`;
    }
    if (allAlreadyAdded) return "Already in Project";
    if (hasConfiguredMultiRemote) return "One more step";
    return isSingle
      ? "Add to Project"
      : `Add ${enrichedServers.length} servers to project`;
  })();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="gap-2">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>
            <PhaseDescription
              phase={releaseState.phase}
              isSingle={isSingle}
              hasConfiguredMultiRemote={hasConfiguredMultiRemote}
              allAlreadyAdded={allAlreadyAdded}
            />
          </Dialog.Description>
        </Dialog.Header>
        <PhaseContent
          releaseState={releaseState}
          isSingle={isSingle}
          onClose={() => onOpenChange(false)}
        />
      </Dialog.Content>
    </Dialog>
  );
}

function PhaseDescription({
  phase,
  isSingle,
  hasConfiguredMultiRemote,
  allAlreadyAdded,
}: {
  phase: ExternalMcpReleaseWorkflow["phase"];
  isSingle: boolean;
  hasConfiguredMultiRemote: boolean;
  allAlreadyAdded: boolean;
}) {
  switch (phase) {
    case "selectRemotes":
      return "This server has multiple endpoints. Select which ones to include.";
    case "configure":
      if (allAlreadyAdded) {
        return "";
      }
      if (hasConfiguredMultiRemote) {
        return "Name the remaining servers before adding to your project.";
      }
      return isSingle
        ? "Add this MCP server to your project."
        : "Configure and add these MCP servers to your project.";
    case "deploying":
      return "Deploying MCP server configuration...";
    case "complete":
      return "";
    case "error":
      return "";
  }
}

function PhaseContent({
  releaseState,
  isSingle,
  onClose,
}: {
  releaseState: ExternalMcpReleaseWorkflow;
  isSingle: boolean;
  onClose: () => void;
}) {
  switch (releaseState.phase) {
    case "selectRemotes":
      return (
        <SelectRemotesPhaseContent
          releaseState={releaseState}
          onClose={onClose}
        />
      );
    case "configure":
      return (
        <ConfigurePhaseContent releaseState={releaseState} onClose={onClose} />
      );
    case "deploying":
      return <DeployingPhaseContent releaseState={releaseState} />;
    case "complete":
      return (
        <CompletePhaseContent
          releaseState={releaseState}
          isSingle={isSingle}
          onClose={onClose}
        />
      );
    case "error":
      return (
        <ErrorPhaseContent releaseState={releaseState} onClose={onClose} />
      );
  }
}

/** Routes scoped to the target project (which may differ from the current project). */
function useTargetRoutes(releaseState: ExternalMcpReleaseWorkflow) {
  return useRoutes(
    releaseState.projectSlug
      ? { projectSlug: releaseState.projectSlug }
      : undefined,
  );
}

// --- Select Remotes Phase ---

function SelectRemotesPhaseContent({
  releaseState,
  onClose,
}: {
  releaseState: SelectRemotesPhase;
  onClose: () => void;
}) {
  const currentConfig =
    releaseState.multiRemoteConfigs[releaseState.currentServerIndex];
  if (!currentConfig) return null;

  const totalServers = releaseState.multiRemoteConfigs.length;
  const currentNumber = releaseState.currentServerIndex + 1;
  const isLast = releaseState.currentServerIndex === totalServers - 1;

  const handleRemoteToggle = (url: string) => {
    const newSelected = new Set(currentConfig.selectedRemoteUrls);
    if (newSelected.has(url)) {
      newSelected.delete(url);
    } else {
      newSelected.add(url);
    }
    releaseState.updateCurrentConfig({ selectedRemoteUrls: newSelected });
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && releaseState.canProceed) {
      e.preventDefault();
      releaseState.nextServer();
    }
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <Stack gap={4} className="py-2">
        {/* Progress indicator */}
        {totalServers > 1 && (
          <Type small muted>
            Server {currentNumber} of {totalServers}
          </Type>
        )}

        {/* Server icon and info */}
        <div className="flex items-center gap-3">
          <div className="bg-primary/10 flex h-10 w-10 shrink-0 items-center justify-center rounded-lg">
            {currentConfig.server.iconUrl ? (
              <img
                src={currentConfig.server.iconUrl}
                alt=""
                className="h-6 w-6 rounded"
              />
            ) : (
              <ServerIcon className="text-muted-foreground h-5 w-5" />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <Type className="truncate font-medium">
              {currentConfig.server.title ??
                currentConfig.server.registrySpecifier}
            </Type>
            <Type small muted className="truncate">
              {currentConfig.remotes.length} endpoints available
            </Type>
          </div>
        </div>

        {/* Name input */}
        <div className="flex flex-col gap-2">
          <Label>Source name</Label>
          <Input
            placeholder={
              currentConfig.server.title ??
              currentConfig.server.registrySpecifier
            }
            value={currentConfig.name}
            onChange={(e) =>
              releaseState.updateCurrentConfig({ name: e.target.value })
            }
          />
        </div>

        {/* Remote checkboxes */}
        <div className="mt-2 flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>Select endpoints to include</Label>
            <button
              type="button"
              onClick={() => {
                const allSelected =
                  currentConfig.selectedRemoteUrls.size ===
                  currentConfig.remotes.length;
                if (allSelected) {
                  releaseState.updateCurrentConfig({
                    selectedRemoteUrls: new Set(),
                  });
                } else {
                  releaseState.updateCurrentConfig({
                    selectedRemoteUrls: new Set(
                      currentConfig.remotes.map((r) => r.url),
                    ),
                  });
                }
              }}
              className="text-muted-foreground hover:text-foreground text-sm transition-colors"
            >
              {currentConfig.selectedRemoteUrls.size ===
              currentConfig.remotes.length
                ? "Deselect all"
                : "Select all"}
            </button>
          </div>
          <div className="bg-muted/50 max-h-64 space-y-2 overflow-y-auto rounded-lg border p-4">
            {currentConfig.remotes.map((remote) => {
              const isSelected = currentConfig.selectedRemoteUrls.has(
                remote.url,
              );
              const { name, description } = getRemoteDisplayInfo(remote.url);
              return (
                <label
                  key={remote.url}
                  className={cn(
                    "bg-background flex cursor-pointer items-start gap-3 rounded-md border p-3 transition-colors",
                    isSelected
                      ? "border-primary/40"
                      : "border-border hover:border-muted-foreground/30",
                  )}
                >
                  <Checkbox
                    checked={isSelected}
                    onCheckedChange={() => handleRemoteToggle(remote.url)}
                    className="mt-0.5"
                  />
                  <div className="min-w-0 flex-1">
                    <Type small className="font-medium">
                      {name}
                    </Type>
                    <Type small muted>
                      {description}
                    </Type>
                  </div>
                </label>
              );
            })}
          </div>
        </div>
      </Stack>

      <Dialog.Footer className="pt-4">
        <Button variant="tertiary" onClick={onClose}>
          Cancel
        </Button>
        <Button
          disabled={!releaseState.canProceed}
          onClick={() => releaseState.nextServer()}
        >
          <Button.Text>{isLast ? "Continue" : "Next"}</Button.Text>
          <Button.RightIcon>
            <ArrowRight className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </Dialog.Footer>
    </div>
  );
}

// --- Configure Phase ---

function ConfigurePhaseContent({
  releaseState,
  onClose,
}: {
  releaseState: ConfigurePhase;
  onClose: () => void;
}) {
  const routes = useTargetRoutes(releaseState);
  const { existingSpecifiers } = releaseState;

  // Filter out multi-remote servers that were already configured in selectRemotes phase
  const singleRemoteConfigs = releaseState.serverConfigs.filter(
    (c) => !c.selectedRemotes,
  );
  const hasOnlySingleRemote = singleRemoteConfigs.length > 0;
  const effectiveIsSingle = singleRemoteConfigs.length === 1;

  // Multi-remote servers (with selectedRemotes) are always considered "new"
  // because different remote selections make them distinct configurations
  const allAlreadyAdded =
    existingSpecifiers.size > 0 &&
    releaseState.serverConfigs.every(
      (c) =>
        // Multi-remote servers are never "already added" - they're new configs
        !c.selectedRemotes &&
        existingSpecifiers.has(c.server.registrySpecifier),
    );
  const hasNewServers = releaseState.serverConfigs.some(
    (c) =>
      // Multi-remote servers are always "new"
      c.selectedRemotes || !existingSpecifiers.has(c.server.registrySpecifier),
  );

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && releaseState.canDeploy && hasNewServers) {
      e.preventDefault();
      releaseState.startDeployment();
    }
  };

  // Auto-deploy when all servers were multi-remote (already configured in selectRemotes)
  useEffect(() => {
    if (!hasOnlySingleRemote && releaseState.canDeploy && hasNewServers) {
      releaseState.startDeployment();
    }
  }, [hasOnlySingleRemote, releaseState.canDeploy, hasNewServers]);

  // If all servers were multi-remote, show loading state while auto-deploying
  if (!hasOnlySingleRemote && !allAlreadyAdded) {
    return (
      <div className="flex items-center justify-center gap-2 py-4">
        <Loader2 className="h-4 w-4 animate-spin" />
        <Type small muted>
          Starting deployment...
        </Type>
      </div>
    );
  }

  return (
    <div onKeyDown={handleKeyDown}>
      <Stack gap={4} className="py-2">
        {allAlreadyAdded ? (
          <div className="bg-muted/30 flex items-start gap-3 rounded-lg border p-3">
            <AlertCircle className="text-muted-foreground mt-0.5 size-4 shrink-0" />
            <Type small muted>
              {releaseState.serverConfigs.length === 1
                ? "This source is"
                : "All selected sources are"}{" "}
              already in your project.{" "}
              <routes.sources.Link className="text-primary">
                View sources
              </routes.sources.Link>
            </Type>
          </div>
        ) : effectiveIsSingle ? (
          <SingleServerConfig
            releaseState={releaseState}
            singleRemoteConfigs={singleRemoteConfigs}
          />
        ) : (
          <BatchServerConfig
            releaseState={releaseState}
            singleRemoteConfigs={singleRemoteConfigs}
          />
        )}
      </Stack>
      <Dialog.Footer>
        <div className="flex gap-2">
          {releaseState.goBack && (
            <Button variant="tertiary" onClick={releaseState.goBack}>
              Back
            </Button>
          )}
          {(!releaseState.goBack || allAlreadyAdded) && (
            <Button variant="tertiary" onClick={onClose}>
              {allAlreadyAdded ? "Close" : "Cancel"}
            </Button>
          )}
        </div>
        {!allAlreadyAdded && (
          <Button
            disabled={!releaseState.canDeploy || !hasNewServers}
            onClick={() => releaseState.startDeployment()}
          >
            <Button.Text>Deploy</Button.Text>
          </Button>
        )}
      </Dialog.Footer>
    </div>
  );
}

function SingleServerConfig({
  releaseState,
  singleRemoteConfigs,
}: {
  releaseState: ConfigurePhase;
  singleRemoteConfigs: ConfigurePhase["serverConfigs"];
}) {
  const config = singleRemoteConfigs[0];
  if (!config) return null;

  // Find the original index in serverConfigs
  const originalIndex = releaseState.serverConfigs.findIndex(
    (c) => c.server.registrySpecifier === config.server.registrySpecifier,
  );

  return (
    <div className="flex flex-col gap-2">
      <Label>Source name</Label>
      <Input
        placeholder={config.server.title || config.server.registrySpecifier}
        value={config.name}
        onChange={(e) =>
          releaseState.updateServerConfig(originalIndex, {
            name: e.target.value,
          })
        }
      />
    </div>
  );
}

function BatchServerConfig({
  releaseState,
  singleRemoteConfigs,
}: {
  releaseState: ConfigurePhase;
  singleRemoteConfigs: ConfigurePhase["serverConfigs"];
}) {
  const { existingSpecifiers } = releaseState;
  return (
    <div className="max-h-80 space-y-3 overflow-y-auto">
      {singleRemoteConfigs.map((config) => {
        const isAlreadyAdded = existingSpecifiers.has(
          config.server.registrySpecifier,
        );
        // Find the original index in serverConfigs
        const originalIndex = releaseState.serverConfigs.findIndex(
          (c) => c.server.registrySpecifier === config.server.registrySpecifier,
        );
        return (
          <div
            key={config.server.registrySpecifier}
            className={cn(
              "flex items-center gap-3 rounded-lg border p-3",
              isAlreadyAdded && "opacity-50",
            )}
          >
            <div className="bg-primary/10 flex h-6 w-6 shrink-0 items-center justify-center rounded">
              {config.server.iconUrl ? (
                <img
                  src={config.server.iconUrl}
                  alt=""
                  className="h-4 w-4 rounded"
                />
              ) : (
                <ServerIcon className="text-muted-foreground h-3 w-3" />
              )}
            </div>
            <div className="min-w-0 flex-1">
              <Input
                placeholder={
                  config.server.title || config.server.registrySpecifier
                }
                value={config.name}
                onChange={(e) =>
                  releaseState.updateServerConfig(originalIndex, {
                    name: e.target.value,
                  })
                }
                className="text-sm"
              />
            </div>
            {isAlreadyAdded && (
              <span className="text-muted-foreground shrink-0 text-xs">
                Already added
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}

// --- Deploying Phase ---

function DeployingPhaseContent({
  releaseState,
}: {
  releaseState: DeployingPhase;
}) {
  const logsEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [releaseState.deploymentLogs.length]);

  const statusText = (() => {
    const s = releaseState.deploymentStatus;
    if (!s || s === "created") return "Waiting for deployment to start...";
    if (s === "pending") return "Processing deployment...";
    return "Deploying...";
  })();

  return (
    <div className="space-y-4 py-2">
      <Stack direction="horizontal" gap={2} align="center">
        <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
        <Type small muted>
          {statusText}
        </Type>
      </Stack>
      {releaseState.deploymentLogs.length > 0 && (
        <div className="bg-muted/30 max-h-48 space-y-1 overflow-y-auto rounded-lg border p-3 font-mono text-xs">
          {releaseState.deploymentLogs.map((log) => (
            <div
              key={log.id}
              className={log.event.includes("error") ? "text-destructive" : ""}
            >
              {log.message}
            </div>
          ))}
          <div ref={logsEndRef} />
        </div>
      )}
    </div>
  );
}

// --- Complete Phase ---

function CompletePhaseContent({
  releaseState,
  onClose,
}: {
  releaseState: CompletePhase;
  isSingle: boolean;
  onClose: () => void;
}) {
  const allDone = releaseState.toolsetStatuses.every(
    (s) => s.status === "completed" || s.status === "failed",
  );
  const allSucceeded = releaseState.toolsetStatuses.every(
    (s) => s.status === "completed",
  );
  const successCount = releaseState.toolsetStatuses.filter(
    (s) => s.status === "completed",
  ).length;

  return (
    <div className="space-y-4 pb-2">
      {/* Success header when all done */}
      {allDone && allSucceeded && (
        <div className="flex items-center gap-3 rounded-lg border border-emerald-500/20 bg-emerald-500/10 p-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-emerald-500/20">
            <Check className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
          </div>
          <div>
            <Type className="font-medium text-emerald-700 dark:text-emerald-300">
              {successCount === 1
                ? "Server added successfully"
                : `${successCount} servers added successfully`}
            </Type>
          </div>
        </div>
      )}

      {/* Toolset creation progress - only show during creation or if there were failures */}
      {(!allDone || !allSucceeded) && (
        <div>
          <Type small muted className="mb-2">
            {allDone ? "Results" : "Adding servers..."}
          </Type>
          <div className="space-y-1.5">
            {releaseState.toolsetStatuses.map((ts) => (
              <ToolsetStatusRow
                key={ts.slug}
                status={ts}
                releaseState={releaseState}
              />
            ))}
          </div>
        </div>
      )}

      {/* Next steps — only shown when all toolsets are done */}
      {allDone &&
        (() => {
          const firstCompleted = releaseState.toolsetStatuses.find(
            (s) => s.status === "completed" && s.toolsetSlug && s.mcpSlug,
          );
          if (firstCompleted) {
            return (
              <SingleServerNextSteps
                toolsetSlug={firstCompleted.toolsetSlug!}
                mcpSlug={firstCompleted.mcpSlug!}
                releaseState={releaseState}
              />
            );
          }
          return (
            <Dialog.Footer>
              <Button variant="tertiary" onClick={onClose}>
                <Button.Text>Close</Button.Text>
              </Button>
            </Dialog.Footer>
          );
        })()}
    </div>
  );
}

function ToolsetStatusRow({
  status,
  releaseState,
}: {
  status: ServerToolsetStatus;
  releaseState: CompletePhase;
}) {
  const routes = useTargetRoutes(releaseState);
  const isCompleted = status.status === "completed" && status.toolsetSlug;

  const content = (
    <div className="flex items-center gap-3 rounded-lg border p-2">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Type small className="truncate">
          {status.name}
        </Type>
      </div>
      <div className="flex items-center gap-2">
        {isCompleted && (
          <ArrowRight className="text-muted-foreground h-3 w-3" />
        )}
        <ToolsetStatusIcon status={status.status} />
      </div>
    </div>
  );

  if (isCompleted) {
    return (
      <routes.mcp.details.Link
        params={[status.toolsetSlug!]}
        className="block no-underline transition-opacity hover:no-underline hover:opacity-80"
      >
        {content}
      </routes.mcp.details.Link>
    );
  }

  return content;
}

function ToolsetStatusIcon({
  status,
}: {
  status: ServerToolsetStatus["status"];
}) {
  switch (status) {
    case "pending":
      return <Circle className="text-muted-foreground h-4 w-4 shrink-0" />;
    case "creating":
      return (
        <Loader2 className="text-muted-foreground h-4 w-4 shrink-0 animate-spin" />
      );
    case "completed":
      return <Check className="h-4 w-4 shrink-0 text-emerald-500" />;
    case "failed":
      return <X className="text-destructive h-4 w-4 shrink-0" />;
  }
}

function SingleServerNextSteps({
  toolsetSlug,
  mcpSlug,
  releaseState,
}: {
  toolsetSlug: string;
  mcpSlug: string;
  releaseState: CompletePhase;
}) {
  const routes = useTargetRoutes(releaseState);

  return (
    <div>
      <Type className="mb-2 font-medium">Next steps</Type>
      <div className="grid grid-cols-2 gap-2">
        <routes.sources.Link className="no-underline hover:no-underline">
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 rounded-lg border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-blue-500/10 dark:bg-blue-500/20">
              <Plus className="h-4 w-4 text-blue-600 dark:text-blue-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Add more sources
              </Type>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </routes.sources.Link>
        <routes.elements.Link
          className="no-underline hover:no-underline"
          queryParams={{ toolset: toolsetSlug }}
        >
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 rounded-lg border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-violet-500/10 dark:bg-violet-500/20">
              <MessageCircle className="h-4 w-4 text-violet-600 dark:text-violet-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Deploy as chat
              </Type>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </routes.elements.Link>
        <a
          href={`${getServerURL()}/mcp/${mcpSlug}/install`}
          target="_blank"
          rel="noopener noreferrer"
          className="no-underline hover:no-underline"
        >
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 rounded-lg border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-emerald-500/10 dark:bg-emerald-500/20">
              <Plug className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Connect via Claude, Cursor
              </Type>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </a>
        <routes.mcp.details.Link
          params={[toolsetSlug]}
          className="no-underline hover:no-underline"
        >
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 rounded-lg border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-orange-500/10 dark:bg-orange-500/20">
              <Settings className="h-4 w-4 text-orange-600 dark:text-orange-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Configure MCP settings
              </Type>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </routes.mcp.details.Link>
      </div>
    </div>
  );
}

// --- Error Phase ---

function ErrorPhaseContent({
  releaseState,
  onClose,
}: {
  releaseState: ErrorPhase;
  onClose: () => void;
}) {
  const routes = useTargetRoutes(releaseState);

  return (
    <div className="space-y-4 py-2">
      <div className="border-destructive/30 bg-destructive/5 flex items-start gap-3 rounded-lg border p-3">
        <AlertCircle className="text-destructive mt-0.5 h-5 w-5 shrink-0" />
        <div className="flex-1">
          <Type className="text-destructive font-medium">
            Deployment failed
          </Type>
          <Type small className="text-destructive/80 mt-1">
            {releaseState.error}
          </Type>
        </div>
      </div>
      {releaseState.deploymentLogs.length > 0 && (
        <div className="bg-muted/30 max-h-48 space-y-1 overflow-y-auto rounded-lg border p-3 font-mono text-xs">
          {releaseState.deploymentLogs.map((log) => (
            <div
              key={log.id}
              className={log.event.includes("error") ? "text-destructive" : ""}
            >
              {log.message}
            </div>
          ))}
        </div>
      )}
      <Dialog.Footer>
        {releaseState.deploymentId && (
          <routes.deployments.deployment.Link
            params={[releaseState.deploymentId]}
            className="no-underline hover:no-underline"
          >
            <Button variant="secondary">
              <ExternalLink className="h-4 w-4" />
              <Button.Text>View Deployment</Button.Text>
            </Button>
          </routes.deployments.deployment.Link>
        )}
        <Button variant="tertiary" onClick={onClose}>
          <Button.Text>Close</Button.Text>
        </Button>
      </Dialog.Footer>
    </div>
  );
}
