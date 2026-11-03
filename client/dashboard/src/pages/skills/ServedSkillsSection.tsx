import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { useRoutes } from "@/routes";
import type { SkillLink } from "@gram/client/models/components/skilllink.js";
import {
  invalidateAllSkillLinks,
  useSkillLinksInfinite,
} from "@gram/client/react-query/skillLinks.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useUnlinkSkillMutation } from "@gram/client/react-query/unlinkSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { type ReactNode, useMemo, useState } from "react";
import { Link, Navigate } from "react-router";
import { toast } from "sonner";
import { SkillDistributionVersionBadge } from "./skill-badges";
import { useCanWriteSkillLinkTarget } from "./use-skill-link-write-access";
import {
  type SkillLinkTarget,
  skillLinksFilter,
  sortSkillLinks,
} from "./skill-links";
import { SkillPickerDialog, type SkillPickerResult } from "./SkillPickerDialog";

/**
 * The route behind a Skills tab on an MCP server or gateway page. The tab only
 * exists while skills over MCP is on for the organization; a direct link with
 * the flag off lands on `fallbackHref` instead of an empty page. The flag is
 * decided before the scope so a reader without skill access is redirected,
 * not walled, while it is off.
 */
export function SkillsOverMcpTabRoute({
  fallbackHref,
  children,
}: {
  fallbackHref: string;
  children: ReactNode;
}): JSX.Element {
  const project = useProject();
  const flag = useFeatureFlag(FEATURE_FLAGS.skillsOverMcp);
  switch (flag.status) {
    case "enabled":
      return (
        <RequireScope
          scope="skill:read"
          resourceId={project.id}
          projectId={project.id}
          level="page"
        >
          {children}
        </RequireScope>
      );
    case "loading":
      return <SkeletonTable />;
    case "disabled":
    case "missing":
    case "error":
      return <Navigate to={fallbackHref} replace />;
  }
}

/**
 * The skills agents load over MCP when they connect to one server or gateway,
 * read from the skill links that point at it. Writers add project skills
 * with a picker and remove a skill per row. The MCP server and gateway pages
 * share this section.
 */
