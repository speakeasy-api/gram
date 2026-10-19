import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { RequirePluginWrite } from "@/components/require-plugin-write";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useDrainInfiniteQuery } from "@/hooks/useDrainInfiniteQuery";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { usePluginServerQueries } from "@/pages/plugins/usePluginServerQueries";
import {
  groupServedSkillsByPlugin,
  hostedServerIndex,
  pluginServesToolset,
  pluginsHaveUnresolvedServers,
  type ServedSkillGroup,
} from "@/pages/skills/mcp-served-skills";
import { SkillDistributionVersionBadge } from "@/pages/skills/skill-badges";
import {
  SkillPickerDialog,
  type SkillPickerResult,
} from "@/pages/skills/SkillPickerDialog";
import { useProjectPluginsWithServers } from "@/pages/skills/use-skills-over-mcp";
import { useRoutes } from "@/routes";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";
import {
  invalidateAllSkillDistributions,
  useSkillDistributionsInfinite,
} from "@gram/client/react-query/skillDistributions.js";
import { useUndistributeSkillMutation } from "@gram/client/react-query/undistributeSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, Navigate } from "react-router";
import { toast } from "sonner";
import { mcpDetailTabHref } from "./MCPDetailsRouting";

type SkillsTabToolset = {
  id: string;
  slug: string;
  name: string;
  mcpEnabled?: boolean | undefined;
};

/**
 * The route behind the Skills tab. The tab only exists while skills over MCP
 * is on for the organization; a direct link with the flag off lands on the
 * overview instead of an empty page. The flag is decided before the scope so
 * a reader without skill access is redirected, not walled, while it is off.
 * Served skills are plugin distributions, which skill readers of this project
 * may list (the same gate as the Skills page); adding and removing is gated
 * per control on plugin write.
 */
export function MCPSkillsTabRoute({
  toolset,
}: {
  toolset: SkillsTabToolset;
}): JSX.Element {
  const routes = useRoutes();
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
          <MCPSkillsTab toolset={toolset} />
        </RequireScope>
      );
    case "loading":
      return <SkeletonTable />;
    case "disabled":
    case "missing":
    case "error":
      return (
        <Navigate
          to={mcpDetailTabHref(routes, toolset.slug, "overview")}
          replace
        />
      );
  }
}

/**
 * The skills agents receive when they connect to this hosted MCP server. A
 * server serves the skills of every plugin that includes it, to the people and
 * agents the plugin is assigned to, so the list is grouped by plugin and
 * adding or removing a skill is a change to that plugin's distributions.
 */
