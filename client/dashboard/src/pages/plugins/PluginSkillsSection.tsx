import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button as UiButton } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { SearchBar } from "@/components/ui/SearchBar";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import type { ViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useRoutes } from "@/routes";
import type { SkillDistribution } from "@gram/client/models/components/skilldistribution.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useQueryClient } from "@tanstack/react-query";
import { Sparkles, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { SettingsSection } from "@/components/detail/settings-section";
import {
  SkillPickerDialog,
  type SkillPickerResult,
} from "../skills/SkillPickerDialog";
import { SectionEmptyState } from "./SectionEmptyState";

/**
 * The Skills section of the plugin detail page: lists the skills this plugin
 * carries and lets writers add or remove skill distributions. Distributed
 * skills ship inside the generated plugin package.
 */
export function PluginSkillsSection({
  pluginId,
  viewMode,
  onMutated,
}: {
  pluginId: string;
  /** Page-level entry layout shared with the server section. */
  viewMode: ViewMode;
  /** Invoked after a successful change, e.g. to offer a marketplace publish. */
  onMutated: (message: string) => void;
}): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const [isAddSkillOpen, setIsAddSkillOpen] = useState(false);
  const [search, setSearch] = useState("");

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
  const normalizedSearch = search.trim().toLowerCase();
  const filteredDistributions = useMemo(() => {
    if (!normalizedSearch) return distributions;
    return distributions.filter(
      (d) =>
        d.skillDisplayName.toLowerCase().includes(normalizedSearch) ||
        d.skillName.toLowerCase().includes(normalizedSearch),
    );
  }, [distributions, normalizedSearch]);

  const undistribute = useUndistributeSkillMutation();

  const handleAddSkillsComplete = async ({
    addedCount,
    failedCount,
  }: SkillPickerResult) => {
    // Some mutations may succeed even when others fail, so always refresh the
    // cache before offering a failed-only retry.
    await invalidateAllSkillDistributions(queryClient);
    if (failedCount === 0) {
      onMutated(
        addedCount > 1
          ? `${addedCount} skills added to plugin`
          : "Skill added to plugin",
      );
      return;
    }
    toast.error(
      addedCount > 0
        ? `Added ${addedCount} skill${addedCount > 1 ? "s" : ""}, ${failedCount} failed`
        : `Unable to add skill${failedCount > 1 ? "s" : ""} to plugin`,
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
    listContent = <Skeleton className="h-24 w-full" />;
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
    <SettingsSection>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <SettingsSection.Header>
          <div className="flex items-center gap-2">
            <SettingsSection.Title>Skills</SettingsSection.Title>
            {/* The query drains page by page, so a count shown mid-drain would
                read as the total and then jump. Wait for the full membership. */}
            {isMembershipLoaded && distributions.length > 0 && (
              <span className="bg-muted text-muted-foreground rounded-full px-1.5 py-0.5 text-xs font-medium tabular-nums">
                {distributions.length}
              </span>
            )}
          </div>
          <SettingsSection.Description>
            Skills distributed to this plugin ship inside the plugin package and
            reach everyone who installs it.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <div className="flex items-center gap-2">
          {distributions.length > 0 && (
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search skills"
              className="h-9 w-56"
            />
          )}
          <RequireScope
            scope="skill:write"
            resourceId={project.id}
            level="component"
          >
            <Button
              variant="secondary"
              size="sm"
              disabled={!isMembershipLoaded}
              onClick={() => setIsAddSkillOpen(true)}
            >
              <Button.LeftIcon>
                <Icon name="plus" className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add Skill</Button.Text>
            </Button>
          </RequireScope>
        </div>
      </div>
      {listContent}

      <SkillPickerDialog
        open={isAddSkillOpen}
        onOpenChange={setIsAddSkillOpen}
        excludedSkillIds={distributions.map((item) => item.skillId)}
        target={{ pluginId }}
        title="Add Skills"
        description="Distribute project skills to this plugin bundle."
        actionLabel="Add"
        emptyMessage="No skills available to add. Record a skill in this project first."
        onBatchComplete={handleAddSkillsComplete}
      />
    </SettingsSection>
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
    <Card.Entity
      className="cursor-pointer"
      onClick={() => {
        void navigate(routes.skills.detail.href(distribution.skillId));
      }}
      icon={<Sparkles className="text-muted-foreground h-8 w-8" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <Text
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
        </Text>
        <SkillVersionBadge distribution={distribution} />
      </div>
      <Text small muted className="truncate font-mono">
        {distribution.skillName}
      </Text>

      <div className="mt-auto flex items-center justify-end gap-2 pt-2">
        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
        >
          <UiButton
            type="button"
            variant="tertiary"
            size="sm"
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
    </Card.Entity>
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
        <Text
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={distribution.skillDisplayName}
        >
          {distribution.skillDisplayName}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Text small muted className="truncate font-mono">
          {distribution.skillName}
        </Text>
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
              variant="tertiary"
              size="sm"
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
