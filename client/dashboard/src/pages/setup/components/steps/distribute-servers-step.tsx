import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  Boxes,
  Check,
  Loader2,
  Search,
  Server as ServerIcon,
} from "lucide-react";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { StepContainer } from "../step-container";
import { type PulseMCPServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import {
  filterToHttpRemotes,
  normalizeRemoteUrl,
} from "@/pages/catalog/remotes";
import { useRemoteMcpInstallWorkflow } from "@/pages/catalog/useRemoteMcpInstallWorkflow";
import { useQueryClient } from "@tanstack/react-query";
import { useMcpServers } from "@gram/client/react-query/mcpServers";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers";
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
import { cn } from "@/lib/utils";

/** Display name of the shared plugin bundle catalog servers are added to. */
const DEFAULT_PLUGIN_NAME = "Default";
/** Server always provisions the Default plugin with this exact slug. */
const DEFAULT_PLUGIN_SLUG = "default";
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
  // Slug of the Default plugin found/created during this run — captured so
  // the install-instructions step below can address it precisely.
  const [distributedPluginSlug, setDistributedPluginSlug] = useState<
    string | null
  >(null);

  // The catalog is small and returned in a single response, so we fetch the
  // whole list once and search/filter it client-side (no cursor pagination).
  const { data, isLoading } = useListMCPCatalog();
  const { data: publishStatus } = usePublishStatus();

  // Default-plugin membership: map its mcp_server-backed entries through their
  // remote MCP server URLs back to catalog remotes so we can flag servers that
  // are already distributed.
  const { data: pluginsData } = usePlugins();
  const defaultPlugin = pluginsData?.plugins.find((p) => p.isDefault);
  const { data: defaultPluginFull } = usePlugin(
    { id: defaultPlugin?.id ?? "" },
    undefined,
    { enabled: !!defaultPlugin?.id },
  );
  const { data: mcpServersData } = useMcpServers();
  const { data: remoteServersData } = useRemoteMcpServers();

  const distributedUrls = useMemo(() => {
    const mcpServerById = new Map(
      (mcpServersData?.mcpServers ?? []).map((s) => [s.id, s]),
    );
    const remoteUrlById = new Map(
      (remoteServersData?.remoteMcpServers ?? []).map((s) => [
        s.id,
        normalizeRemoteUrl(s.url),
      ]),
    );
    const urls = new Set<string>();
    for (const server of defaultPluginFull?.servers ?? []) {
      if (!server.mcpServerId) continue;
      const remoteMcpServerId = mcpServerById.get(
        server.mcpServerId,
      )?.remoteMcpServerId;
      const url = remoteMcpServerId
        ? remoteUrlById.get(remoteMcpServerId)
        : undefined;
      if (url) urls.add(url);
    }
    return urls;
  }, [
    defaultPluginFull?.servers,
    mcpServersData?.mcpServers,
    remoteServersData?.remoteMcpServers,
  ]);

  const isDistributed = useCallback(
    (server: PulseMCPServer) =>
      (server.remotes ?? []).some((remote) =>
        distributedUrls.has(normalizeRemoteUrl(remote.url)),
      ),
    [distributedUrls],
  );

  const servers = useMemo(
    () => (data?.servers as PulseMCPServer[]) ?? [],
    [data],
  );
  // Onboarding only surfaces servers Speakeasy can fully auto-configure: those whose
  // OAuth authorization server advertises a dynamic client registration
  // endpoint (DCR), reported live by the catalog's `supports_dcr` flag.
  // Everything else would dead-end on "OAuth setup required" or a missing API
  // key, so we steer the user to the full catalog for those (see note below the
  // list).
  const autoConfigurableServers = useMemo(
    () => servers.filter((s) => s.supportsDcr),
    [servers],
  );

  // Search runs client-side over the full auto-configurable list. It only
  // narrows what's shown — selection (selectedServerObjects) still reads the
  // unfiltered autoConfigurableServers so a selected server survives a search
  // that no longer matches it.
  const matchedServers = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return autoConfigurableServers;
    return autoConfigurableServers.filter((s) =>
      [s.registrySpecifier, s.title ?? "", s.description ?? ""].some((field) =>
        field.toLowerCase().includes(needle),
      ),
    );
  }, [autoConfigurableServers, query]);

  const visibleServers = showAll
    ? matchedServers
    : matchedServers.slice(0, INITIAL_VISIBLE);

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
        (s) => selected.has(serverKey(s)) && !isDistributed(s),
      ),
    [autoConfigurableServers, selected, isDistributed],
  );

  // --- Install workflow (driven headlessly, no dialog) ----------------------
  // Every selected server is installed as a Remote MCP server. There is no UI
  // to answer the selectRemotes phase, so multi-remote servers install every
  // endpoint.
  const workflow = useRemoteMcpInstallWorkflow({
    servers: serversToDeploy,
    autoSelectRemotes: true,
  });
  const startedRef = useRef(false);
  const finishedRef = useRef(false);

  /**
   * Bundle the freshly created MCP servers into the shared "Default" plugin
   * and republish the marketplace so the org's users receive them.
   */
  const finishDistribution = async () => {
    try {
      // Only reachable in the complete phase (the driving effect guards this);
      // the check also narrows the workflow union so statuses is typed.
      if (workflow.phase !== "complete") return;
      const toAdd = workflow.statuses.flatMap((s) =>
        s.status === "completed" && s.mcpServerId
          ? [{ mcpServerId: s.mcpServerId, displayName: s.name }]
          : [],
      );
      if (toAdd.length === 0) {
        setDrawerError(
          workflow.statuses.find((s) => s.error)?.error ??
            "No servers were installed. Try again.",
        );
        return;
      }

      const { plugins } = await client.plugins.listPlugins();
      const plugin =
        plugins.find((p) => p.isDefault) ??
        (await client.plugins.createPlugin({
          createPluginForm: { name: DEFAULT_PLUGIN_NAME },
        }));

      setDistributedPluginSlug(plugin.slug);

      const full = await client.plugins.getPlugin({ id: plugin.id });
      const alreadyBundled = new Set(
        (full.servers ?? [])
          .map((s) => s.mcpServerId)
          .filter((id): id is string => !!id),
      );

      for (const server of toAdd) {
        if (alreadyBundled.has(server.mcpServerId)) continue;
        await client.plugins.addPluginServer({
          addPluginServerForm: {
            pluginId: plugin.id,
            mcpServerId: server.mcpServerId,
            displayName: server.displayName,
            policy: "required",
          },
        });
      }

      if (publishStatus?.connected) {
        await client.plugins.publishPlugins({
          publishPluginsRequestBody: { githubUsernames: [] },
        });
      }

      // Refresh plugin state so the just-distributed servers flip to "Added"
      // (disabled) in the grid, and drop them from the selection.
      await Promise.all([
        invalidateAllPlugins(queryClient),
        invalidateAllPlugin(queryClient),
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

  // Drive the workflow: auto-start the install, then bundle + publish once all
  // servers are created. Guarded by refs so each fires exactly once per run.
  useEffect(() => {
    if (!drawerOpen || drawerError) return;

    if (workflow.phase === "configure") {
      if (!startedRef.current && workflow.canInstall) {
        startedRef.current = true;
        void workflow.startInstall();
      }
      return;
    }

    if (workflow.phase === "complete" && !finishedRef.current) {
      finishedRef.current = true;
      void finishDistribution();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- guarded by refs; re-run on workflow identity to observe phase + status changes
  }, [workflow, drawerOpen, drawerError]);

  const handleDistribute = () => {
    if (selectedServerObjects.length === 0) return;
    startedRef.current = false;
    finishedRef.current = false;
    setDrawerError(null);
    setDrawerStep("adding");
    // Remote MCP servers are created from streamable-http endpoints only. If
    // nothing survives the filter the workflow would never start, so surface
    // the dead end instead of an endless spinner.
    const deployable = selectedServerObjects.map(filterToHttpRemotes);
    if (!deployable.some((server) => (server.remotes ?? []).length > 0)) {
      setServersToDeploy([]);
      setDrawerError(
        "None of the selected servers expose a compatible remote endpoint.",
      );
      setDrawerOpen(true);
      return;
    }
    setServersToDeploy(deployable);
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
  // Gate the action on actually-deployable servers, not the raw selection:
  // already-distributed picks are filtered out of selectedServerObjects, so
  // counting selected.size could enable a Continue that deploys nothing.
  const deployableCount = selectedServerObjects.length;
  const continueLabel =
    deployableCount > 0
      ? `Distribute ${deployableCount} server${deployableCount === 1 ? "" : "s"}`
      : "Distribute servers";
  // Once at least one server has been distributed, the secondary action is no
  // longer a skip — the user has done the step, so let them move on.
  const skipLabel = distributedUrls.size > 0 ? "Continue" : "Skip for now";

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
      skipLabel={skipLabel}
      continueLabel={continueLabel}
      isLoading={drawerOpen && isAdding}
      canContinue={deployableCount > 0}
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
              onKeyDown={(e) => {
                // Escape clears the query first; only let it bubble to parent
                // Escape handlers (e.g. closing the drawer) once empty.
                if (e.key === "Escape" && query) {
                  e.stopPropagation();
                  setQuery("");
                  setShowAll(false);
                }
              }}
              placeholder="Search MCP servers"
              className="pl-9"
            />
          </div>

          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
            </div>
          ) : matchedServers.length === 0 ? (
            <p className="text-muted-foreground mt-3 text-sm">
              {query
                ? `No auto-configurable servers match "${query}".`
                : "No auto-configurable servers available."}
            </p>
          ) : (
            <div className="mt-3 grid grid-cols-2 gap-3">
              {visibleServers.map((server) => {
                const key = serverKey(server);
                const distributed = isDistributed(server);
                const isSelected = selected.has(key);
                return (
                  <button
                    key={key}
                    type="button"
                    onClick={() => {
                      if (distributed) {
                        showInstructions();
                      } else {
                        toggle(key);
                      }
                    }}
                    className={cn(
                      "flex min-h-[118px] items-start gap-3 rounded-lg border p-4 text-left transition-all",
                      isSelected && !distributed
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
                    {distributed ? (
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

          {!showAll && matchedServers.length > INITIAL_VISIBLE && (
            <button
              type="button"
              onClick={() => setShowAll(true)}
              className="text-muted-foreground hover:text-foreground mt-2 flex w-full items-center justify-center gap-1.5 py-2 text-sm transition-colors"
            >
              Show more servers
            </button>
          )}

          {!isLoading && (
            <p className="text-muted-foreground mt-4 text-xs leading-relaxed">
              Only servers that support OAuth dynamic client registration (DCR)
              are shown here — Speakeasy can configure these automatically. More
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
                      Install for yourself in Claude Code
                    </p>
                    <p className="text-muted-foreground text-xs">
                      Registers the marketplace for your own account.
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
                  <div className="space-y-2">
                    <p className="text-foreground text-sm font-medium">
                      Roll out to your whole organization
                    </p>
                    <p className="text-muted-foreground text-xs leading-relaxed">
                      Push the marketplace to every developer through Claude
                      Code Managed Settings — no per-user install command
                      required. The full guide also covers Claude Cowork,
                      Cursor, and Codex.
                    </p>
                    <InstallInstructionsButton
                      repoOwner={publishStatus.repoOwner}
                      repoName={publishStatus.repoName}
                      marketplaceUrl={publishStatus.marketplaceUrl}
                      candidatePlugins={[
                        {
                          name: DEFAULT_PLUGIN_NAME,
                          slug: distributedPluginSlug ?? DEFAULT_PLUGIN_SLUG,
                        },
                      ]}
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
