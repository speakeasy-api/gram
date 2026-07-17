import { RequireScope } from "@/components/require-scope";
import { Button as UiButton } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import type { SkillDistribution } from "@gram/client/models/components/skilldistribution.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge, Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Sparkles, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

/**
 * The Skills section of the plugin detail page: lists the skills this plugin
 * carries and lets writers add or remove skill distributions. Distributed
 * skills ship inside the generated plugin package.
 */
export function PluginSkillsSection({
  pluginId,
  onMutated,
}: {
  pluginId: string;
  /** Invoked after a successful change, e.g. to offer a marketplace publish. */
  onMutated: (message: string) => void;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const [isAddSkillOpen, setIsAddSkillOpen] = useState(false);

  const distributionsQuery = useSkillDistributionsInfinite(
    { pluginId, limit: 50 },
    undefined,
    { throwOnError: false },
  );
  const distributions = useMemo(
    () =>
      distributionsQuery.data?.pages.flatMap(
        (page) => page.result.distributions,
      ) ?? [],
    [distributionsQuery.data?.pages],
  );

  const skillsQuery = useSkillsInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    enabled: isAddSkillOpen,
  });
  const availableSkills = useMemo(() => {
    const distributedSkillIds = new Set(distributions.map((d) => d.skillId));
    return (
      skillsQuery.data?.pages.flatMap((page) => page.result.skills) ?? []
    ).filter((skill) => !distributedSkillIds.has(skill.id));
  }, [distributions, skillsQuery.data?.pages]);

  const distribute = useDistributeSkillMutation();
  const undistribute = useUndistributeSkillMutation();

  const handleAddSkill: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const skillId = new FormData(e.currentTarget).get("skillId") as string;
    if (!skillId) return;
    distribute.mutate(
      {
        request: { distributeSkillRequestBody: { id: skillId, pluginId } },
      },
      {
        onSuccess: () => {
          setIsAddSkillOpen(false);
          void invalidateAllSkillDistributions(queryClient);
          onMutated("Skill added to plugin");
        },
        onError: () => {
          toast.error("Unable to add skill to plugin");
        },
      },
    );
  };

  const handleRemoveSkill = (distribution: SkillDistribution) => {
    undistribute.mutate(
      {
        request: {
          undistributeSkillRequestBody: {
            id: distribution.skillId,
            pluginId,
          },
        },
      },
      {
        onSuccess: () => {
          void invalidateAllSkillDistributions(queryClient);
          onMutated("Skill removed from plugin");
        },
        onError: () => {
          toast.error("Unable to remove skill from plugin");
        },
      },
    );
  };

  return (
    <>
      <div className="mb-3 flex items-center gap-3">
        <div className="border-border flex-1 border-t" />
        <div className="flex shrink-0 items-center gap-2">
          <Type
            small
            muted
            className="font-mono text-xs tracking-wide uppercase"
          >
            Skills
          </Type>
          {distributions.length > 0 && (
            <span className="bg-muted text-muted-foreground rounded-full px-1.5 py-0.5 text-xs font-medium tabular-nums">
              {distributions.length}
            </span>
          )}
        </div>
        <div className="border-border flex-1 border-t" />
      </div>
      <div className="mb-3 flex items-center justify-between gap-4">
        <Type small muted className="max-w-md">
          Skills distributed to this plugin ship inside the plugin package and
          reach everyone who installs it.
        </Type>
        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsAddSkillOpen(true)}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add Skill</Button.Text>
          </Button>
        </RequireScope>
      </div>
      <div className="mb-8">
        {distributionsQuery.isPending && !distributionsQuery.data ? (
          <Skeleton className="h-24 w-full rounded-xl" />
        ) : distributions.length === 0 ? (
          <Stack
            gap={2}
            className="border-border rounded-xl border py-8"
            align="center"
            justify="center"
          >
            <Type variant="body" muted>
              No skills distributed yet
            </Type>
            <Type small muted>
              Add project skills to bundle them with this plugin.
            </Type>
          </Stack>
        ) : (
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            {distributions.map((distribution) => (
              <PluginSkillCard
                key={distribution.id}
                distribution={distribution}
                isRemoving={undistribute.isPending}
                onRemove={() => handleRemoveSkill(distribution)}
              />
            ))}
          </div>
        )}
        {distributionsQuery.hasNextPage && (
          <div className="mt-4">
            <Button
              variant="secondary"
              size="sm"
              disabled={distributionsQuery.isFetchingNextPage}
              onClick={() => void distributionsQuery.fetchNextPage()}
            >
              <Button.Text>
                {distributionsQuery.isFetchingNextPage
                  ? "Loading..."
                  : "Load more skills"}
              </Button.Text>
            </Button>
          </div>
        )}
      </div>

      {/* Add Skill Dialog */}
      <Dialog open={isAddSkillOpen} onOpenChange={setIsAddSkillOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Add Skill</Dialog.Title>
            <Dialog.Description>
              Distribute a project skill to this plugin bundle.
            </Dialog.Description>
          </Dialog.Header>
          <form onSubmit={handleAddSkill} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <label className="text-sm font-medium">Skill</label>
              {skillsQuery.isPending ? (
                <Skeleton className="h-9 w-full" />
              ) : availableSkills.length > 0 ? (
                <select
                  name="skillId"
                  className="bg-background rounded-md border px-3 py-2 text-sm"
                  required
                >
                  <option value="">Select a skill</option>
                  {availableSkills.map((skill) => (
                    <option key={skill.id} value={skill.id}>
                      {skill.displayName}
                    </option>
                  ))}
                </select>
              ) : (
                <Type muted small>
                  No skills available to add. Record a skill in this project
                  first.
                </Type>
              )}
            </div>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setIsAddSkillOpen(false)}
                type="button"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={
                  distribute.isPending ||
                  skillsQuery.isPending ||
                  availableSkills.length === 0
                }
              >
                Add
              </Button>
            </Dialog.Footer>
          </form>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function PluginSkillCard({
  distribution,
  isRemoving,
  onRemove,
}: {
  distribution: SkillDistribution;
  isRemoving: boolean;
  onRemove: () => void;
}): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => {
        void navigate(routes.skills.detail.href(distribution.skillId));
      }}
      icon={<Sparkles className="text-muted-foreground h-8 w-8" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Type
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={distribution.skillDisplayName}
        >
          {distribution.skillDisplayName}
        </Type>
        {distribution.pinnedVersionId ? (
          <Badge
            variant="neutral"
            className="text-xs"
            title={distribution.pinnedVersionId}
          >
            Pinned
          </Badge>
        ) : (
          <Badge variant="information" className="text-xs">
            Latest
          </Badge>
        )}
      </div>
      <Type small muted className="truncate font-mono">
        {distribution.skillName}
      </Type>

      <div className="mt-auto flex items-center justify-end gap-2 pt-2">
        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <UiButton
            type="button"
            variant="ghost"
            size="icon-sm"
            tooltip="Remove skill"
            aria-label="Remove skill"
            className="hover:text-destructive"
            disabled={isRemoving}
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
          >
            <Trash2 className="h-4 w-4" />
          </UiButton>
        </RequireScope>
      </div>
    </DotCard>
  );
}