export function ServedSkillsSection({
  target,
  surfaceName,
  grantResourceId,
  notice,
}: {
  target: SkillLinkTarget;
  /** The server or gateway name, for copy such as "agents connected to …". */
  surfaceName: string;
  /** Resource id MCP grants use for an `mcp_server` target, when not its id. */
  grantResourceId?: string;
  /** Shown above the list, e.g. why agents can't connect right now. */
  notice?: ReactNode;
}): JSX.Element {
  const { grants } = useRBAC();
  const project = useProject();
  const scope = usePluginQueryScope();
  const queryClient = useQueryClient();
  const canWrite = useCanWriteSkillLinkTarget(target, { grantResourceId });
  // The picker and the last-loaded times read the project's skill list,
  // which needs project-wide skill read.
  const canReadAllSkills = hasScopeInGrants(
    grants ?? [],
    "skill:read",
    project.id,
    project.id,
  );
  const [isPickerOpen, setIsPickerOpen] = useState(false);

  const linksQuery = useSkillLinksInfinite(
    { ...scope, ...skillLinksFilter(target), limit: 50 },
    undefined,
    { throwOnError: false },
  );
  useDrainInfiniteQuery(linksQuery);
  const links = useMemo(
    () =>
      sortSkillLinks(
        linksQuery.data?.pages.flatMap((page) => page.result.links) ?? [],
      ),
    [linksQuery.data?.pages],
  );
  const isLinksLoaded = !!linksQuery.data && !linksQuery.hasNextPage;

  // Same key as the picker's query, so opening it reuses this list.
  const skillsQuery = useSkillsInfinite({ ...scope, limit: 200 }, undefined, {
    throwOnError: false,
    enabled: canReadAllSkills,
  });
  useDrainInfiniteQuery(skillsQuery, canReadAllSkills);
  const lastLoadedBySkill = useMemo(() => {
    const result = new Map<string, Date | undefined>();
    for (const page of skillsQuery.data?.pages ?? []) {
      for (const skill of page.result.skills) {
        result.set(skill.id, skill.lastLoadedOverMcpAt);
      }
    }
    return result;
  }, [skillsQuery.data?.pages]);
  const isLastLoadedKnown =
    canReadAllSkills && !!skillsQuery.data && !skillsQuery.hasNextPage;

  const unlink = useUnlinkSkillMutation();
  const handleRemove = (link: SkillLink) => {
    if (!canWrite) return;
    unlink.mutate(
      { request: { unlinkSkillRequestBody: { linkId: link.id } } },
      {
        onSuccess: () => {
          void invalidateAllSkillLinks(queryClient);
          toast.success(`${link.skillDisplayName} removed from ${surfaceName}`);
        },
        onError: () => {
          toast.error("Unable to remove skill");
        },
      },
    );
  };

  const handleAddComplete = async ({
    addedCount,
    failedCount,
  }: SkillPickerResult) => {
    await invalidateAllSkillLinks(queryClient);
    if (failedCount === 0) {
      toast.success(
        addedCount > 1 ? `${addedCount} skills added` : "Skill added",
      );
      return;
    }
    toast.error(
      addedCount > 0
        ? `Added ${addedCount} skill${addedCount > 1 ? "s" : ""}, ${failedCount} failed`
        : `Unable to add skill${failedCount > 1 ? "s" : ""}`,
    );
  };

  const canAdd = canWrite && canReadAllSkills;
  const addControl = canAdd ? (
    <Button
      size="sm"
      disabled={!isLinksLoaded}
      onClick={() => setIsPickerOpen(true)}
    >
      <Button.LeftIcon>
        <Icon name="plus" className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>Add skill</Button.Text>
    </Button>
  ) : null;

  const columns = servedSkillColumns({
    canRemove: canWrite,
    isRemoving: unlink.isPending,
    onRemove: handleRemove,
    surfaceName,
    lastLoaded: isLastLoadedKnown ? lastLoadedBySkill : undefined,
  });

  let body: JSX.Element;
  if (linksQuery.error) {
    // Any failed page, not only the first: a later page's failure stops the
    // drain and would otherwise leave the skeleton up for good.
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load skills"
        description={`The skills served by ${surfaceName} could not be loaded.`}
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() =>
              void (linksQuery.data && linksQuery.hasNextPage
                ? linksQuery.fetchNextPage()
                : linksQuery.refetch())
            }
          >
            Retry
          </Button>
        }
      />
    );
  } else if (!isLinksLoaded) {
    body = <SkeletonTable />;
  } else if (links.length === 0) {
    body = (
      <InlineEmptyState
        icon="sparkles"
        heading="No skills served yet"
        description={
          canAdd
            ? `Add a project skill to serve it to agents connected to ${surfaceName}.`
            : `Agents connected to ${surfaceName} don't load any skills yet.`
        }
        action={addControl ?? undefined}
      />
    );
  } else {
    body = <Table columns={columns} data={links} rowKey={(link) => link.id} />;
  }

  return (
    <Page.Section>
      <Page.Section.Title>Skills</Page.Section.Title>
      <Page.Section.Description>
        Skills agents load over MCP when they connect to {surfaceName}.
      </Page.Section.Description>
      {addControl && links.length > 0 ? (
        <Page.Section.CTA>{addControl}</Page.Section.CTA>
      ) : null}
      {/* The section renders only its named slots, so the picker lives in
          the body next to the list it changes. */}
      <Page.Section.Body>
        {notice}
        {body}
        {canAdd && (
          <SkillPickerDialog
            open={isPickerOpen}
            onOpenChange={setIsPickerOpen}
            excludedSkillIds={links.map((link) => link.skillId)}
            target={{ linkTarget: target }}
            title="Add skills"
            description={`Serve project skills to agents connected to ${surfaceName}.`}
            actionLabel="Add"
            emptyMessage="No skills available to add. Record a skill in this project first."
            onBatchComplete={handleAddComplete}
          />
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}

function servedSkillColumns({
  canRemove,
  isRemoving,
  onRemove,
  surfaceName,
  lastLoaded,
}: {
  canRemove: boolean;
  isRemoving: boolean;
  onRemove: (link: SkillLink) => void;
  surfaceName: string;
  /** Per-skill last load, or undefined while it can't be known. */
  lastLoaded: ReadonlyMap<string, Date | undefined> | undefined;
}): Column<SkillLink>[] {
  const columns: Column<SkillLink>[] = [
    {
      key: "skill",
      header: "Skill",
      width: "1fr",
      render: (link) => <ServedSkillCell link={link} />,
    },
    {
      key: "version",
      header: "Version",
      width: "110px",
      render: (link) => (
        <SkillDistributionVersionBadge pinnedVersionId={link.pinnedVersionId} />
      ),
    },
    {
      key: "lastLoaded",
      header: "Last loaded over MCP",
      width: "190px",
      render: (link) =>
        lastLoaded ? (
          <LastLoadedCell loadedAt={lastLoaded.get(link.skillId)} />
        ) : (
          <Text small muted>
            —
          </Text>
        ),
    },
  ];
  if (canRemove) {
    columns.push({
      key: "actions",
      header: "",
      width: "64px",
      render: (link) => (
        <div className="flex justify-end">
          <Button
            type="button"
            variant="tertiary"
            size="sm"
            tooltip="Stop serving"
            aria-label={`Remove ${link.skillDisplayName} from ${surfaceName}`}
            className="hover:text-destructive"
            disabled={isRemoving}
            onClick={() => onRemove(link)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ),
    });
  }
  return columns;
}

function ServedSkillCell({ link }: { link: SkillLink }): JSX.Element {
  const routes = useRoutes();
  return (
    <div className="min-w-0">
      <Link
        to={routes.skills.detail.href(link.skillId)}
        className="block truncate text-sm font-medium hover:underline"
        title={link.skillDisplayName}
      >
        {link.skillDisplayName}
      </Link>
      <Text small muted className="block truncate font-mono text-xs">
        {link.skillName}
      </Text>
    </div>
  );
}

function LastLoadedCell({
  loadedAt,
}: {
  loadedAt?: Date | undefined;
}): JSX.Element {
  if (!loadedAt) {
    return (
      <Text small muted>
        Never
      </Text>
    );
  }
  return (
    <Text small muted title={dateTimeFormatters.full.format(loadedAt)}>
      <HumanizeDateTime date={loadedAt} />
    </Text>
  );
}
