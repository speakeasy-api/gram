import { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  Boxes,
  Check,
  Loader2,
  Search,
  Server as ServerIcon,
} from "lucide-react";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { StepContainer } from "../step-container";
import {
  type PulseMCPServer,
  useInfiniteListMCPCatalog,
} from "@/pages/catalog/hooks";
import { useExternalMcpReleaseWorkflow } from "@/pages/catalog/useExternalMcpReleaseWorkflow";
import { useQueryClient } from "@tanstack/react-query";
import {
  invalidateAllListToolsets,
  useListToolsets,
} from "@gram/client/react-query/listToolsets";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import {
  invalidateAllPlugins,
  usePlugins,
} from "@gram/client/react-query/plugins";
import {
  invalidateAllPlugin,
  usePlugin,
} from "@gram/client/react-query/plugin";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { Input } from "@/components/ui/input";
import { CopyButton } from "@/components/ui/copy-button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { InstallInstructionsButton } from "@/pages/plugins/InstallInstructionsDialog";
import { ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG } from "@/lib/externalMcpUserSessions";
import { cn } from "@/lib/utils";
import { isAutoConfigurableServer } from "./auto-configurable-servers";

/** Display name of the shared plugin bundle catalog servers are added to. */
const DEFAULT_PLUGIN_NAME = "Default";
/** Max catalog servers shown before the user expands the list. */
const INITIAL_VISIBLE = 10;

interface DistributeServersStepProps {
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
}

/** Stable selection key for a catalog server, matching the catalog page convention. */
function serverKey(server: PulseMCPServer): string {
  return `${server.registryId}-${server.registrySpecifier}`;
}

type DrawerStep = "adding" | "done";

