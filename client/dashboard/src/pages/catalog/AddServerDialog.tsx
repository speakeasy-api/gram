import { catalogLogoClassName } from "./logo";
import { CatalogGuardrailsPhase } from "./CatalogGuardrailsPhase";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrganization } from "@/contexts/Auth";
import { Checkbox } from "@/components/ui/Checkbox";
import { CreationIdentityChoice } from "@/pages/mcp/x/tabs/settings/sections/authentication/CreationIdentityChoice";
import { useAgentCredentialFields } from "@/lib/remote-identity";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { invalidateAllListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Stack } from "@/components/ui/Stack";
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
  TriangleAlert,
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
  isFigmaCatalogServer,
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
const EMPTY_SERVERS: PulseMCPServer[] = [];

function useEnrichedServers(
  servers: PulseMCPServer[],
  open: boolean,
  targetProjectSlug?: string,
) {
  const client = useSdkClient();
  const currentProjectSlug = useProjectSlugForRequests();
  const projectSlug = targetProjectSlug ?? currentProjectSlug;
  const queryClient = useQueryClient();
  const [admissionError, setAdmissionError] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ["catalog-install-details", projectSlug, servers],
    enabled: open && servers.length > 0,
    staleTime: 0,
    retry: false,
    refetchOnMount: "always",
    // Do not change the install configuration behind an open dialog.
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    queryFn: () =>
      Promise.all(
        servers.map(async (server) => {
          if (!server.registryId) return filterToHttpRemotes(server);
          const details = await client.mcpRegistries.getServerDetails({
            registryId: server.registryId,
            serverSpecifier: server.registrySpecifier,
            gramProject: projectSlug,
          });
          // Callers may select a subset of endpoints, but never retain an endpoint
          // or its header requirements that the authoritative detail no longer has.
          const remotes = server.remotes?.length
            ? server.remotes.map((remote) => {
                const current = details.remotes?.find(
                  (candidate) =>
                    candidate.url === remote.url &&
                    candidate.transportType === remote.transportType,
                );
                if (!current)
                  throw new Error(
                    "Catalog endpoints changed. Close this dialog and select the server again.",
                  );
                return current;
              })
            : details.remotes;
          return filterToHttpRemotes({
            ...server,
            tools: details.tools,
            remotes,
          });
        }),
      ),
  });
  const error = admissionError ?? query.error?.message ?? null;
  useEffect(() => {
    if (!open) setAdmissionError(null);
  }, [open]);
  useEffect(() => {
    if (error)
      void invalidateAllListMCPCatalog(queryClient).catch(() => undefined);
  }, [error, queryClient]);

  const beforeInstall = async () => {
    // A flag can flip while Configure/Guardrails is open. Never reuse the
    // successful opening request as admission for a later install.
    const current = await query.refetch();
    if (current.isError) return false;
    if (JSON.stringify(current.data) !== JSON.stringify(query.data)) {
      setAdmissionError(
        "Catalog details changed. Close this dialog and select the server again.",
      );
      return false;
    }
    return true;
  };
  return {
    enrichedServers: error ? EMPTY_SERVERS : (query.data ?? EMPTY_SERVERS),
    isLoading: open && servers.length > 0 && query.isFetching,
    error,
    beforeInstall,
  };
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
    beforeInstall,
  } = useEnrichedServers(servers, open, projectSlug);

  // Use enriched servers (with remotes) for the workflow. Callers that run
  // without a visible dialog can never answer the selectRemotes phase, so
  // multi-remote servers install every endpoint for them.
  // Guardrails are an interactive step (there is nobody to draft a policy in a
  // headless or auto-started install) and follow the policy admin permission.
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const mcpScoped =
    useFeatureFlag(FEATURE_FLAGS.mcpScopedPolicies).status === "enabled";
  const releaseState = useRemoteMcpInstallWorkflow({
    servers: enrichedServers,
    beforeInstall,
    projectSlug,
    autoSelectRemotes: !!(autoStartInstall || headless),
    offerGuardrails:
      mcpScoped &&
      !autoStartInstall &&
      !headless &&
      hasScope("org:admin", organization.id) &&
      // Unproxied servers (e.g. Figma) never pass through Speakeasy.
      enrichedServers.some((server) => !isFigmaCatalogServer(server)),
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
      isLoadingDetails ||
      !!detailsError ||
      autoStartRef.current ||
      releaseState.phase !== "configure" ||
      !releaseState.canInstall
    ) {
      return;
    }

    autoStartRef.current = true;
    void releaseState.startInstall();
  }, [autoStartInstall, open, releaseState, isLoadingDetails, detailsError]);

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

    // Dead-end guard: when the issuer lookup failed, canInstall never becomes
    // true and the install never starts. Interactive users see the reason in
    // the configure step, but headless/auto-start callers would wait forever.
    if (
      releaseState.phase === "configure" &&
      releaseState.installBlockedReason
    ) {
      finishedRef.current = true;
      onInstallFinished({
        projectSlug,
        status: "failed",
        succeededCount: 0,
        failedCount: servers.length,
        error: releaseState.installBlockedReason,
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
            <Text muted>Loading server configuration...</Text>
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
            <div className="border-destructive/30 bg-destructive/5 flex items-start gap-3 border p-3">
              <AlertCircle className="text-destructive mt-0.5 h-5 w-5 shrink-0" />
              <Text small className="text-destructive/80">
                {detailsError}
              </Text>
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
    case "guardrails":
      return releaseState.servers.length === 1
        ? `Guardrails for ${releaseState.servers[0]!.name}`
        : `Guardrails for ${releaseState.servers.length} servers`;
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
    case "guardrails":
      return isSingle
        ? "Protect this server before it takes traffic. Suggested from the catalog's tool annotations."
        : "Protect these servers before they take traffic. Suggested from the catalog's tool annotations.";
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
    case "guardrails":
      return (
        <CatalogGuardrailsPhase releaseState={releaseState} onClose={onClose} />
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
          <Text small muted>
            Server {currentNumber} of {totalServers}
          </Text>
        )}

        {/* Server icon and info */}
        <div className="flex items-center gap-3">
          <div className="bg-primary/10 flex h-10 w-10 shrink-0 items-center justify-center">
            {currentConfig.server.iconUrl ? (
              <img
                src={currentConfig.server.iconUrl}
                alt=""
                className={cn(
                  "h-6 w-6",
                  catalogLogoClassName(currentConfig.server.registrySpecifier),
                )}
              />
            ) : (
              <ServerIcon className="text-muted-foreground h-5 w-5" />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <Text className="truncate font-medium">
              {currentConfig.server.title ??
                currentConfig.server.registrySpecifier}
            </Text>
            <Text small muted className="truncate">
              {currentConfig.remotes.length} endpoints available
            </Text>
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
            onChange={(value) =>
              releaseState.updateCurrentConfig({
                name: value,
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
          <div className="bg-muted/50 max-h-64 space-y-2 overflow-y-auto border p-4">
            {currentConfig.remotes.map((remote) => {
              const isSelected = currentConfig.selectedRemoteUrls.has(
                remote.url,
              );
              const { name, description } = getRemoteDisplayInfo(remote.url);
              return (
                <label
                  key={remote.url}
                  className={cn(
                    "bg-background flex cursor-pointer items-start gap-3 border p-3 transition-colors",
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
                    <Text small className="font-medium">
                      {name}
                    </Text>
                    <Text small muted>
                      {description}
                    </Text>
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
  const project = useProject();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canCreateIdentity =
    !rbacLoading && hasScope("project:write", project.id);
  // Multi-remote servers were already named in the selectRemotes phase; only
  // servers with a single endpoint still need a name input here.
  const singleRemoteConfigs = releaseState.serverConfigs.filter(
    (c) => (c.server.remotes ?? []).length <= 1,
  );
  const effectiveIsSingle = singleRemoteConfigs.length === 1;
  const hasHeaderInputs = releaseState.serverConfigs.some(
    (config) => configCollectibleHeaderCount(config) > 0,
  );
  const hasIdentityChoices = releaseState.serverConfigs.some(
    (config) => !isFigmaCatalogServer(config.server),
  );
  // Headers the upstream marks required gate the primary button, but a Skip
  // action always lets the user install now and fill values in from the
  // server's Settings tab later. Bulk installs never collect header values.
  const missingRequiredHeaders = bulk
    ? 0
    : releaseState.serverConfigs.reduce(
        (count, config) => count + missingRequiredHeaderCount(config),
        0,
      );
  // When every server came through the selectRemotes phase and none needs
  // header values, there is nothing left to configure — install immediately.
  const nothingToConfigure =
    singleRemoteConfigs.length === 0 && !hasHeaderInputs && !hasIdentityChoices;

  const userIdentityPermissionBlocked =
    !canCreateIdentity &&
    releaseState.serverConfigs.some((config) => config.identityMode === "user");
  const canSubmit =
    releaseState.canInstall &&
    missingRequiredHeaders === 0 &&
    !userIdentityPermissionBlocked;

  // With guardrails on offer, finishing Configure moves to that step; the
  // install itself starts from there.
  const advance = (configureSkipped = false) => {
    if (releaseState.continueToGuardrails) {
      releaseState.continueToGuardrails({ configureSkipped });
    } else {
      void releaseState.startInstall();
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && canSubmit) {
      e.preventDefault();
      advance();
    }
  };

  useEffect(() => {
    if (nothingToConfigure && releaseState.canInstall) {
      advance(true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only trigger on install readiness changes, not on every releaseState update
  }, [nothingToConfigure, releaseState.canInstall]);

  if (
    nothingToConfigure &&
    releaseState.serverConfigs.length > 0 &&
    !releaseState.installBlockedReason
  ) {
    return (
      <div className="flex items-center justify-center gap-2 py-4">
        <Loader2 className="h-4 w-4 animate-spin" />
        <Text small muted>
          Starting install...
        </Text>
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
        <IdentityConfigurations
          releaseState={releaseState}
          canCreateIdentity={canCreateIdentity}
          rbacLoading={rbacLoading}
        />
        {!bulk && <HeaderValueSections releaseState={releaseState} />}
        {releaseState.installBlockedReason && (
          <InstallBlockedWarning reason={releaseState.installBlockedReason} />
        )}
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
        <div className="flex gap-2">
          {missingRequiredHeaders > 0 && (
            <Button
              variant="secondary"
              // Skipping header values is not a way around the identity
              // permission check: the same install runs either way.
              disabled={
                !releaseState.canInstall || userIdentityPermissionBlocked
              }
              onClick={() => advance()}
            >
              <Button.Text>Skip for now</Button.Text>
            </Button>
          )}
          <Button disabled={!canSubmit} onClick={() => advance()}>
            <Button.Text>
              {releaseState.continueToGuardrails
                ? "Continue"
                : "Add to Project"}
            </Button.Text>
          </Button>
        </div>
      </Dialog.Footer>
    </div>
  );
}

function IdentityConfigurations({
  releaseState,
  canCreateIdentity,
  rbacLoading,
}: {
  releaseState: ConfigurePhase;
  canCreateIdentity: boolean;
  rbacLoading: boolean;
}) {
  const configs = releaseState.serverConfigs.filter(
    (config) => !isFigmaCatalogServer(config.server),
  );
  if (configs.length === 0) return null;

  return (
    <div className="flex flex-col gap-4 border-t pt-4">
      {configs.map((config) => (
        <CatalogServerIdentity
          key={config.server.registrySpecifier}
          config={config}
          showServerName={configs.length > 1}
          index={configIndexOf(releaseState, config)}
          releaseState={releaseState}
          canCreateIdentity={canCreateIdentity}
          rbacLoading={rbacLoading}
        />
      ))}
    </div>
  );
}

/**
 * One server's identity decision. Split out so each row owns the credential
 * form's own state — the shared choice is the same block Add-by-URL shows.
 */
function CatalogServerIdentity({
  config,
  showServerName,
  index,
  releaseState,
  canCreateIdentity,
  rbacLoading,
}: {
  config: ServerConfig;
  showServerName: boolean;
  index: number;
  releaseState: ConfigurePhase;
  canCreateIdentity: boolean;
  rbacLoading: boolean;
}) {
  const credential = useAgentCredentialFields();
  const authorizationValue = credential.authorizationValue;
  // releaseState is rebuilt every render; its updater is not, so depend on the
  // updater alone or this re-runs on every parent render.
  const { updateServerConfig } = releaseState;

  // The credential form owns the value; the workflow config carries it to the
  // install RPC.
  useEffect(() => {
    if (config.agentAuthorization !== authorizationValue) {
      updateServerConfig(index, { agentAuthorization: authorizationValue });
    }
  }, [
    authorizationValue,
    config.agentAuthorization,
    index,
    updateServerConfig,
  ]);

  return (
    <div className="space-y-2">
      {showServerName ? (
        <Text small className="font-medium">
          {config.name}
        </Text>
      ) : null}
      <CreationIdentityChoice
        value={config.identityMode}
        onChange={(identityMode) =>
          releaseState.updateServerConfig(index, { identityMode })
        }
        credential={credential}
        upstreamName={config.name || "this server"}
        advertisesOAuth={!!config.server.supportsDcr}
        authenticationRequired={false}
        canCreateIdentity={canCreateIdentity}
        rbacLoading={rbacLoading}
      />
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
    <Text small muted>
      Already in this project — adding it again creates another server.
    </Text>
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
        onChange={(value) =>
          releaseState.updateServerConfig(originalIndex, {
            name: value,
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
            className="flex flex-col gap-2 border p-3"
          >
            <div className="flex items-center gap-3">
              <div className="bg-primary/10 flex h-6 w-6 shrink-0 items-center justify-center">
                {config.server.iconUrl ? (
                  <img
                    src={config.server.iconUrl}
                    alt=""
                    className={cn(
                      "h-4 w-4",
                      catalogLogoClassName(config.server.registrySpecifier),
                    )}
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
                  onChange={(value) =>
                    releaseState.updateServerConfig(originalIndex, {
                      name: value,
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

function InstallBlockedWarning({ reason }: { reason: string }) {
  return (
    <div className="border-destructive/30 bg-destructive/5 flex items-start gap-2 border p-2">
      <AlertCircle className="text-destructive mt-0.5 h-4 w-4 shrink-0" />
      <Text small className="text-destructive/80">
        {reason}
      </Text>
    </div>
  );
}

function NoRemoteWarning() {
  return (
    <div className="border-destructive/30 bg-destructive/5 flex items-start gap-2 border p-2">
      <AlertCircle className="text-destructive mt-0.5 h-4 w-4 shrink-0" />
      <Text small className="text-destructive/80">
        This server does not expose a compatible remote endpoint and cannot be
        added.
      </Text>
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
      <div className="flex items-center gap-3 border p-4">
        <div className="bg-primary/10 flex h-10 w-10 shrink-0 items-center justify-center">
          <ServerIcon className="text-muted-foreground h-5 w-5" />
        </div>
        <div>
          <Text className="font-medium">
            Installing {totalServers}{" "}
            {totalServers === 1 ? "server" : "servers"}
          </Text>
          <Text small muted>
            All servers will use their default names.
          </Text>
        </div>
      </div>
      {alreadyInstalledCount > 0 && (
        <Text small muted>
          {alreadyInstalledCount} already in this project (a new server is
          created for each).
        </Text>
      )}
    </div>
  );
}

// --- Header inputs ---

function configCollectibleHeaderCount(config: ServerConfig): number {
  return config.remotes.reduce(
    (count, remote) => count + collectibleHeaders(remote).length,
    0,
  );
}

function missingRequiredHeaderCount(config: ServerConfig): number {
  return config.remotes.reduce(
    (count, remote) =>
      count +
      collectibleHeaders(remote).filter(
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
        <Text small muted className="block">
          Values are stored on the server and sent with every upstream request.
          You can skip this step — headers can always be configured later in the
          server's Settings tab.
        </Text>
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
    (remote) => collectibleHeaders(remote).length > 0,
  );
  const showRemoteName = config.remotes.length > 1;

  return (
    <div className="flex flex-col gap-3">
      {showServerName && (
        <Text small className="font-medium">
          {config.name || config.server.registrySpecifier}
        </Text>
      )}
      {remotesWithHeaders.map((remote) => (
        <div key={remote.url} className="flex flex-col gap-2">
          {showRemoteName && (
            <Text small muted>
              {getRemoteDisplayInfo(remote.url).name}
            </Text>
          )}
          {collectibleHeaders(remote).map((header) => (
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
        <Text small className="font-mono">
          {label}
        </Text>
        {required && (
          <Text small muted>
            required
          </Text>
        )}
      </div>
      <Input
        type={secret ? "password" : "text"}
        placeholder={placeholder ?? description ?? ""}
        value={value}
        onChange={onChange}
      />
      {description && (
        <Text small muted>
          {description}
        </Text>
      )}
    </div>
  );
}

// --- Installing / Complete Phases ---

function InstallStatusList({ statuses }: { statuses: ServerInstallStatus[] }) {
  return (
    <div className="space-y-1.5 py-2">
      {statuses.map((status) => (
        <div key={status.key} className="flex items-center gap-3 border p-2">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Text small className="truncate">
              {status.name}
            </Text>
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
        <div className="flex items-center gap-3 border border-emerald-500/20 bg-emerald-500/10 p-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-full bg-emerald-500/20">
            <Check className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
          </div>
          <div>
            <Text className="font-medium text-emerald-700 dark:text-emerald-300">
              {successCount === 1
                ? "Server added successfully"
                : `${successCount} servers added successfully`}
            </Text>
          </div>
        </div>
      )}

      <GuardrailOutcomeNotice outcome={releaseState.guardrail} />

      {/* Per-server results — only shown if something failed */}
      {!allSucceeded && (
        <div>
          <Text small muted className="mb-2">
            Results
          </Text>
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

/** What became of the guardrail requested during install. A failure is spelled
 *  out: the servers exist either way, and the fix is a step on each server. */
export function GuardrailOutcomeNotice({
  outcome,
}: {
  outcome: CompletePhase["guardrail"];
}): JSX.Element | null {
  if (!outcome) return null;
  if (outcome.status === "created") {
    return (
      <div className="border p-3">
        <Text small className="font-medium">
          Guardrail created
        </Text>
        <Text small muted>
          {outcome.name} is active and scoped to the added servers. Review it
          under the server&apos;s Guardrails tab.
        </Text>
      </div>
    );
  }
  return (
    <div className="border-destructive/40 border p-3" role="alert">
      <Text small className="text-destructive font-medium">
        Guardrail was not created
      </Text>
      <Text small muted>
        The servers were added, but &quot;{outcome.name}&quot; could not be
        created: {outcome.error}. Add it from each server&apos;s Guardrails tab.
      </Text>
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
  const setupRequired =
    status.status === "failed" && status.mcpServerParam
      ? status.setupRequired
      : undefined;

  if (setupRequired) {
    // Created, but held disabled until identity is finished: a next step,
    // not a failure, so it reads in warning tones and says what to do.
    return (
      <routes.mcp.x.settings.Link
        params={[status.mcpServerParam!]}
        hash="authentication"
        className="block no-underline transition-opacity hover:no-underline hover:opacity-80"
      >
        <div className="flex items-start gap-3 border p-2">
          <TriangleAlert className="text-default-warning mt-0.5 h-4 w-4 shrink-0" />
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <Text small className="truncate">
              {status.name}
            </Text>
            <Text small muted>
              Added, but disabled until identity is set up. {setupRequired}
            </Text>
          </div>
          <span className="text-foreground flex shrink-0 items-center gap-1 text-xs font-medium">
            Finish setup
            <ArrowRight className="h-3 w-3" />
          </span>
        </div>
      </routes.mcp.x.settings.Link>
    );
  }

  const content = (
    <div className="flex items-center gap-3 border p-2">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Text small className="truncate">
          {status.name}
        </Text>
        {status.error && (
          <Text small className="text-destructive/80 truncate">
            {status.error}
          </Text>
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
      <Text className="mb-2 font-medium">Next steps</Text>
      <div className="grid grid-cols-2 gap-2">
        <routes.mcp.Link className="no-underline hover:no-underline">
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center bg-blue-500/10 dark:bg-blue-500/20">
              <Plus className="h-4 w-4 text-blue-600 dark:text-blue-400" />
            </div>
            <div className="flex-1">
              <Text className="text-sm font-medium no-underline">
                Add more sources
              </Text>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </routes.mcp.Link>
        {status.mcpEndpointUrl && (
          <a
            href={`${status.mcpEndpointUrl}/install`}
            target="_blank"
            rel="noopener noreferrer"
            className="no-underline hover:no-underline"
          >
            <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 border p-3 transition-all [&_*]:no-underline">
              <div className="flex h-8 w-8 shrink-0 items-center justify-center bg-emerald-500/10 dark:bg-emerald-500/20">
                <Plug className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              </div>
              <div className="flex-1">
                <Text className="text-sm font-medium no-underline">
                  Connect via coding agents
                </Text>
              </div>
              <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
          </a>
        )}
        <routes.mcp.x.Link
          params={[status.mcpServerParam!]}
          className="no-underline hover:no-underline"
        >
          <div className="group hover:border-foreground/20 hover:bg-muted/30 flex h-full items-center gap-3 border p-3 transition-all [&_*]:no-underline">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center bg-orange-500/10 dark:bg-orange-500/20">
              <Settings className="h-4 w-4 text-orange-600 dark:text-orange-400" />
            </div>
            <div className="flex-1">
              <Text className="text-sm font-medium no-underline">
                Configure MCP settings
              </Text>
            </div>
            <ArrowRight className="text-muted-foreground h-4 w-4 opacity-0 transition-opacity group-hover:opacity-100" />
          </div>
        </routes.mcp.x.Link>
      </div>
    </div>
  );
}
