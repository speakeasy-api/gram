import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * Lists the plugins an individual skill is distributed to and lets writers
 * revoke plugin distributions. Adding distributions happens through the
 * plugin banner at the top of the skill detail page.
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
  const undistribute = useUndistributeSkillMutation();

  const distributions = useMemo(
    () =>
      distributionsQuery.data?.pages.flatMap(
        (page) => page.result.distributions,
      ) ?? [],
    [distributionsQuery.data?.pages],
  );

  const handleUndistribute = async (
    distribution: PluginSkillDistribution,
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
      <ErrorAlert
        title="Unable to load distributions"
        error={distributionsQuery.error}
      />
    );
  }

  return (
    <div className="space-y-3">
      {distributions.length === 0 ? (
        <div className="border-border rounded-xl border border-dashed p-6">
          <Type small muted>
            Not distributed to any plugins yet. Use the banner above to
            distribute this skill.
          </Type>
        </div>
      ) : (
        <ul className="border-border bg-card divide-y overflow-hidden rounded-xl border">
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
    </div>
  );
}

function VersionTrackingBadge({
  distribution,
}: {
  distribution: PluginSkillDistribution;
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