export function MCPSkillsTab({
  toolset,
}: {
  toolset: SkillsTabToolset;
}): JSX.Element {
  const routes = useRoutes();
  const { grants } = useRBAC();
  const project = useProject();
  const scope = usePluginQueryScope();
  const queryClient = useQueryClient();
  const canWritePlugin = usePluginWriteAccess();
  // The picker lists every project skill, which needs project-wide skill
  // read; resource-scoped readers can still see what is served.
  const canReadAllSkills = hasScopeInGrants(
    grants,
    "skill:read",
    project.id,
    project.id,
  );
  const [pickerPluginId, setPickerPluginId] = useState<string | null>(null);

  const pluginsQuery = useProjectPluginsWithServers();
  // A plugin may include this server under any mcp_servers row fronting the
  // toolset, not only the canonical one, and the backend serves skills
  // through every such row; the server list resolves those rows.
  const { serversQuery, canReadServers } = usePluginServerQueries();
  const isServerIndexLoading =
    canReadServers && !serversQuery.data && serversQuery.isLoading;
  const hostedIndex = useMemo(
    () =>
      hostedServerIndex(serversQuery.data?.mcpServers, [
        { id: toolset.id, mcpEnabled: toolset.mcpEnabled },
      ]),
    [serversQuery.data?.mcpServers, toolset.id, toolset.mcpEnabled],
  );
  const servingPlugins = useMemo(
    () =>
      (pluginsQuery.plugins ?? []).filter((plugin) =>
        pluginServesToolset(plugin, toolset.id, hostedIndex),
      ),
    [pluginsQuery.plugins, toolset.id, hostedIndex],
  );
  // A plugin server keyed by an mcp_servers id the list does not resolve may
  // be a wrapper of this toolset the reader cannot view, so an empty
  // membership is not proof that no plugin includes the server.
  const hasUnresolvedServers = useMemo(
    () => pluginsHaveUnresolvedServers(pluginsQuery.plugins ?? [], hostedIndex),
    [pluginsQuery.plugins, hostedIndex],
  );

  // Every distribution in the project, then grouped under the plugins that
  // include this server: one query instead of one per plugin, and the full
  // membership backs the picker's exclusion set.
  const distributionsQuery = useSkillDistributionsInfinite(
    { ...scope, limit: 50 },
    undefined,
    { throwOnError: false },
  );
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
  const groups = useMemo(
    () => groupServedSkillsByPlugin(distributions, servingPlugins),
    [distributions, servingPlugins],
  );
  const servedCount = groups.reduce(
    (sum, group) => sum + group.items.length,
    0,
  );

  const undistribute = useUndistributeSkillMutation();
  const handleRemove = (distribution: PluginSkillDistribution) => {
    if (!canWritePlugin) return;
    undistribute.mutate(
      {
        request: {
          undistributeSkillRequestBody: {
            id: distribution.skillId,
            pluginId: distribution.pluginId,
          },
        },
      },
      {
        onSuccess: () => {
          void invalidateAllSkillDistributions(queryClient);
          toast.success(
            `Removed ${distribution.skillDisplayName} from ${distribution.pluginName}`,
          );
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
    await invalidateAllSkillDistributions(queryClient);
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

  const pickerPlugin = servingPlugins.find(
    (plugin) => plugin.id === pickerPluginId,
  );
  const addControl =
    canReadAllSkills && servingPlugins.length > 0 ? (
      <RequirePluginWrite>
        <AddServedSkillButton
          plugins={servingPlugins}
          disabled={!isMembershipLoaded}
          onPick={setPickerPluginId}
        />
      </RequirePluginWrite>
    ) : null;

  const columns = servedSkillColumns({
    canRemove: canWritePlugin,
    isRemoving: undistribute.isPending,
    onRemove: handleRemove,
  });

  const goToPlugins = (
    <routes.plugins.Link>
      <Button size="sm" variant="secondary">
        Go to plugins
      </Button>
    </routes.plugins.Link>
  );

  let body: JSX.Element;
  if (pluginsQuery.isPermissionLoading) {
    // Whether the reader may list plugins is unknown until the grants load;
    // deciding denial now would flash the locked state at every reader.
    body = <SkeletonTable />;
  } else if (!pluginsQuery.canReadPlugins) {
    // Skill read opens the tab, but which plugins include the server is a
    // plugin listing, which needs organization read or plugin write.
    body = (
      <InlineEmptyState
        icon="lock"
        heading="You can't see which plugins include this server"
        description="Skills reach agents through plugins. Listing the plugins that include this server needs organization read access."
      />
    );
  } else if (pluginsQuery.error && !pluginsQuery.plugins) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load plugins"
        description="The plugins that include this server could not be loaded."
        action={
          <Button size="sm" variant="secondary" onClick={pluginsQuery.refetch}>
            Retry
          </Button>
        }
      />
    );
  } else if (canReadServers && serversQuery.error && !serversQuery.data) {
    // Without the server list, plugins that include this server through a
    // non-canonical wrapper would silently vanish from the membership.
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load MCP servers"
        description="The list that tells which plugins include this server could not be loaded."
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void serversQuery.refetch()}
          >
            Retry
          </Button>
        }
      />
    );
  } else if (distributionsQuery.error) {
    // Any failed page, not only the first: a later page's failure keeps the
    // loaded pages, stops the drain and would otherwise leave the skeleton
    // up for good.
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load skills"
        description="The skills served by this server could not be loaded."
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() =>
              void (distributionsQuery.data && distributionsQuery.hasNextPage
                ? distributionsQuery.fetchNextPage()
                : distributionsQuery.refetch())
            }
          >
            Retry
          </Button>
        }
      />
    );
  } else if (
    pluginsQuery.isLoading ||
    !pluginsQuery.plugins ||
    isServerIndexLoading ||
    !isMembershipLoaded
  ) {
    body = <SkeletonTable />;
  } else if (servingPlugins.length === 0 && hasUnresolvedServers) {
    body = (
      <InlineEmptyState
        icon="puzzle"
        heading="No plugin you can see includes this server"
        description="Some plugins include MCP servers you can't view, and one of them may be this server. Skills reach agents through plugins: add this server to a plugin, then distribute skills to that plugin."
        action={goToPlugins}
      />
    );
  } else if (servingPlugins.length === 0) {
    body = (
      <InlineEmptyState
        icon="puzzle"
        heading="No plugin includes this server yet"
        description="Skills reach agents through plugins. Add this server to a plugin, then distribute skills to that plugin."
        action={goToPlugins}
      />
    );
  } else if (servedCount === 0) {
    body = (
      <InlineEmptyState
        icon="sparkles"
        heading="No skills served yet"
        description={`Distribute a skill to ${listNames(servingPlugins)} to serve it to agents connected to ${toolset.name}.`}
        action={addControl ?? undefined}
      />
    );
  } else {
    body = (
      <Table
        columns={columns}
        data={groups}
        rowKey={(distribution) => distribution.id}
        renderGroupHeader={(group) => {
          const served = groups.find(
            (candidate) => candidate.key === group.key,
          );
          return served ? <ServedPluginHeader group={served} /> : null;
        }}
      />
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>Skills</Page.Section.Title>
      <Page.Section.Description>
        Skills agents load when they connect to this server. A plugin that
        includes the server serves its skills to the people and agents it is
        assigned to.
      </Page.Section.Description>
      {addControl && servedCount > 0 ? (
        <Page.Section.CTA>{addControl}</Page.Section.CTA>
      ) : null}
      {/* The section renders only its named slots, so the picker lives in
          the body next to the list it changes. */}
      <Page.Section.Body>
        {body}
        {pickerPlugin && canReadAllSkills && canWritePlugin && (
          <SkillPickerDialog
            open
            onOpenChange={(open) => {
              if (!open) setPickerPluginId(null);
            }}
            excludedSkillIds={distributions
              .filter(
                (distribution) => distribution.pluginId === pickerPlugin.id,
              )
              .map((distribution) => distribution.skillId)}
            target={{ pluginId: pickerPlugin.id }}
            title="Add skills"
            description={`Serve project skills to agents connected to ${toolset.name} through ${pickerPlugin.name}.`}
            actionLabel="Add"
            emptyMessage="No skills available to add. Record a skill in this project first."
            onBatchComplete={handleAddComplete}
          />
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * The one add control. With one plugin it opens the picker; with several it
 * first asks which plugin carries the skill, since that decides who gets it.
 */
function AddServedSkillButton({
  plugins,
  disabled,
  onPick,
}: {
  plugins: Plugin[];
  disabled: boolean;
  onPick: (pluginId: string) => void;
}): JSX.Element {
  const [only] = plugins;
  if (plugins.length === 1 && only) {
    return (
      <Button size="sm" disabled={disabled} onClick={() => onPick(only.id)}>
        <Button.LeftIcon>
          <Icon name="plus" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Add skill</Button.Text>
      </Button>
    );
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled}>
        <Button size="sm" disabled={disabled}>
          <Button.LeftIcon>
            <Icon name="plus" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Add skill</Button.Text>
          <Button.RightIcon>
            <ChevronDown className="h-4 w-4" />
          </Button.RightIcon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {plugins.map((plugin) => (
          <DropdownMenuItem key={plugin.id} onSelect={() => onPick(plugin.id)}>
            Add to {plugin.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The plugin a group of skills comes from, with a nudge when nobody receives them. */
function ServedPluginHeader({
  group,
}: {
  group: ServedSkillGroup<PluginSkillDistribution, Plugin>;
}): JSX.Element {
  const routes = useRoutes();
  const { plugin, items } = group;
  const unassigned = plugin.assignmentCount === 0;
  return (
    <div className="border-border flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b px-4 py-2">
      <div className="flex min-w-0 items-center gap-2">
        <Link
          to={routes.plugins.detail.href(plugin.id)}
          className="truncate text-sm font-medium hover:underline"
        >
          {plugin.name}
        </Link>
        <Text small muted className="shrink-0">
          {items.length === 1 ? "1 skill" : `${items.length} skills`}
        </Text>
      </div>
      {unassigned && (
        <Text small className="text-warning">
          Not assigned to anyone yet, so no agent receives these skills.{" "}
          <Link
            to={routes.plugins.detail.href(plugin.id)}
            className="underline"
          >
            Assign the plugin
          </Link>
        </Text>
      )}
    </div>
  );
}

function listNames(plugins: Plugin[]): string {
  const names = plugins.map((plugin) => plugin.name);
  if (names.length <= 2) return names.join(" or ");
  return `${names.slice(0, -1).join(", ")} or ${names[names.length - 1]}`;
}

function servedSkillColumns({
  canRemove,
  isRemoving,
  onRemove,
}: {
  canRemove: boolean;
  isRemoving: boolean;
  onRemove: (distribution: PluginSkillDistribution) => void;
}): Column<PluginSkillDistribution>[] {
  const columns: Column<PluginSkillDistribution>[] = [
    {
      key: "skill",
      header: "Skill",
      width: "1fr",
      render: (distribution) => <ServedSkillCell distribution={distribution} />,
    },
    {
      key: "version",
      header: "Version",
      width: "110px",
      render: (distribution) => (
        <SkillDistributionVersionBadge
          pinnedVersionId={distribution.pinnedVersionId}
        />
      ),
    },
    {
      key: "lastLoaded",
      header: "Last loaded over MCP",
      width: "190px",
      render: (distribution) => (
        <LastLoadedCell loadedAt={distribution.lastLoadedOverMcpAt} />
      ),
    },
  ];
  if (canRemove) {
    columns.push({
      key: "actions",
      header: "",
      width: "64px",
      render: (distribution) => (
        <div className="flex justify-end">
          <Button
            type="button"
            variant="tertiary"
            size="sm"
            tooltip="Remove from plugin"
            aria-label={`Remove ${distribution.skillDisplayName} from ${distribution.pluginName}`}
            className="hover:text-destructive"
            disabled={isRemoving}
            onClick={() => onRemove(distribution)}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ),
    });
  }
  return columns;
}

function ServedSkillCell({
  distribution,
}: {
  distribution: PluginSkillDistribution;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <div className="min-w-0">
      <Link
        to={routes.skills.detail.href(distribution.skillId)}
        className="block truncate text-sm font-medium hover:underline"
        title={distribution.skillDisplayName}
      >
        {distribution.skillDisplayName}
      </Link>
      <Text small muted className="block truncate font-mono text-xs">
        {distribution.skillName}
      </Text>
    </div>
  );
}

function LastLoadedCell({ loadedAt }: { loadedAt?: Date }): JSX.Element {
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