export function DistributeServersStep({
  onComplete,
  onSkip,
  onBack,
}: DistributeServersStepProps): JSX.Element {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const queryClient = useQueryClient();
  const [query, setQuery] = useState("");
  const [showAll, setShowAll] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // Drawer state: the deploy → bundle → publish flow runs entirely inside our
  // own Sheet, not the catalog's AddServerDialog.
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerStep, setDrawerStep] = useState<DrawerStep>("adding");
  const [drawerError, setDrawerError] = useState<string | null>(null);
  // Servers handed to the release workflow once the user hits Distribute.
  const [serversToDeploy, setServersToDeploy] = useState<PulseMCPServer[]>([]);

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteListMCPCatalog(query || undefined);
  const { data: toolsetsData, refetch: refetchToolsets } = useListToolsets();
  const { data: publishStatus } = usePublishStatus();

  // Default-plugin membership: map its toolset-backed servers back to catalog
  // registry specifiers so we can flag servers that are already distributed.
  const { data: pluginsData } = usePlugins();
  const defaultPlugin = pluginsData?.plugins.find(
    (p) => p.name === DEFAULT_PLUGIN_NAME,
  );
  const { data: defaultPluginFull } = usePlugin(
    { id: defaultPlugin?.id ?? "" },
    undefined,
    { enabled: !!defaultPlugin?.id },
  );

  const distributedSpecifiers = useMemo(() => {
    const toolsetById = new Map(
      (toolsetsData?.toolsets ?? []).map((t) => [t.id, t]),
    );
    const specs = new Set<string>();
    for (const server of defaultPluginFull?.servers ?? []) {
      if (!server.toolsetId) continue;
      const spec = toolsetById.get(server.toolsetId)?.origin?.registrySpecifier;
      if (spec) specs.add(spec);
    }
    return specs;
  }, [defaultPluginFull?.servers, toolsetsData?.toolsets]);

  const servers = useMemo(
    () => data?.pages.flatMap((page) => page.servers as PulseMCPServer[]) ?? [],
    [data],
  );
  // Onboarding only surfaces servers Gram can fully auto-configure (no-auth or
  // OAuth with dynamic client registration). Everything else would dead-end on
  // "OAuth setup required" or a missing API key, so we steer the user to the
  // full catalog for those (see note below the list).
  const autoConfigurableServers = useMemo(
    () => servers.filter((s) => isAutoConfigurableServer(s.registrySpecifier)),
    [servers],
  );
  const visibleServers = showAll
    ? autoConfigurableServers
    : autoConfigurableServers.slice(0, INITIAL_VISIBLE);

  const toggle = (key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  const selectedServerObjects = useMemo(
    () =>
      autoConfigurableServers.filter(
        (s) =>
          selected.has(serverKey(s)) &&
          !distributedSpecifiers.has(s.registrySpecifier),
      ),
    [autoConfigurableServers, selected, distributedSpecifiers],
  );

  // --- Deploy workflow (driven headlessly, no dialog) -----------------------
  // Raw catalog servers are fed in un-enriched, so each partitions as a
  // single-remote server and the backend resolves all remotes at deploy time.
  // Mirror the catalog AddServerDialog: gate Speakeasy OAuth user-session
  // onboarding behind the same flag so distributed servers that require OAuth
  // are auto-configured instead of surfacing "OAuth setup required" later.
  const workflow = useExternalMcpReleaseWorkflow({
    servers: serversToDeploy,
    onboardExternalMcpToUserSessions:
      telemetry.isFeatureEnabled(ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG) ??
      false,
  });
  const startedRef = useRef(false);
  const finishedRef = useRef(false);

  /**
   * Bundle the freshly deployed toolsets into the shared "Default" plugin and
   * republish the marketplace so the org's users receive them.
   */
  const finishDistribution = async () => {
    try {
      const specifiers = new Set(
        serversToDeploy.map((s) => s.registrySpecifier),
      );
      const { data: fresh } = await refetchToolsets();
      const toAdd = (fresh?.toolsets ?? []).filter(
        (t) =>
          t.origin?.registrySpecifier &&
          specifiers.has(t.origin.registrySpecifier),
      );
      if (toAdd.length === 0) {
        setDrawerError("No servers were deployed. Try again.");
        return;
      }

      const { plugins } = await client.plugins.listPlugins();
      const plugin =
        plugins.find((p) => p.name === DEFAULT_PLUGIN_NAME) ??
        (await client.plugins.createPlugin({
          createPluginForm: { name: DEFAULT_PLUGIN_NAME },
        }));

      const full = await client.plugins.getPlugin({ id: plugin.id });
      const alreadyBundled = new Set(
        (full.servers ?? [])
          .map((s) => s.toolsetId)
          .filter((id): id is string => !!id),
      );

      for (const toolset of toAdd) {
        if (alreadyBundled.has(toolset.id)) continue;
        await client.plugins.addPluginServer({
          addPluginServerForm: {
            pluginId: plugin.id,
            toolsetId: toolset.id,
            displayName: toolset.name,
            policy: "required",
          },
        });
      }

      if (publishStatus?.connected) {
        await client.plugins.publishPlugins({
          publishPluginsRequestBody: { githubUsernames: [] },
        });
      }

      // Refresh plugin + toolset state so the just-distributed servers flip to
      // "Added" (disabled) in the grid, and drop them from the selection.
      await Promise.all([
        invalidateAllPlugins(queryClient),
        invalidateAllPlugin(queryClient),
        invalidateAllListToolsets(queryClient),
      ]);
      setSelected(new Set());

      setDrawerStep("done");
    } catch (error) {
      setDrawerError(
        error instanceof Error
          ? error.message
          : "Failed to add servers to your marketplace",
      );
    }
  };

  // Drive the workflow: auto-start the deploy, then bundle + publish once all
  // toolsets are created. Guarded by refs so each fires exactly once per run.
  useEffect(() => {
    if (!drawerOpen || drawerError) return;

    if (workflow.phase === "configure") {
      if (
        !startedRef.current &&
        workflow.canDeploy &&
        !workflow.isInstallStateLoading
      ) {
        startedRef.current = true;
        void workflow.startDeployment();
      }
      return;
    }

    if (workflow.phase === "complete" && !finishedRef.current) {
      const done =
        workflow.toolsetStatuses.length > 0 &&
        workflow.toolsetStatuses.every(
          (s) => s.status === "completed" || s.status === "failed",
        );
      if (done) {
        finishedRef.current = true;
        void finishDistribution();
      }
      return;
    }

    if (workflow.phase === "error" && !finishedRef.current) {
      finishedRef.current = true;
      setDrawerError(workflow.error);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- guarded by refs; re-run on workflow identity to observe phase + toolset-status changes
  }, [workflow, drawerOpen, drawerError]);

  const handleDistribute = () => {
    if (selectedServerObjects.length === 0) return;
    startedRef.current = false;
    finishedRef.current = false;
    setDrawerError(null);
    setDrawerStep("adding");
    setServersToDeploy(selectedServerObjects);
    setDrawerOpen(true);
  };

  // Open the drawer straight to the install-instructions step for a server
  // that's already distributed — no deploy. Refs are pre-tripped so the
  // workflow effect stays inert.
  const showInstructions = () => {
    startedRef.current = true;
    finishedRef.current = true;
    setServersToDeploy([]);
    setDrawerError(null);
    setDrawerStep("done");
    setDrawerOpen(true);
  };

  const resetDrawer = () => {
    setDrawerOpen(false);
    setServersToDeploy([]);
    setDrawerError(null);
    startedRef.current = false;
    finishedRef.current = false;
    workflow.reset();
  };

  const isAdding = drawerStep === "adding" && !drawerError;
  const drawerIdx = drawerStep === "done" ? 1 : 0;
  const marketplaceCommand = publishStatus?.marketplaceUrl
    ? `/plugin marketplace add ${publishStatus.marketplaceUrl}`
    : null;
  const continueLabel =
    selected.size > 0
      ? `Distribute ${selected.size} server${selected.size === 1 ? "" : "s"}`
      : "Distribute servers";

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Boxes className="text-foreground h-6 w-6" />
        </div>
      }
      title="Distribute MCP servers"
      description="Choose some MCP Servers to distribute to your organization. Selected servers are deployed to your project, bundled into your Default plugin, and published to your marketplace so your team can install them."
      onContinue={handleDistribute}
      onSkip={onSkip}
      skipLabel="Skip for now"
      continueLabel={continueLabel}
      isLoading={drawerOpen && isAdding}
      canContinue={selected.size > 0}
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        <div>
          <label className="text-foreground text-sm font-medium">
            Select servers from the catalog
          </label>
          <div className="relative mt-3">
            <Search className="text-muted-foreground pointer-events-none absolute top-[18px] left-3 h-4 w-4 -translate-y-1/2" />
            <Input
              type="search"
              value={query}
              onChange={(value) => {
                setQuery(value);
                setShowAll(false);
              }}
              placeholder="Search MCP servers"
              className="pl-9"
            />
          </div>

          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
            </div>
          ) : autoConfigurableServers.length === 0 ? (
            <p className="text-muted-foreground mt-3 text-sm">
              {query
                ? `No auto-configurable servers match "${query}".`
                : "No auto-configurable servers available."}
            </p>
          ) : (
            <div className="mt-3 grid grid-cols-2 gap-3">
              {visibleServers.map((server) => {
                const key = serverKey(server);
                const isDistributed = distributedSpecifiers.has(
                  server.registrySpecifier,
                );
                const isSelected = selected.has(key);
                return (
                  <button
                    key={key}
                    type="button"
                    onClick={() => {
                      if (isDistributed) {
                        showInstructions();
                      } else {
                        toggle(key);
                      }
                    }}
                    className={cn(
                      "flex min-h-[118px] items-start gap-3 rounded-lg border p-4 text-left transition-all",
                      isSelected && !isDistributed
                        ? "border-foreground bg-secondary"
                        : "border-border bg-card hover:border-foreground/30",
                    )}
                  >
                    <div className="flex h-10 w-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-lg bg-white">
                      {server.iconUrl ? (
                        <img
                          src={server.iconUrl}
                          alt=""
                          className="h-6 w-6 rounded"
                        />
                      ) : (
                        <ServerIcon className="h-5 w-5 text-neutral-600" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <span className="text-foreground block truncate text-sm font-medium">
                        {server.title ?? server.registrySpecifier}
                      </span>
                      <span className="text-muted-foreground block text-xs">
                        {server.description ?? server.registrySpecifier}
                      </span>
                    </div>
                    {isDistributed ? (
                      <Badge variant="success" className="flex-shrink-0">
                        <Badge.LeftIcon>
                          <Check className="h-3 w-3" />
                        </Badge.LeftIcon>
                        <Badge.Text>Added</Badge.Text>
                      </Badge>
                    ) : (
                      isSelected && (
                        <Check className="text-foreground h-4 w-4 flex-shrink-0" />
                      )
                    )}
                  </button>
                );
              })}
            </div>
          )}

          {!showAll && autoConfigurableServers.length > INITIAL_VISIBLE && (
            <button
              type="button"
              onClick={() => setShowAll(true)}
              className="text-muted-foreground hover:text-foreground mt-2 flex w-full items-center justify-center gap-1.5 py-2 text-sm transition-colors"
            >
              Show more servers
            </button>
          )}
          {showAll && hasNextPage && !query && (
            <button
              type="button"
              onClick={() => void fetchNextPage()}
              disabled={isFetchingNextPage}
              className="text-muted-foreground hover:text-foreground mt-2 flex w-full items-center justify-center gap-1.5 py-2 text-sm transition-colors disabled:opacity-50"
            >
              {isFetchingNextPage ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                "Load more servers"
              )}
            </button>
          )}

          {!isLoading && (
            <p className="text-muted-foreground mt-4 text-xs leading-relaxed">
              Only servers that support OAuth dynamic client registration (DCR)
              are shown here — Gram can configure these automatically. More
              servers, including those that need manual OAuth or API key setup,
              are available in the{" "}
              <routes.catalog.Link className="underline underline-offset-2 hover:text-foreground">
                catalog
              </routes.catalog.Link>
              .
            </p>
          )}
        </div>
      </div>

      <Sheet
        open={drawerOpen}
        onOpenChange={(open) => {
          // Block dismissing mid-deploy so we don't orphan an in-flight run.
          if (!open && isAdding) return;
          if (!open) resetDrawer();
        }}
      >
        <SheetContent
          side="right"
          className="flex w-full flex-col overflow-hidden p-0 sm:max-w-[662px]"
        >
          <SheetHeader className="sr-only">
            <SheetTitle>Distribute MCP servers</SheetTitle>
            <SheetDescription>
              Deploy the selected servers and publish them to your marketplace.
            </SheetDescription>
          </SheetHeader>

          {/* Step progress */}
          <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
            {[0, 1].map((idx) => (
              <div
                key={idx}
                className={cn(
                  "h-1 rounded-full transition-all",
                  idx === drawerIdx
                    ? "bg-foreground w-6"
                    : idx < drawerIdx
                      ? "bg-foreground/40 w-4"
                      : "bg-border w-4",
                )}
              />
            ))}
            <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
              {drawerIdx + 1}/2
            </span>
          </div>

          {/* Sliding steps */}
          <div className="relative flex-1 overflow-hidden">
            <div
              className="flex h-full transition-transform duration-300 ease-in-out"
              style={{ transform: `translateX(-${drawerIdx * 100}%)` }}
            >
              {/* Step 1 — Adding */}
              <div className="w-full shrink-0 space-y-3 overflow-y-auto px-6 pb-4">
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step 1
                </p>
                <h4 className="text-foreground text-base font-medium">
                  Adding servers
                </h4>
                <p className="text-muted-foreground text-sm leading-relaxed">
                  Deploying {serversToDeploy.length}{" "}
                  {serversToDeploy.length === 1 ? "server" : "servers"} and
                  publishing them to your marketplace. This can take a moment.
                </p>
                {drawerError ? (
                  <div className="text-destructive bg-destructive/5 border-destructive/20 flex items-start gap-2 rounded-md border p-3 text-sm">
                    <AlertCircle className="mt-0.5 h-4 w-4 flex-shrink-0" />
                    <div>
                      <p className="font-medium">Couldn't add your servers</p>
                      <p className="text-muted-foreground mt-0.5">
                        {drawerError}
                      </p>
                    </div>
                  </div>
                ) : (
                  <div className="flex flex-col items-center justify-center gap-3 py-12">
                    <Loader2 className="text-muted-foreground h-8 w-8 animate-spin" />
                    <p className="text-muted-foreground text-sm">
                      Adding to your Default plugin…
                    </p>
                  </div>
                )}
              </div>

              {/* Step 2 — Distribute */}
              <div className="w-full shrink-0 space-y-4 overflow-y-auto px-6 pb-4">
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step 2
                </p>
                <h4 className="text-foreground text-base font-medium">
                  Distribute to your team
                </h4>
                <p className="text-muted-foreground text-sm leading-relaxed">
                  Your servers are bundled in the Default plugin and published
                  to your marketplace. Share these instructions so your
                  organization can install them.
                </p>
                {marketplaceCommand && (
                  <div className="space-y-2">
                    <p className="text-foreground text-sm font-medium">
                      Add the marketplace in Claude Code
                    </p>
                    <div className="bg-muted/50 flex items-center justify-between gap-2 rounded-md border p-3">
                      <code className="text-foreground truncate text-xs">
                        {marketplaceCommand}
                      </code>
                      <CopyButton text={marketplaceCommand} />
                    </div>
                  </div>
                )}
                {publishStatus?.repoOwner && publishStatus?.repoName && (
                  <div>
                    <p className="text-muted-foreground mb-2 text-sm">
                      For Cursor, Codex and other platforms:
                    </p>
                    <InstallInstructionsButton
                      repoOwner={publishStatus.repoOwner}
                      repoName={publishStatus.repoName}
                      marketplaceUrl={publishStatus.marketplaceUrl}
                    />
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Footer — only when there's an action to take */}
          {(drawerError || drawerStep === "done") && (
            <div className="border-border flex items-center justify-end border-t px-6 py-4">
              {drawerError ? (
                <Button variant="secondary" size="sm" onClick={resetDrawer}>
                  <Button.Text>Close</Button.Text>
                </Button>
              ) : (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => {
                    resetDrawer();
                    onComplete();
                  }}
                >
                  <Button.Text>Finish</Button.Text>
                </Button>
              )}
            </div>
          )}
        </SheetContent>
      </Sheet>
    </StepContainer>
  );
}
