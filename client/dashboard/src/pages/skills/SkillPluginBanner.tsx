import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { ClientIconFan } from "@/pages/mcp/overview/PluginStatusBanner";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Button, cn } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ChevronDown, CircleCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

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
    return "Updated plugin distributions";
  }
  if (removedCount > 0) {
    return removedCount > 1
      ? `Removed from ${removedCount} plugins`
      : "Removed from plugin";
  }
  return addedCount > 1
    ? `Distributed to ${addedCount} plugins`
    : "Distributed to plugin";
}

function saveButtonLabel(isSaving: boolean, isDistributed: boolean): string {
  if (isSaving) return "Saving";
  if (isDistributed) return "Update";
  return "Distribute";
}

/**
 * The distribution status banner at the top of the skill detail page:
 * summarizes which plugins carry this skill and lets writers stage and save
 * plugin membership changes in one place.
 */
export function SkillPluginBanner({
  skillId,
}: {
  skillId: string;
}): JSX.Element | null {
  const project = useProject();
  const queryClient = useQueryClient();
  const { data: pluginsData } = usePlugins(undefined, undefined, {
    throwOnError: false,
  });
  const distributionsQuery = useSkillDistributionsInfinite(
    { skillId, limit: 50 },
    undefined,
    { throwOnError: false },
  );
  // The picker seeds and diffs against the complete membership; a partial
  // page would show later-page memberships as unchecked and no-op re-saves.
  useDrainInfiniteQuery(distributionsQuery);
  const isMembershipLoaded =
    !!distributionsQuery.data && !distributionsQuery.hasNextPage;
  const distributions = useMemo(
    () =>
      distributionsQuery.data?.pages.flatMap(
        (page) => page.result.distributions,
      ) ?? [],
    [distributionsQuery.data?.pages],
  );

  const distribute = useDistributeSkillMutation();
  const undistribute = useUndistributeSkillMutation();
  const [selectedPluginIds, setSelectedPluginIds] = useState<string[]>([]);
  const [isPickerOpen, setIsPickerOpen] = useState(false);

  // Sort+join so the effect only re-fires when membership actually changes,
  // not on every unrelated refetch.
  const memberKey = distributions
    .map((distribution) => distribution.pluginId)
    .sort()
    .join(",");

  // Seed the picker's staged selection from committed membership on load and
  // whenever it changes underneath us (e.g. after a successful save). Not
  // tied to isPickerOpen — closing the popover must not discard an
  // in-progress, unsaved selection.
  useEffect(() => {
    setSelectedPluginIds(memberKey ? memberKey.split(",") : []);
  }, [memberKey]);

  // Don't flash the "not distributed" warning while either side still loads,
  // and don't derive picker state from a partially drained membership.
  if (!pluginsData || (!isMembershipLoaded && !distributionsQuery.error)) {
    return null;
  }

  const plugins = pluginsData.plugins;
  const isDistributed = distributions.length > 0;
  const memberIdSet = new Set(
    distributions.map((distribution) => distribution.pluginId),
  );
  const selectedIdSet = new Set(selectedPluginIds);
  const hasChanges =
    selectedPluginIds.some((id) => !memberIdSet.has(id)) ||
    distributions.some(
      (distribution) => !selectedIdSet.has(distribution.pluginId),
    );
  const isSaving = distribute.isPending || undistribute.isPending;

  const togglePlugin = (pluginId: string) => {
    setSelectedPluginIds((prev) =>
      prev.includes(pluginId)
        ? prev.filter((id) => id !== pluginId)
        : [...prev, pluginId],
    );
  };

  const handleSave = async () => {
    const toAdd = selectedPluginIds.filter((id) => !memberIdSet.has(id));
    const toRemove = distributions.filter(
      (distribution) => !selectedIdSet.has(distribution.pluginId),
    );
    if (toAdd.length === 0 && toRemove.length === 0) return;
    // allSettled + unconditional invalidation: some mutations in the batch
    // may succeed even when others fail, and the cache must reflect what the
    // server actually committed.
    const results = await Promise.allSettled([
      ...toAdd.map((pluginId) =>
        distribute.mutateAsync({
          request: {
            distributeSkillRequestBody: { id: skillId, pluginId },
          },
        }),
      ),
      ...toRemove.map((distribution) =>
        undistribute.mutateAsync({
          request: {
            undistributeSkillRequestBody: {
              id: skillId,
              pluginId: distribution.pluginId,
            },
          },
        }),
      ),
    ]);
    await invalidateAllSkillDistributions(queryClient);
    const firstFailure = results.find((result) => result.status === "rejected");
    if (firstFailure) {
      toast.error(
        firstFailure.reason instanceof Error
          ? firstFailure.reason.message
          : "Failed to update plugin distributions",
      );
      return;
    }
    toast.success(describeSaveResult(toAdd.length, toRemove.length));
  };

  return (
    <div className="border-border/70 relative overflow-hidden rounded-xl border shadow-sm">
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-tr from-slate-50 via-slate-50 to-orange-100 transition-all duration-700 ease-in-out dark:from-slate-950 dark:via-neutral-800 dark:to-amber-900/60",
          isDistributed ? "opacity-0" : "opacity-100",
        )}
      />
      <div
        aria-hidden="true"
        className={cn(
          "absolute inset-0 bg-gradient-to-br from-slate-50/10 via-slate-50 to-emerald-100/50 transition-colors transition-opacity duration-700 ease-in-out dark:from-slate-950/60 dark:via-neutral-800 dark:to-emerald-900/30",
          isDistributed ? "opacity-100" : "opacity-0",
        )}
      />
      <div className="relative flex items-center justify-between gap-8 p-6">
        <div className="flex max-w-md flex-col gap-3">
          <div className="flex items-center gap-2">
            {isDistributed ? (
              <CircleCheck className="text-emerald-500 h-4 w-4 shrink-0" />
            ) : (
              <AlertTriangle className="text-warning-foreground h-4 w-4 shrink-0" />
            )}
            <Type
              className={cn(
                "text-warning-foreground text-base font-semibold",
                isDistributed && "text-emerald-500",
              )}
            >
              {isDistributed
                ? `Distributed to ${distributions.length} plugin${
                    distributions.length > 1 ? "s" : ""
                  }`
                : "Not distributed to any plugin"}
            </Type>
          </div>
          <Type variant="small" className="text-muted-foreground/90">
            Plugins are the preferred way to distribute skills to your
            organization's users. Skills distributed to a plugin ship inside the
            plugin package and reach everyone who installs it.
          </Type>
          {plugins.length > 0 && (
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
            >
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
                          {/* Disabled while saving: the post-save reseed
                              would silently discard mid-flight toggles. */}
                          <Checkbox
                            checked={selectedPluginIds.includes(plugin.id)}
                            disabled={isSaving}
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
                  {isSaving && (
                    <Button.LeftIcon>
                      <Spinner />
                    </Button.LeftIcon>
                  )}
                  <Button.Text>
                    {saveButtonLabel(isSaving, isDistributed)}
                  </Button.Text>
                </Button>
              </div>
            </RequireScope>
          )}
        </div>
        <ClientIconFan />
      </div>
    </div>
  );
}
