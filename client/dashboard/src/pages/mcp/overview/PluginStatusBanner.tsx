import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Plugin } from "@gram/client/models/components/plugin.js";
import { PluginServer } from "@gram/client/models/components/pluginserver.js";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import {
  invalidateAllPlugins,
  usePlugins,
} from "@gram/client/react-query/plugins";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { Button, cn } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ChevronDown, CircleCheck, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { InstallInstructionsDialog } from "@/pages/plugins/InstallInstructionsDialog";
import { HostedServerRef } from "./MCPOverviewTab";
import { Spinner } from "@/components/ui/spinner";

function serverMatchesRef(server: PluginServer, ref: HostedServerRef): boolean {
  return ref.kind === "toolset"
    ? server.toolsetId === ref.id
    : server.mcpServerId === ref.id;
}

// A little fanned stack of client badges — purely decorative, gestures at
// "this is what publishing unlocks" without claiming these are the only
// supported clients.
function ClientIconFan(): React.JSX.Element {
  return (
    <div className="relative hidden h-36 shrink-0 sm:block" aria-hidden="true">
      <div className="absolute inset-6 rounded-full bg-black/15 blur-2xl" />
      <img
        src="/icons/decorative/plugin-clients.webp"
        alt=""
        className="relative h-full w-full mr-10 object-contain drop-shadow-xl"
      />
    </div>
  );
}

function summarizePluginSelection(
  selectedIds: string[],
  plugins: Plugin[],
): string {
  if (selectedIds.length === 0) return "No plugins selected";
  const firstName =
    plugins.find((plugin) => plugin.id === selectedIds[0])?.name ??
    selectedIds[0];
  if (selectedIds.length === 1) return firstName ?? "1 plugin";
  return `${firstName} & ${selectedIds.length - 1} more`;
}

function describeSaveResult(addedCount: number, removedCount: number): string {
  if (addedCount > 0 && removedCount > 0) {
    return "Updated plugin membership and published to GitHub";
  }
  if (removedCount > 0) {
    return removedCount > 1
      ? `Removed from ${removedCount} plugins and published to GitHub`
      : "Removed from plugin and published to GitHub";
  }
  return addedCount > 1
    ? `Added to ${addedCount} plugins and published to GitHub`
    : "Added to plugin and published to GitHub";
}

