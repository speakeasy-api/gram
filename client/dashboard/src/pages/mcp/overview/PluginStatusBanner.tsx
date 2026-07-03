import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import { Plugin } from "@gram/client/models/components";
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
import { AlertTriangle, ChevronDown, CircleCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { InstallInstructionsButton } from "@/pages/plugins/InstallInstructionsDialog";

// A little fanned stack of client badges — purely decorative, gestures at
// "this is what publishing unlocks" without claiming these are the only
// supported clients.
function ClientIconFan(): React.JSX.Element {
  return (
    <div className="relative hidden h-32 shrink-0 sm:block" aria-hidden="true">
      <div className="absolute inset-6 rounded-full bg-black/15 blur-2xl" />
      <img
        src="/icons/decorative/plugin-clients.webp"
        alt=""
        className="relative h-full w-full object-contain drop-shadow-xl"
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
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element | null {
  const queryClient = useQueryClient();
  const { data } = usePlugins();
  const { data: publishStatus } = usePublishStatus();
  const [selectedPluginIds, setSelectedPluginIds] = useState<string[]>([]);
  const [isPickerOpen, setIsPickerOpen] = useState(false);

  const addServerMutation = useAddPluginServerMutation();
  const removeServerMutation = useRemovePluginServerMutation();
  const publishMutation = usePublishPluginsMutation();

  // Sort+join so the effect only re-fires when membership actually changes,
  // not on every unrelated `data` refetch.
  const memberKey = (data?.plugins ?? [])
    .filter((plugin) =>
      plugin.servers?.some((server) => server.toolsetId === toolset.id),
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
    plugin.servers?.some((server) => server.toolsetId === toolset.id),
  );
  const isPublished = memberPlugins.length > 0;
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
                toolsetId: toolset.id,
                displayName: toolset.name,
                policy: "required",
              },
            },
          }),
        ),
        ...toRemove.map((plugin) => {
          const serverId = plugin.servers?.find(
            (server) => server.toolsetId === toolset.id,
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
    <div className="border-muted relative overflow-hidden rounded-xl border shadow-sm">
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-br from-orange-50 via-slate-50 to-orange-100 transition-opacity duration-700 ease-in-out",
          isPublished ? "opacity-0" : "opacity-100",
        )}
      />
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-br from-emerald-50/10 via-slate-50 to-emerald-100/70 transition-opacity duration-700 ease-in-out",
          isPublished ? "opacity-100" : "opacity-0",
        )}
      />
      <div className="relative flex flex-col">
        <div className="flex items-center justify-between gap-8 p-6">
          <div className="flex max-w-md flex-col gap-3">
            <div className="flex items-center gap-2">
              {isPublished ? (
                <CircleCheck className="text-emerald-500 h-4 w-4 shrink-0" />
              ) : (
                <AlertTriangle className="text-warning-foreground h-4 w-4 shrink-0" />
              )}
              <Type
                className={cn(
                  "text-warning-foreground text-base font-semibold",
                  isPublished && "text-emerald-500",
                )}
              >
                {isPublished
                  ? `Published to ${memberPlugins.length} plugin${
                      memberPlugins.length > 1 ? "s" : ""
                    }`
                  : "Not published"}
              </Type>
            </div>
            <Type variant="small" className="text-muted-foreground/90">
              Plugins are the preferred way to distribute MCP servers to your
              organization's users.
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
                  {isSaving ? "Saving…" : isPublished ? "Update" : "Publish"}
                </Button>
              </div>
            )}
          </div>
          <ClientIconFan />
        </div>

        {isPublished && publishStatus?.repoOwner && publishStatus.repoName && (
          <>
            <div className="border-border border-t" />
            <div className="flex items-center justify-between gap-4 p-6">
              <div className="flex flex-col gap-1">
                <Type className="text-sm font-semibold">
                  Install the plugin
                </Type>
                <Type variant="small" className="text-muted-foreground/90">
                  Your team installs the plugin in their AI client to start
                  using this server.
                </Type>
              </div>
              <InstallInstructionsButton
                repoOwner={publishStatus.repoOwner}
                repoName={publishStatus.repoName}
                marketplaceUrl={publishStatus.marketplaceUrl}
                pluginName={
                  memberPlugins.length === 1
                    ? memberPlugins[0]?.name
                    : undefined
                }
                pluginSlug={
                  memberPlugins.length === 1
                    ? memberPlugins[0]?.slug
                    : undefined
                }
              />
            </div>
          </>
        )}
      </div>
    </div>
  );
}
