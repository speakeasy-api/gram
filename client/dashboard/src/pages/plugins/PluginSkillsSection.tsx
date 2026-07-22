import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button as UiButton } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { DotCard } from "@/components/ui/dot-card";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { ViewMode } from "@/components/ui/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useRoutes } from "@/routes";
import type { Skill } from "@gram/client/models/components/skill.js";
import type { SkillDistribution } from "@gram/client/models/components/skilldistribution.js";
import { useDistributeSkillMutation } from "@gram/client/react-query/distributeSkill.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge, Button, cn, Icon } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Sparkles, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { SectionEmptyState } from "./SectionEmptyState";

/**
 * The Skills section of the plugin detail page: lists the skills this plugin
 * carries and lets writers add or remove skill distributions. Distributed
 * skills ship inside the generated plugin package.
 */
export function PluginSkillsSection({
  pluginId,
  searchQuery,
  viewMode,
  onMutated,
}: {
  pluginId: string;
  /** Page-level search query; narrows the listed skill distributions. */
  searchQuery: string;
  /** Page-level entry layout shared with the server section. */
  viewMode: ViewMode;
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
  // The full membership backs both the list and the add-picker's exclusion
  // set, so partial pages would offer already-distributed skills.
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

  // Case-insensitive match on the card's visible labels: the skill display
  // name and its mono slug-style name.
  const normalizedSearch = searchQuery.trim().toLowerCase();
  const filteredDistributions = useMemo(() => {
    if (!normalizedSearch) return distributions;
    return distributions.filter(
      (d) =>
        d.skillDisplayName.toLowerCase().includes(normalizedSearch) ||
        d.skillName.toLowerCase().includes(normalizedSearch),
    );
  }, [distributions, normalizedSearch]);

  const skillsQuery = useSkillsInfinite({ limit: 200 }, undefined, {
    throwOnError: false,
    enabled: isAddSkillOpen,
  });
  useDrainInfiniteQuery(skillsQuery, isAddSkillOpen);
  const isSkillListLoading = skillsQuery.isPending || skillsQuery.hasNextPage;
  // Skills without a valid version stay listed but disabled: the distribute
  // endpoint rejects them, so the picker explains why and links to the fix.
  const availableSkills = useMemo(() => {
    const distributedSkillIds = new Set(distributions.map((d) => d.skillId));
    return (
      skillsQuery.data?.pages.flatMap((page) => page.result.skills) ?? []
    ).filter((skill) => !distributedSkillIds.has(skill.id));
  }, [distributions, skillsQuery.data?.pages]);

  const distribute = useDistributeSkillMutation();
  const undistribute = useUndistributeSkillMutation();
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([]);
  // Shared mutation observers only reflect their latest call, so a local flag
  // covers the whole allSettled batch to keep the dialog locked until it ends.
  const [isBatchAdding, setIsBatchAdding] = useState(false);

  const openAddSkillDialog = (open: boolean) => {
    setIsAddSkillOpen(open);
    if (open) setSelectedSkillIds([]);
  };

  const toggleSkill = (skillId: string) => {
    setSelectedSkillIds((prev) =>
      prev.includes(skillId)
        ? prev.filter((id) => id !== skillId)
        : [...prev, skillId],
    );
  };

  const handleAddSkills = async () => {
    if (selectedSkillIds.length === 0 || isBatchAdding) return;
    setIsBatchAdding(true);
    try {
      // allSettled + unconditional invalidation: some mutations in the batch
      // may succeed even when others fail, and the cache must reflect what the
      // server actually committed.
      const results = await Promise.allSettled(
        selectedSkillIds.map((skillId) =>
          distribute.mutateAsync({
            request: { distributeSkillRequestBody: { id: skillId, pluginId } },
          }),
        ),
      );
      await invalidateAllSkillDistributions(queryClient);
      const failedIds = selectedSkillIds.filter(
        (_, index) => results[index]?.status === "rejected",
      );
      const addedCount = selectedSkillIds.length - failedIds.length;
      if (failedIds.length === 0) {
        setIsAddSkillOpen(false);
        onMutated(
          addedCount > 1
            ? `${addedCount} skills added to plugin`
            : "Skill added to plugin",
        );
        return;
      }
      // Keep only the failures selected so a retry doesn't re-add the skills
      // the server already committed.
      setSelectedSkillIds(failedIds);
      toast.error(
        addedCount > 0
          ? `Added ${addedCount} skill${addedCount > 1 ? "s" : ""}, ${failedIds.length} failed`
          : `Unable to add skill${failedIds.length > 1 ? "s" : ""} to plugin`,
      );
    } finally {
      setIsBatchAdding(false);
    }
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

  // Precomputed list body keeps the JSX below free of nested ternaries while
  // distinguishing "nothing distributed yet" from "no search matches".
  let listContent: JSX.Element;
  if (distributionsQuery.error && !distributionsQuery.data) {
    listContent = (
      <ErrorAlert
        title="Unable to load distributed skills"
        error={distributionsQuery.error}
      />
    );
  } else if (!isMembershipLoaded) {
    listContent = <Skeleton className="h-24 w-full rounded-xl" />;
  } else if (distributions.length === 0) {
    listContent = (
      <SectionEmptyState
        title="No skills distributed yet"
        subtitle="Add project skills to bundle them with this plugin."
      />
    );
  } else if (filteredDistributions.length === 0) {
    listContent = <SectionEmptyState title="No skills match your search" />;
  } else if (viewMode === "table") {
    listContent = (
      <DotTable
        headers={[
          { label: "Name" },
          { label: "Identifier" },
          { label: "Version" },
          { label: "", className: "text-right" },
        ]}
      >
        {filteredDistributions.map((distribution) => (
          <PluginSkillTableRow
            key={distribution.id}
            distribution={distribution}
            isRemoving={undistribute.isPending}
            onRemove={() => handleRemoveSkill(distribution)}
          />
        ))}
      </DotTable>
    );
  } else {
    listContent = (
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
        {filteredDistributions.map((distribution) => (
          <PluginSkillCard
            key={distribution.id}
            distribution={distribution}
            isRemoving={undistribute.isPending}
            onRemove={() => handleRemoveSkill(distribution)}
          />
        ))}
      </div>
    );
  }

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
            disabled={!isMembershipLoaded}
            onClick={() => openAddSkillDialog(true)}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add Skill</Button.Text>
          </Button>
        </RequireScope>
      </div>
      <div className="mb-8">{listContent}</div>

      {/* Add Skill Dialog */}
      <Dialog open={isAddSkillOpen} onOpenChange={openAddSkillDialog}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Add Skills</Dialog.Title>
            <Dialog.Description>
              Distribute project skills to this plugin bundle.
            </Dialog.Description>
          </Dialog.Header>
          <div className="flex flex-col gap-2">
            <label className="text-sm font-medium">Skills</label>
            {isSkillListLoading ? (
              <Skeleton className="h-24 w-full" />
            ) : availableSkills.length > 0 ? (
              <div className="flex max-h-64 flex-col gap-0.5 overflow-y-auto rounded-md border p-1">
                {availableSkills.map((skill) => (
                  <AddSkillOption
                    key={skill.id}
                    skill={skill}
                    checked={selectedSkillIds.includes(skill.id)}
                    disabled={isBatchAdding}
                    onToggle={() => toggleSkill(skill.id)}
                  />
                ))}
              </div>
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
              type="button"
              disabled={
                isBatchAdding ||
                isSkillListLoading ||
                selectedSkillIds.length === 0
              }
              onClick={() => void handleAddSkills()}
            >
              {selectedSkillIds.length > 1
                ? `Add ${selectedSkillIds.length} skills`
                : "Add"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function AddSkillOption({
  skill,
  checked,
  disabled,
  onToggle,
}: {
  skill: Skill;
  checked: boolean;
  disabled: boolean;
  onToggle: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const isDistributable = skill.hasValidVersion;

  return (
    <label
      className={cn(
        "flex items-center gap-2 rounded-sm px-2 py-1.5 text-sm",
        isDistributable ? "hover:bg-accent cursor-pointer" : "opacity-70",
      )}
    >
      <Checkbox
        checked={checked}
        disabled={disabled || !isDistributable}
        onCheckedChange={onToggle}
      />
      <div className="flex min-w-0 flex-col">
        <span className="truncate">{skill.displayName}</span>
        <span className="text-muted-foreground truncate font-mono text-xs">
          {skill.name}
        </span>
        {!isDistributable && (
          <span className="text-muted-foreground text-xs">
            {skill.versionCount === 0
              ? "No versions recorded"
              : "No valid version"}
            {" · "}
            <Link
              to={routes.skills.detail.href(skill.id)}
              className="text-foreground underline underline-offset-2"
            >
              Fix
            </Link>
          </span>
        )}
      </div>
    </label>
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
          <Link
            to={routes.skills.detail.href(distribution.skillId)}
            onClick={(e) => e.stopPropagation()}
          >
            {distribution.skillDisplayName}
          </Link>
        </Type>
        <SkillVersionBadge distribution={distribution} />
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

function PluginSkillTableRow({
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
  const href = routes.skills.detail.href(distribution.skillId);

  return (
    <DotRow
      icon={<Sparkles className="text-muted-foreground h-5 w-5" />}
      href={href}
      ariaLabel={`View skill ${distribution.skillDisplayName}`}
    >
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={distribution.skillDisplayName}
        >
          {distribution.skillDisplayName}
        </Type>
      </td>
      <td className="px-3 py-3">
        <Type small muted className="truncate font-mono">
          {distribution.skillName}
        </Type>
      </td>
      <td className="px-3 py-3">
        <SkillVersionBadge distribution={distribution} />
      </td>
      <td className="px-3 py-3">
        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <div
            className="relative z-20 flex items-center justify-end"
            onClick={(event) => event.stopPropagation()}
          >
            <UiButton
              type="button"
              variant="ghost"
              size="icon-sm"
              tooltip="Remove skill"
              aria-label={`Remove skill ${distribution.skillDisplayName}`}
              className="hover:text-destructive"
              disabled={isRemoving}
              onClick={onRemove}
            >
              <Trash2 className="h-4 w-4" />
            </UiButton>
          </div>
        </RequireScope>
      </td>
    </DotRow>
  );
}

function SkillVersionBadge({
  distribution,
}: {
  distribution: SkillDistribution;
}): JSX.Element {
  if (distribution.pinnedVersionId) {
    return (
      <Badge
        variant="neutral"
        className="text-xs"
        title={distribution.pinnedVersionId}
      >
        Pinned
      </Badge>
    );
  }

  return (
    <Badge variant="information" className="text-xs">
      Latest
    </Badge>
  );
}
