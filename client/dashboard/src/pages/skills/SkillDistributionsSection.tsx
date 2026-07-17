import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { SkillDistribution } from "@gram/client/models/components/skilldistribution.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import { usePlugins } from "@gram/client/react-query/plugins.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * Lists the plugins an individual skill is distributed to and lets writers
 * add or revoke plugin distributions from the skill sheet.
 */
export function SkillDistributionsSection({
  skillId,
}: {
  skillId: string;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const distributionsQuery = useSkillDistributionsInfinite(
    { skillId, limit: 50 },
    undefined,
    { throwOnError: false },
  );
  const pluginsQuery = usePlugins(undefined, undefined, {
    throwOnError: false,
  });
  const distribute = useDistributeSkillMutation();
  const undistribute = useUndistributeSkillMutation();
  const [selectedPluginId, setSelectedPluginId] = useState("");

  const distributions = useMemo(
    () =>
      distributionsQuery.data?.pages.flatMap(
        (page) => page.result.distributions,
      ) ?? [],
    [distributionsQuery.data?.pages],
  );
  const availablePlugins = useMemo(() => {
    const distributedPluginIds = new Set(distributions.map((d) => d.pluginId));
    return (pluginsQuery.data?.plugins ?? []).filter(
      (plugin) => !distributedPluginIds.has(plugin.id),
    );
  }, [distributions, pluginsQuery.data?.plugins]);

  const handleDistribute = async (): Promise<void> => {
    if (!selectedPluginId) return;
    try {
      await distribute.mutateAsync({
        request: {
          distributeSkillRequestBody: {
            id: skillId,
            pluginId: selectedPluginId,
          },
        },
      });
      await invalidateAllSkillDistributions(queryClient);
      setSelectedPluginId("");
      toast.success("Skill distributed to plugin");
    } catch (_err) {
      toast.error("Unable to distribute skill");
    }
  };

  const handleUndistribute = async (
    distribution: SkillDistribution,
  ): Promise<void> => {
    try {
      await undistribute.mutateAsync({
        request: {
          undistributeSkillRequestBody: {
            id: skillId,
            pluginId: distribution.pluginId,
          },
        },
      });
      await invalidateAllSkillDistributions(queryClient);
      toast.success(`Removed from ${distribution.pluginName}`);
    } catch (_err) {
      toast.error("Unable to remove distribution");
    }
  };

  if (distributionsQuery.isPending && !distributionsQuery.data) {
    return <Skeleton className="h-24 w-full" />;
  }
  if (distributionsQuery.error && !distributionsQuery.data) {
    return (
      <ErrorAlert title="Unable to load distributions" error="Try again." />
    );
  }

  return (
    <div className="space-y-3">
      <Type small muted>
        Distributed skills ship inside the plugin package and reach everyone who
        installs the plugin.
      </Type>

      {distributions.length === 0 ? (
        <div className="border-border rounded-lg border border-dashed p-4">
          <Type small muted>
            Not distributed to any plugins yet.
          </Type>
        </div>
      ) : (
        <ul className="border-border divide-y rounded-lg border">
          {distributions.map((distribution) => (
            <li
              key={distribution.id}
              className="flex items-center justify-between gap-3 px-4 py-3"
            >
              <div className="min-w-0">
                <Link
                  to={routes.plugins.detail.href(distribution.pluginId)}
                  className="text-sm font-medium hover:underline"
                >
                  {distribution.pluginName}
                </Link>
                <Type
                  small
                  muted
                  className="block text-xs"
                  title={dateTimeFormatters.full.format(distribution.createdAt)}
                >
                  Distributed <HumanizeDateTime date={distribution.createdAt} />
                </Type>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <VersionTrackingBadge distribution={distribution} />
                <RequireScope
                  scope="skill:write"
                  resourceId={project.id}
                  level="component"
                >
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    tooltip="Remove from plugin"
                    aria-label={`Remove from ${distribution.pluginName}`}
                    className="hover:text-destructive"
                    disabled={undistribute.isPending}
                    onClick={() => void handleUndistribute(distribution)}
                  >
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </RequireScope>
              </div>
            </li>
          ))}
        </ul>
      )}

      {distributionsQuery.hasNextPage && (
        <Button
          variant="outline"
          size="sm"
          disabled={distributionsQuery.isFetchingNextPage}
          onClick={() => void distributionsQuery.fetchNextPage()}
        >
          {distributionsQuery.isFetchingNextPage
            ? "Loading..."
            : "Load more distributions"}
        </Button>
      )}

      <RequireScope
        scope="skill:write"
        resourceId={project.id}
        level="component"
      >
        <DistributeControl
          availablePlugins={availablePlugins}
          isLoadingPlugins={pluginsQuery.isPending}
          selectedPluginId={selectedPluginId}
          onSelect={setSelectedPluginId}
          isDistributing={distribute.isPending}
          onDistribute={() => void handleDistribute()}
        />
      </RequireScope>
    </div>
  );
}

function VersionTrackingBadge({
  distribution,
}: {
  distribution: SkillDistribution;
}): JSX.Element {
  if (distribution.pinnedVersionId) {
    return (
      <Badge variant="neutral" title={distribution.pinnedVersionId}>
        Pinned
      </Badge>
    );
  }
  return <Badge variant="information">Latest</Badge>;
}

function DistributeControl({
  availablePlugins,
  isLoadingPlugins,
  selectedPluginId,
  onSelect,
  isDistributing,
  onDistribute,
}: {
  availablePlugins: { id: string; name: string }[];
  isLoadingPlugins: boolean;
  selectedPluginId: string;
  onSelect: (pluginId: string) => void;
  isDistributing: boolean;
  onDistribute: () => void;
}): JSX.Element {
  if (isLoadingPlugins) {
    return <Skeleton className="h-9 w-full" />;
  }
  if (availablePlugins.length === 0) {
    return (
      <Type small muted>
        No more plugins to distribute to. Create a plugin to carry this skill.
      </Type>
    );
  }
  return (
    <div className="flex items-center gap-2">
      <Select value={selectedPluginId} onValueChange={onSelect}>
        <SelectTrigger className="w-full sm:w-64">
          <SelectValue placeholder="Select a plugin" />
        </SelectTrigger>
        <SelectContent>
          {availablePlugins.map((plugin) => (
            <SelectItem key={plugin.id} value={plugin.id}>
              {plugin.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        disabled={!selectedPluginId || isDistributing}
        onClick={onDistribute}
      >
        {isDistributing ? "Distributing..." : "Distribute"}
      </Button>
    </div>
  );
}
