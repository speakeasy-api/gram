import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import { Button, Dialog, Input, Stack } from "@speakeasy-api/moonshine";
import {
  AlertCircle,
  ArrowRight,
  Check,
  Circle,
  Loader2,
  Plug,
  Plus,
  Server as ServerIcon,
  Settings,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import {
  type CompletePhase,
  type ConfigurePhase,
  headerValueKey,
  type RemoteMcpInstallWorkflow,
  type SelectRemotesPhase,
  type ServerConfig,
  type ServerInstallStatus,
  useRemoteMcpInstallWorkflow,
} from "./useRemoteMcpInstallWorkflow";
import {
  collectibleHeaders,
  filterToHttpRemotes,
  getRemoteDisplayInfo,
} from "./remotes";

export interface AddServerDialogProps {
  servers: PulseMCPServer[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onServersAdded?: () => void;
  onInstallFinished?: (result: {
    projectSlug?: string;
    status: "succeeded" | "failed";
    succeededCount: number;
    failedCount: number;
    firstCompletedMcpServerId?: string;
    firstCompletedMcpServerParam?: string;
    firstCompletedMcpEndpointUrl?: string;
    completedMcpServerIds?: string[];
    error?: string;
  }) => void;
  projectSlug?: string;
  /** When true, shows a summary view instead of individual name inputs in the configure phase. */
  bulk?: boolean;
  /** When true, starts the install as soon as the default configuration is ready. */
  autoStartInstall?: boolean;
  /** When true, runs the workflow without rendering the dialog UI. */
  headless?: boolean;
}

/**
 * Hook to fetch server details (including remotes and their header
 * requirements) for all servers. The catalog list response can omit remotes or
 * strip their headers, so the details call is the authoritative source for the
 * endpoint data the install flow needs.
 */
function useEnrichedServers(servers: PulseMCPServer[], open: boolean) {
  const client = useSdkClient();
  const [enrichedServers, setEnrichedServers] = useState<PulseMCPServer[]>([]);
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

      let result: PulseMCPServer[];
      try {
        result = await Promise.all(
          servers.map(async (server) => {
            try {
              if (!server.registryId) {
                return server;
              }
              const details = await client.mcpRegistries.getServerDetails({
                registryId: server.registryId,
                serverSpecifier: server.registrySpecifier,
              });

              return {
                ...server,
                remotes: mergeRemoteHeaders(
                  server.remotes,
                  details.remotes as ExternalMCPRemote[] | undefined,
                ),
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
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch details",
        );
        result = servers;
      }

      setEnrichedServers(result.map(filterToHttpRemotes));
      setIsLoading(false);
    };

    void fetchServerDetails();
  }, [open, serversKey, servers, client]);

  return { enrichedServers, isLoading, error };
}

// Keep the list entry's remote selection but adopt header requirements from
// the details response when the list variant lacked them.
function mergeRemoteHeaders(
  existing: ExternalMCPRemote[] | undefined,
  fromDetails: ExternalMCPRemote[] | undefined,
): ExternalMCPRemote[] | undefined {
  if (!existing || existing.length === 0) return fromDetails ?? existing;
  if (!fromDetails || fromDetails.length === 0) return existing;

  const detailsByUrl = new Map(fromDetails.map((r) => [r.url, r]));
  return existing.map((remote) => {
    if (remote.headers && remote.headers.length > 0) return remote;
    const detailed = detailsByUrl.get(remote.url);
    return detailed?.headers?.length
      ? { ...remote, headers: detailed.headers }
      : remote;
  });
}

export function AddServerDialog({
  servers,
  open,
  onOpenChange,
  onServersAdded,
  onInstallFinished,
  projectSlug,
  bulk,
  autoStartInstall,
  headless,
}: AddServerDialogProps): JSX.Element | null {
  // Fetch server details (including remotes) when dialog opens
  const {
    enrichedServers,
    isLoading: isLoadingDetails,
    error: detailsError,
  } = useEnrichedServers(servers, open);

  // Use enriched servers (with remotes) for the workflow. Callers that run
  // without a visible dialog can never answer the selectRemotes phase, so
  // multi-remote servers install every endpoint for them.
  const releaseState = useRemoteMcpInstallWorkflow({
    servers: enrichedServers,
    projectSlug,
    autoSelectRemotes: !!(autoStartInstall || headless),
  });
  const serversKey = servers.map((s) => s.registrySpecifier).join(",");
  const autoStartRef = useRef(false);
  const finishedRef = useRef(false);

  // Reset when dialog closes
  useEffect(() => {
    if (!open) {
      releaseState.reset();
      autoStartRef.current = false;
      finishedRef.current = false;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only reset when dialog open/close state changes, not on every releaseState update
  }, [open]);

  useEffect(() => {
    autoStartRef.current = false;
    finishedRef.current = false;
  }, [projectSlug, serversKey]);

  useEffect(() => {
    if (
      !open ||
      !autoStartInstall ||
      autoStartRef.current ||
      releaseState.phase !== "configure" ||
      !releaseState.canInstall
    ) {
      return;
    }

    autoStartRef.current = true;
    void releaseState.startInstall();
  }, [autoStartInstall, open, releaseState]);

  // Clean up Radix body scroll-lock on unmount (e.g. when navigating away mid-dialog)
  useEffect(() => {
    return () => {
      document.body.style.removeProperty("pointer-events");
    };
  }, []);

  // Notify parent when all installs are done
  const allInstallsDone =
    releaseState.phase === "complete" &&
    releaseState.statuses.length > 0 &&
    releaseState.statuses.every(
      (s) => s.status === "completed" || s.status === "failed",
    );
  const prevAllDoneRef = useRef(false);
  useEffect(() => {
    if (allInstallsDone && !prevAllDoneRef.current) {
      prevAllDoneRef.current = true;
      onServersAdded?.();
    }
    if (!allInstallsDone) {
      prevAllDoneRef.current = false;
    }
  }, [allInstallsDone, onServersAdded]);

  useEffect(() => {
    if (!open || !onInstallFinished || finishedRef.current) return;

    if (detailsError) {
      finishedRef.current = true;
      onInstallFinished({
        projectSlug,
        status: "failed",
        succeededCount: 0,
        failedCount: servers.length,
        error: detailsError,
      });
      return;
    }

    // Dead-end guard: when no server has a compatible endpoint, canInstall
    // never becomes true and the install never starts. Interactive users see
    // the warning in the configure step, but headless/auto-start callers would
    // wait forever — report the failure instead.
    if (
      releaseState.phase === "configure" &&
      releaseState.serverConfigs.length > 0 &&
      releaseState.serverConfigs.every((config) => config.remotes.length === 0)
    ) {
      finishedRef.current = true;
      onInstallFinished({
        projectSlug,
        status: "failed",
        succeededCount: 0,
        failedCount: servers.length,
        error:
          "None of the selected servers expose a compatible remote endpoint.",
      });
      return;
    }

    if (releaseState.phase !== "complete") return;

    const statuses = releaseState.statuses;
    if (statuses.length === 0) return;

    const succeededCount = statuses.filter(
      (s) => s.status === "completed",
    ).length;
    const failedCount = statuses.filter((s) => s.status === "failed").length;
    const firstCompleted = statuses.find(
      (s) => s.status === "completed" && s.mcpServerParam,
    );

    finishedRef.current = true;
    onInstallFinished({
      projectSlug,
      status: failedCount === 0 ? "succeeded" : "failed",
      succeededCount,
      failedCount,
      firstCompletedMcpServerId: firstCompleted?.mcpServerId,
      firstCompletedMcpServerParam: firstCompleted?.mcpServerParam,
      firstCompletedMcpEndpointUrl: firstCompleted?.mcpEndpointUrl,
      completedMcpServerIds: statuses.flatMap((s) =>
        s.status === "completed" && s.mcpServerId ? [s.mcpServerId] : [],
      ),
      error: statuses
        .filter((s) => s.status === "failed" && s.error)
        .map((s) => `${s.name}: ${s.error}`)
        .join("\n"),
    });
  }, [
    detailsError,
    onInstallFinished,
    open,
    projectSlug,
    releaseState,
    servers.length,
  ]);

  if (servers.length === 0) return null;

  if (headless) return null;

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
  const title = dialogTitle(releaseState, isSingle, enrichedServers.length);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="gap-2">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>
            {phaseDescription(releaseState.phase, isSingle)}
          </Dialog.Description>
        </Dialog.Header>
        <PhaseContent
          releaseState={releaseState}
          isSingle={isSingle}
          bulk={bulk}
          onClose={() => onOpenChange(false)}
        />
      </Dialog.Content>
    </Dialog>
  );
}

function dialogTitle(
  releaseState: RemoteMcpInstallWorkflow,
  isSingle: boolean,
  serverCount: number,
): string {
  switch (releaseState.phase) {
    case "complete":
      return "Added to Project";
    case "installing":
      return "Adding to Project";
    case "selectRemotes": {
      const config =
        releaseState.multiRemoteConfigs[releaseState.currentServerIndex];
      return `Configure ${config?.server.title ?? config?.server.registrySpecifier ?? "Server"}`;
    }
    case "configure":
      return isSingle
        ? "Add to Project"
        : `Add ${serverCount} servers to project`;
  }
}

function phaseDescription(
  phase: RemoteMcpInstallWorkflow["phase"],
  isSingle: boolean,
): string {
  switch (phase) {
    case "selectRemotes":
      return "This server has multiple endpoints. Select which ones to include.";
    case "configure":
      return isSingle
        ? "Add this MCP server to your project."
        : "Configure and add these MCP servers to your project.";
    case "installing":
      return "Creating MCP servers...";
    case "complete":
      return "";
  }
}

function PhaseContent({
  releaseState,
  isSingle,
  bulk,
  onClose,
}: {
  releaseState: RemoteMcpInstallWorkflow;
  isSingle: boolean;
  bulk?: boolean;
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
        <ConfigurePhaseContent
          releaseState={releaseState}
          bulk={bulk}
          onClose={onClose}
        />
      );
    case "installing":
      return <InstallStatusList statuses={releaseState.statuses} />;
    case "complete":
      return (
        <CompletePhaseContent
          releaseState={releaseState}
          isSingle={isSingle}
          onClose={onClose}
        />
      );
  }
}

/** Routes scoped to the target project (which may differ from the current project). */
function useTargetRoutes(releaseState: RemoteMcpInstallWorkflow) {
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
          <Label>Server name</Label>
          <Input
            placeholder={
              currentConfig.server.title ??
              currentConfig.server.registrySpecifier
            }
            value={currentConfig.name}
            onChange={(e) =>
              releaseState.updateCurrentConfig({
                name: e.target.value,
              })
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
  bulk,
  onClose,
}: {
  releaseState: ConfigurePhase;
  bulk?: boolean;
  onClose: () => void;
}) {
  // Multi-remote servers were already named in the selectRemotes phase; only
  // servers with a single endpoint still need a name input here.
  const singleRemoteConfigs = releaseState.serverConfigs.filter(
    (c) => (c.server.remotes ?? []).length <= 1,
  );
  const effectiveIsSingle = singleRemoteConfigs.length === 1;
  const hasHeaderInputs = releaseState.serverConfigs.some(
    (config) => configCollectibleHeaderCount(config) > 0,
  );
  // Headers the upstream marks required must be filled before installing —
  // omitting them would create a server that cannot authenticate. Bulk
  // installs never collect header values, so they are exempt.
  const missingRequiredHeaders = bulk
    ? 0
    : releaseState.serverConfigs.reduce(
        (count, config) => count + missingRequiredHeaderCount(config),
        0,
      );
  // When every server came through the selectRemotes phase and none needs
  // header values, there is nothing left to configure — install immediately.
  const nothingToConfigure =
    singleRemoteConfigs.length === 0 && !hasHeaderInputs;

  const canSubmit = releaseState.canInstall && missingRequiredHeaders === 0;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && canSubmit) {
      e.preventDefault();
      void releaseState.startInstall();
    }
  };

  useEffect(() => {
    if (nothingToConfigure && releaseState.canInstall) {
      void releaseState.startInstall();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only trigger on install readiness changes, not on every releaseState update
  }, [nothingToConfigure, releaseState.canInstall]);

  if (nothingToConfigure && releaseState.serverConfigs.length > 0) {
    return (
      <div className="flex items-center justify-center gap-2 py-4">
        <Loader2 className="h-4 w-4 animate-spin" />
        <Type small muted>
          Starting install...
        </Type>
      </div>
    );
  }

  return (
    <div onKeyDown={handleKeyDown}>
      <Stack gap={4} className="py-2">
        {bulk ? (
          <BulkInstallSummary releaseState={releaseState} />
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
        {!bulk && <HeaderValueSections releaseState={releaseState} />}
      </Stack>
      <Dialog.Footer>
        <div className="flex gap-2">
          {releaseState.goBack && (
            <Button variant="tertiary" onClick={releaseState.goBack}>
              Back
            </Button>
          )}
          <Button variant="tertiary" onClick={onClose}>
            Cancel
          </Button>
        </div>
        <Button
          disabled={!canSubmit}
          onClick={() => {
            void releaseState.startInstall();
          }}
        >
          <Button.Text>Add to Project</Button.Text>
        </Button>
      </Dialog.Footer>
    </div>
  );
}

function configIndexOf(releaseState: ConfigurePhase, config: ServerConfig) {
  return releaseState.serverConfigs.indexOf(config);
}

function AlreadyInstalledHint({
  releaseState,
  config,
}: {
  releaseState: ConfigurePhase;
  config: ServerConfig;
}) {
  if (!releaseState.isServerAlreadyInstalled(config.server)) return null;
  return (
    <Type small muted>
      Already in this project — adding it again creates another server.
    </Type>
  );
}

function SingleServerConfig({
  releaseState,
  singleRemoteConfigs,
}: {
  releaseState: ConfigurePhase;
  singleRemoteConfigs: ServerConfig[];
}) {
  const config = singleRemoteConfigs[0];
  if (!config) return null;

  const originalIndex = configIndexOf(releaseState, config);

  return (
    <div className="flex flex-col gap-2">
      <Label>Server name</Label>
      <Input
        placeholder={config.server.title || config.server.registrySpecifier}
        value={config.name}
        onChange={(e) =>
          releaseState.updateServerConfig(originalIndex, {
            name: e.target.value,
          })
        }
      />
      {config.remotes.length === 0 && <NoRemoteWarning />}
      <AlreadyInstalledHint releaseState={releaseState} config={config} />
    </div>
  );
}

function BatchServerConfig({
  releaseState,
  singleRemoteConfigs,
}: {
  releaseState: ConfigurePhase;
  singleRemoteConfigs: ServerConfig[];
}) {
  return (
    <div className="max-h-80 space-y-3 overflow-y-auto">
      {singleRemoteConfigs.map((config) => {
        const originalIndex = configIndexOf(releaseState, config);
        return (
          <div
            key={config.server.registrySpecifier}
            className="flex flex-col gap-2 rounded-lg border p-3"
          >
            <div className="flex items-center gap-3">
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
            </div>
            {config.remotes.length === 0 && <NoRemoteWarning />}
            <AlreadyInstalledHint releaseState={releaseState} config={config} />
          </div>
        );
      })}
    </div>
  );
}

function NoRemoteWarning() {
  return (
    <div className="border-destructive/30 bg-destructive/5 flex items-start gap-2 rounded-md border p-2">
      <AlertCircle className="text-destructive mt-0.5 h-4 w-4 shrink-0" />
      <Type small className="text-destructive/80">
        This server does not expose a compatible remote endpoint and cannot be
        added.
      </Type>
    </div>
  );
}

function BulkInstallSummary({
  releaseState,
}: {
  releaseState: ConfigurePhase;
}) {
  const totalServers = releaseState.serverConfigs.length;
  const alreadyInstalledCount = releaseState.serverConfigs.filter((c) =>
    releaseState.isServerAlreadyInstalled(c.server),
  ).length;

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 rounded-lg border p-4">
        <div className="bg-primary/10 flex h-10 w-10 shrink-0 items-center justify-center rounded-lg">
          <ServerIcon className="text-muted-foreground h-5 w-5" />
        </div>
        <div>
          <Type className="font-medium">
            Installing {totalServers}{" "}
            {totalServers === 1 ? "server" : "servers"}
          </Type>
          <Type small muted>
            All servers will use their default names.
          </Type>
        </div>
      </div>
      {alreadyInstalledCount > 0 && (
        <Type small muted>
          {alreadyInstalledCount} already in this project (a new server is
          created for each).
        </Type>
      )}
    </div>
  );
}

// --- Header inputs ---

function configCollectibleHeaderCount(config: ServerConfig): number {
  return config.remotes.reduce(
    (count, remote) => count + collectibleHeaders(config.server, remote).length,
    0,
  );
}

function missingRequiredHeaderCount(config: ServerConfig): number {
  return config.remotes.reduce(
    (count, remote) =>
      count +
      collectibleHeaders(config.server, remote).filter(
        (header) =>
          (header.isRequired ?? false) &&
          !config.headerValues[headerValueKey(remote.url, header.name)]?.trim(),
      ).length,
    0,
  );
}

/**
 * Optional upstream header values, collected per endpoint. Values left blank
 * are simply not saved — headers can always be configured later from the
 * server's Settings tab.
 */
function HeaderValueSections({
  releaseState,
}: {
  releaseState: ConfigurePhase;
}) {
  const configsWithHeaders = releaseState.serverConfigs.filter(
    (config) => configCollectibleHeaderCount(config) > 0,
  );
  if (configsWithHeaders.length === 0) return null;

  const showServerName = releaseState.serverConfigs.length > 1;

  return (
    <div className="flex flex-col gap-3">
      <div>
        <Label>Upstream headers</Label>
        <Type small muted className="block">
          Values are stored on the server and sent with every upstream request.
          Required headers must be set now; optional ones can be left blank and
          configured later in Settings.
        </Type>
      </div>
      <div className="max-h-64 space-y-3 overflow-y-auto">
        {configsWithHeaders.map((config) => (
          <HeaderValueConfig
            key={config.server.registrySpecifier}
            releaseState={releaseState}
            config={config}
            showServerName={showServerName}
          />
        ))}
      </div>
    </div>
  );
}

function HeaderValueConfig({
  releaseState,
  config,
  showServerName,
}: {
  releaseState: ConfigurePhase;
  config: ServerConfig;
  showServerName: boolean;
}) {
  const configIndex = configIndexOf(releaseState, config);
  const remotesWithHeaders = config.remotes.filter(
    (remote) => collectibleHeaders(config.server, remote).length > 0,
  );
  const showRemoteName = config.remotes.length > 1;

  return (
    <div className="flex flex-col gap-3">
      {showServerName && (
        <Type small className="font-medium">
          {config.name || config.server.registrySpecifier}
        </Type>
      )}
      {remotesWithHeaders.map((remote) => (
        <div key={remote.url} className="flex flex-col gap-2">
          {showRemoteName && (
            <Type small muted>
              {getRemoteDisplayInfo(remote.url).name}
            </Type>
          )}
          {collectibleHeaders(config.server, remote).map((header) => (
            <HeaderValueField
              key={header.name}
              label={header.name}
              required={header.isRequired ?? false}
              secret={header.isSecret ?? false}
              description={header.description}
              placeholder={header.placeholder}
              value={
                config.headerValues[headerValueKey(remote.url, header.name)] ??
                ""
              }
              onChange={(value) =>
                releaseState.setHeaderValue(
                  configIndex,
                  remote.url,
                  header.name,
                  value,
                )
              }
            />
          ))}
        </div>
      ))}
    </div>
  );
}

function HeaderValueField({
  label,
  required,
  secret,
  description,
  placeholder,
  value,
  onChange,
}: {
  label: string;
  required: boolean;
  secret: boolean;
  description?: string;
  placeholder?: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline gap-2">
        <Type small className="font-mono">
          {label}
        </Type>
        {required && (
          <Type small muted>
            required
          </Type>
        )}
      </div>
      <Input
        type={secret ? "password" : "text"}
        placeholder={placeholder ?? description ?? ""}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {description && (
        <Type small muted>
          {description}
        </Type>
      )}
    </div>
  );
}

// --- Installing / Complete Phases ---

function InstallStatusList({ statuses }: { statuses: ServerInstallStatus[] }) {
  return (
    <div className="space-y-1.5 py-2">
      {statuses.map((status) => (
        <div
          key={status.key}
          className="flex items-center gap-3 rounded-lg border p-2"
        >
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Type small className="truncate">
              {status.name}
            </Type>
          </div>
          <InstallStatusIcon status={status.status} />
        </div>
      ))}
    </div>
  );
}

function CompletePhaseContent({
  releaseState,
  onClose,
}: {
  releaseState: CompletePhase;
  isSingle: boolean;
  onClose: () => void;
}) {
  const allSucceeded = releaseState.statuses.every(
    (s) => s.status === "completed",
  );
  const successCount = releaseState.statuses.filter(
    (s) => s.status === "completed",
  ).length;
  const firstCompleted = releaseState.statuses.find(
    (s) => s.status === "completed" && s.mcpServerParam,
  );

  return (
    <div className="space-y-4 pb-2">
      {/* Success header when all done */}
      {allSucceeded && (
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

      {/* Per-server results — only shown if something failed */}
      {!allSucceeded && (
        <div>
          <Type small muted className="mb-2">
            Results
          </Type>
          <div className="space-y-1.5">
            {releaseState.statuses.map((status) => (
              <InstallStatusRow
                key={status.key}
                status={status}
                releaseState={releaseState}
              />
            ))}
          </div>
        </div>
      )}

      {firstCompleted ? (
        <NextSteps status={firstCompleted} releaseState={releaseState} />
      ) : (
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onClose}>
            <Button.Text>Close</Button.Text>
          </Button>
        </Dialog.Footer>
      )}
    </div>
  );
}

function InstallStatusRow({
  status,
  releaseState,
}: {
  status: ServerInstallStatus;
  releaseState: CompletePhase;
}) {
  const routes = useTargetRoutes(releaseState);
  const isCompleted = status.status === "completed" && status.mcpServerParam;

  const content = (
    <div className="flex items-center gap-3 rounded-lg border p-2">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Type small className="truncate">
          {status.name}
        </Type>
        {status.error && (
          <Type small className="text-destructive/80 truncate">
            {status.error}
          </Type>
        )}
      </div>
      <div className="flex items-center gap-2">
        {isCompleted && (
          <ArrowRight className="text-muted-foreground h-3 w-3" />
        )}
        <InstallStatusIcon status={status.status} />
      </div>
    </div>
  );

  if (isCompleted) {
    return (
      <routes.mcp.x.Link
        params={[status.mcpServerParam!]}
        className="block no-underline transition-opacity hover:no-underline hover:opacity-80"
      >
        {content}
      </routes.mcp.x.Link>
    );
  }

  return content;
}

function InstallStatusIcon({
  status,
}: {
  status: ServerInstallStatus["status"];
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

function NextSteps({
  status,
  releaseState,
}: {
  status: ServerInstallStatus;
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
        {status.mcpEndpointUrl && (
          <a
            href={`${status.mcpEndpointUrl}/install`}
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
                  Connect via Claude, Cursor, Codex
                </Type>
              </div>
              <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
          </a>
        )}
        <routes.mcp.x.Link
          params={[status.mcpServerParam!]}
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
        </routes.mcp.x.Link>
      </div>
    </div>
  );
}