export function PluginStatusBanner({
  server,
}: {
  server: HostedServerRef;
}): React.JSX.Element | null {
  const routes = useRoutes();
  const queryClient = useQueryClient();
  // throwOnError: false — this query calls EnsureDefaultPlugin server-side,
  // which self-heals most conflicts but can still fail; a banner on the MCP
  // detail page shouldn't crash the whole page via the error boundary when
  // it does. The existing `if (!data) return null` below already degrades
  // gracefully on a failed fetch.
  const { data } = usePlugins(undefined, undefined, { throwOnError: false });
  const [isInstallDialogOpen, setIsInstallDialogOpen] = useState(false);
  // Polled so the banner picks up the Temporal generator-rollout schedule's
  // auto-sync without a manual refresh.
  const { data: publishStatus } = usePublishStatus(undefined, undefined, {
    refetchInterval: 5_000,
  });
  const [selectedPluginIds, setSelectedPluginIds] = useState<string[]>([]);
  const [isPickerOpen, setIsPickerOpen] = useState(false);

  const addServerMutation = useAddPluginServerMutation();
  const removeServerMutation = useRemovePluginServerMutation();
  const publishMutation = usePublishPluginsMutation();

  // Sort+join so the effect only re-fires when membership actually changes,
  // not on every unrelated `data` refetch.
  const memberKey = (data?.plugins ?? [])
    .filter((plugin) =>
      plugin.servers?.some((s) => serverMatchesRef(s, server)),
    )
    .map((plugin) => plugin.id)
    .sort()
    .join(",");

  // Seed the picker's staged selection from committed membership on load and
  // whenever it changes underneath us (e.g. after a successful save). Not
  // tied to isPickerOpen — losing focus/closing the popover must not discard
  // an in-progress, unsaved selection.
  useEffect(() => {
    setSelectedPluginIds(memberKey ? memberKey.split(",") : []);
  }, [memberKey]);

  // Don't flash the "not published" warning while plugins are still loading.
  if (!data) return null;

  const plugins = data.plugins;
  const memberPlugins = plugins.filter((plugin) =>
    plugin.servers?.some((s) => serverMatchesRef(s, server)),
  );
  const isPublished = memberPlugins.length > 0;
  // A repo existing isn't enough — a marketplace with no active collaborator
  // isn't discoverable in Claude/Codex/etc, so it isn't truly set up yet.
  // Mirrors the connected/hasCollaborators gate on the Plugins page.
  const marketplaceReady = !!(
    publishStatus?.repoOwner &&
    publishStatus.repoName &&
    publishStatus.hasCollaborators !== false
  );
  // "Published" means a teammate can actually install it — server
  // membership in a plugin alone isn't enough if the marketplace repo
  // itself was never set up on GitHub.
  const isTrulyPublished = isPublished && marketplaceReady;
  const memberIdSet = new Set(memberPlugins.map((plugin) => plugin.id));
  const selectedIdSet = new Set(selectedPluginIds);
  const hasChanges =
    selectedPluginIds.some((id) => !memberIdSet.has(id)) ||
    memberPlugins.some((plugin) => !selectedIdSet.has(plugin.id));
  const isSaving =
    addServerMutation.isPending ||
    removeServerMutation.isPending ||
    publishMutation.isPending;

  const togglePlugin = (pluginId: string) => {
    setSelectedPluginIds((prev) =>
      prev.includes(pluginId)
        ? prev.filter((id) => id !== pluginId)
        : [...prev, pluginId],
    );
  };

  const handleSave = async () => {
    const toAdd = selectedPluginIds.filter((id) => !memberIdSet.has(id));
    const toRemove = memberPlugins.filter(
      (plugin) => !selectedIdSet.has(plugin.id),
    );
    if (toAdd.length === 0 && toRemove.length === 0) return;
    try {
      await Promise.all([
        ...toAdd.map((pluginId) =>
          addServerMutation.mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: {
              addPluginServerForm: {
                pluginId,
                ...(server.kind === "toolset"
                  ? { toolsetId: server.id }
                  : { mcpServerId: server.id }),
                policy: "required",
              },
            },
          }),
        ),
        ...toRemove.map((plugin) => {
          const serverId = plugin.servers?.find((s) =>
            serverMatchesRef(s, server),
          )?.id;
          if (!serverId) return Promise.resolve();
          return removeServerMutation.mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: { id: serverId, pluginId: plugin.id },
          });
        }),
      ]);
      const publishResult = await publishMutation.mutateAsync({
        security: { sessionHeaderGramSession: "" },
        request: {
          publishPluginsRequestBody: { githubUsernames: [] },
        },
      });
      await Promise.all([
        invalidateAllPlugins(queryClient),
        invalidateAllPublishStatus(queryClient),
      ]);
      toast.success(describeSaveResult(toAdd.length, toRemove.length), {
        description: (
          <a
            href={publishResult.repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="underline underline-offset-2"
          >
            {publishResult.repoUrl}
          </a>
        ),
      });
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update plugin membership",
      );
    }
  };

  return (
    <div className="border border-border/70 relative overflow-hidden rounded-xl shadow-sm">
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-tr from-slate-50 via-slate-50 to-orange-100 transition-all duration-700 ease-in-out dark:from-slate-950 dark:via-neutral-800 dark:to-amber-900/60",
          isTrulyPublished ? "opacity-0" : "opacity-100",
        )}
      />
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-br from-slate-50/10 via-slate-50 to-emerald-100/50 transition-colors transition-opacity duration-700 ease-in-out dark:from-slate-950/60 dark:via-neutral-800 dark:to-emerald-900/30",
          isTrulyPublished ? "opacity-100" : "opacity-0",
        )}
      />
      <div className="relative flex flex-col">
        <div className="flex items-center justify-between gap-8 p-6">
          <div className="flex max-w-md flex-col gap-3">
            <div className="flex items-center gap-2">
              {isTrulyPublished ? (
                <CircleCheck className="text-emerald-500 h-4 w-4 shrink-0" />
              ) : (
                <AlertTriangle className="text-warning-foreground h-4 w-4 shrink-0" />
              )}
              <Type
                className={cn(
                  "text-warning-foreground text-base font-semibold",
                  isTrulyPublished && "text-emerald-500",
                )}
              >
                {isTrulyPublished
                  ? `Published to ${memberPlugins.length} plugin${
                      memberPlugins.length > 1 ? "s" : ""
                    }`
                  : isPublished
                    ? "Marketplace needs setup"
                    : "Not published to any plugin"}
              </Type>
            </div>
            <Type variant="small" className="text-muted-foreground/90">
              Plugins are the preferred way to distribute MCP servers to your
              organization's users. Plugins are installed via marketplaces which
              are GitHub repositories that Speakeasy hosts on your behalf.
            </Type>
            {plugins.length > 0 && (
              <div className="flex items-center gap-2">
                <Popover open={isPickerOpen} onOpenChange={setIsPickerOpen}>
                  <PopoverTrigger asChild>
                    <button
                      type="button"
                      className="border-input bg-background hover:bg-muted flex h-8 w-56 items-center justify-between gap-2 rounded-md border px-3 text-sm shadow-xs outline-none"
                    >
                      <span
                        className={cn(
                          "truncate",
                          selectedPluginIds.length === 0 &&
                            "text-muted-foreground",
                        )}
                      >
                        {summarizePluginSelection(selectedPluginIds, plugins)}
                      </span>
                      <ChevronDown className="text-muted-foreground size-4 shrink-0" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent align="start" className="w-80 p-1">
                    <div className="flex max-h-56 flex-col gap-0.5 overflow-y-auto">
                      {plugins.map((plugin) => (
                        <label
                          key={plugin.id}
                          className="hover:bg-accent flex cursor-pointer items-start gap-2 rounded-sm px-2 py-1.5 text-sm"
                        >
                          <Checkbox
                            checked={selectedPluginIds.includes(plugin.id)}
                            onCheckedChange={() => togglePlugin(plugin.id)}
                            className="mt-0.5"
                          />
                          <div className="flex flex-col">
                            <span>{plugin.name}</span>
                            {plugin.description && (
                              <span className="text-muted-foreground text-xs">
                                {plugin.description}
                              </span>
                            )}
                          </div>
                        </label>
                      ))}
                    </div>
                  </PopoverContent>
                </Popover>
                <Button
                  size="sm"
                  disabled={!hasChanges || isSaving}
                  onClick={() => void handleSave()}
                >
                  {isSaving ? (
                    <>
                      <Spinner /> Publishing
                    </>
                  ) : isPublished ? (
                    "Update"
                  ) : (
                    "Publish"
                  )}
                </Button>
              </div>
            )}
          </div>
          <ClientIconFan />
        </div>

        {isPublished && (
          <>
            <div className="border-border border-t" />
            <div className="flex items-center justify-between gap-4 p-6">
              <div className="flex flex-col gap-1">
                <Type className="text-sm font-semibold">
                  Install the plugin
                </Type>
                <Type variant="small" className="text-muted-foreground/90">
                  {marketplaceReady
                    ? "Your team installs the plugin in their AI client to start using this server."
                    : "Your project marketplace isn't fully set up yet, which means you won't be able to install this MCP via the plugin."}
                </Type>
              </div>
              {marketplaceReady &&
              publishStatus?.repoOwner &&
              publishStatus.repoName ? (
                <div className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={() => setIsInstallDialogOpen(true)}
                  >
                    <Button.LeftIcon>
                      <Plus className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Install</Button.Text>
                  </Button>
                  <routes.plugins.Link>
                    <Button size="sm" variant="secondary">
                      <Button.Text>Go to plugins</Button.Text>
                    </Button>
                  </routes.plugins.Link>
                  <InstallInstructionsDialog
                    open={isInstallDialogOpen}
                    onOpenChange={setIsInstallDialogOpen}
                    repoOwner={publishStatus.repoOwner}
                    repoName={publishStatus.repoName}
                    marketplaceUrl={publishStatus.marketplaceUrl}
                    candidatePlugins={memberPlugins.map((plugin) => ({
                      name: plugin.name,
                      slug: plugin.slug,
                      description: plugin.description,
                    }))}
                  />
                </div>
              ) : (
                <routes.plugins.Link>
                  <Button size="sm" variant="primary">
                    Set up marketplace
                  </Button>
                </routes.plugins.Link>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
