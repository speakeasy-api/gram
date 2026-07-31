import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { MCPServerTableRow } from "@/components/mcp/MCPServerTableRow";
import { MCPTableRow, MCPTableRowSkeleton } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/DotTable";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  indexMcpActivity,
  lookupMcpActivity,
  mcpActivityStatus,
  type McpActivityStatus,
  type McpActivityTargetType,
} from "@/components/mcp/mcp-activity";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/useToolsets";
import { MCPEmptyState } from "./MCPEmptyState";
import {
  useFilterState as useMcpDimensionFilters,
  type FilterValue,
} from "@/components/filters";
import {
  hasActiveMcpFilters,
  matchesMcpFilters,
  mcpServerFacets,
  MCP_FILTERS,
  MCP_FILTER_OPTIONS,
  pluginFilterOptions,
  pluginMembership,
  type PluginMembership,
} from "./mcp-filter-schema";
import { usePlugins } from "@gram/client/react-query/plugins.js";

const BUILT_IN_SERVERS = [
  {
    name: "MCP Logs",
    description:
      "Search and analyze your project's MCP server logs, tool calls, and agent sessions.",
    slug: "logs",
  },
];

// A tunnelled mcp_servers row attributes its telemetry as "tunneled_mcp_server";
// a remote-backed one attributes as "hosted_mcp_server" (same as hosted
// toolsets). Deriving the type here lets the activity lookup disambiguate a
// tunnelled server from a hosted toolset that happens to share its slug.
function mcpServerTargetType(server: {
  tunneledMcpServerId?: string;
}): McpActivityTargetType {
  return server.tunneledMcpServerId
    ? "tunneled_mcp_server"
    : "hosted_mcp_server";
}

// The listing renders mcp_servers rows only. A row is toolset-backed when the
// server's `toolsetId` resolves to a toolset entry, which then supplies the
// hosted card's data (name, tools, origin); every other row renders through
// the mcp_servers card.
type McpListingRow =
  | { kind: "toolset"; server: McpServer; toolset: ToolsetEntry }
  | { kind: "server"; server: McpServer };

function rowDisplayName(row: McpListingRow): string {
  return row.kind === "toolset" ? row.toolset.name : (row.server.name ?? "");
}

function rowMatchesQuery(row: McpListingRow, query: string): boolean {
  const haystack =
    row.kind === "toolset"
      ? [row.toolset.name, row.toolset.slug, row.server.slug]
      : [row.server.name, row.server.slug];
  return haystack.some((value) => value?.toLowerCase().includes(query));
}

function rowFacets(row: McpListingRow, membership: PluginMembership) {
  return mcpServerFacets(
    row.server,
    membership,
    row.kind === "toolset" ? row.toolset : undefined,
  );
}

// Telemetry still attributes toolset-backed servers by their toolset slug;
// remote/tunnelled servers attribute by the mcp_servers slug.
function rowActivityTarget(row: McpListingRow): {
  targetType: McpActivityTargetType;
  targetId: string | undefined;
} {
  if (row.kind === "toolset") {
    return { targetType: "hosted_mcp_server", targetId: row.toolset.slug };
  }
  return {
    targetType: mcpServerTargetType(row.server),
    targetId: row.server.slug,
  };
}

function McpListingCard({
  row,
  endpointCount,
  activityStatus,
  recentWindowDays,
}: {
  row: McpListingRow;
  endpointCount: number;
  activityStatus?: McpActivityStatus | null;
  recentWindowDays?: number;
}): JSX.Element {
  if (row.kind === "toolset") {
    return (
      <MCPCard
        toolset={row.toolset}
        activityStatus={activityStatus}
        recentWindowDays={recentWindowDays}
      />
    );
  }
  return (
    <MCPServerCard
      server={row.server}
      endpointCount={endpointCount}
      activityStatus={activityStatus}
      recentWindowDays={recentWindowDays}
    />
  );
}

function McpListingTableRow({
  row,
  endpointCount,
  activityStatus,
  recentWindowDays,
}: {
  row: McpListingRow;
  endpointCount: number;
  activityStatus?: McpActivityStatus | null;
  recentWindowDays?: number;
}): JSX.Element {
  if (row.kind === "toolset") {
    return (
      <MCPTableRow
        toolset={row.toolset}
        activityStatus={activityStatus}
        recentWindowDays={recentWindowDays}
      />
    );
  }
  return (
    <MCPServerTableRow
      server={row.server}
      endpointCount={endpointCount}
      activityStatus={activityStatus}
      recentWindowDays={recentWindowDays}
    />
  );
}

export function MCPRoot(): JSX.Element {
  return <Outlet />;
}

export const MCPPage = (): JSX.Element => {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["mcp:read", "mcp:write"]} level="page">
          <MCPOverview />
        </RequireScope>
      </Page.Body>
    </Page>
  );
};

function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const client = useSdkClient();

  // mcp_servers is the single listing source: every published toolset has a
  // wrapper mcp_servers row, so the grid renders mcp_servers only and the
  // useToolsets() fetch above hydrates toolset-backed rows with hosted card
  // data instead of contributing rows of its own.
  // These listing fetches are non-critical: degrade to the last good (or empty)
  // data with an inline indicator instead of throwing to the page error
  // boundary and replacing the whole screen. Key them by project so a tolerated
  // failure can't leave another project's rows on screen after a switch.
  const gramProject = useProjectSlugForRequests();
  const {
    data: mcpServersResult,
    isLoading: isLoadingMcpServers,
    isFetching: isFetchingMcpServers,
    isError: isMcpServersError,
    refetch: refetchMcpServers,
  } = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const {
    data: endpointsResult,
    isLoading: isLoadingEndpoints,
    isFetching: isFetchingEndpoints,
    isError: isEndpointsError,
    refetch: refetchEndpoints,
  } = useMcpEndpoints({ gramProject }, undefined, {
    throwOnError: false,
  });
  // Plugin membership only drives the "Included in plugins" filter, so a failed
  // fetch degrades to an empty option list rather than breaking the listing.
  const { data: pluginsResult, refetch: refetchPlugins } = usePlugins(
    undefined,
    undefined,
    { throwOnError: false },
  );
  // Per-server tool-call activity powers the subtle "never used" / "no recent
  // calls" markers. It's purely decorative: the backend 404s when observability
  // is disabled for the org, so a failed or absent fetch simply hides the
  // markers rather than degrading the listing.
  const {
    data: activityResult,
    isError: isActivityError,
    isFetching: isFetchingActivity,
    refetch: refetchActivity,
  } = useGetMcpServerActivity(
    { gramProject, getMcpServerActivityPayload: {} },
    undefined,
    { throwOnError: false },
  );
  const activityByTarget = useMemo(
    () => indexMcpActivity(activityResult?.activity),
    [activityResult],
  );
  const recentWindowDays = activityResult?.recentWindowDays ?? 14;
  // Resolve a card's activity marker. Returns undefined (hide the marker) when
  // the activity fetch hasn't resolved or errored (react-query keeps the last
  // good `data` on error, so we must also gate on isError to avoid showing stale
  // markers after observability is disabled), or when the server has no
  // matchable identifier. We only flag a server once we can confirm its state.
  const activityStatusFor = (
    targetType: McpActivityTargetType,
    targetId: string | undefined,
  ) => {
    if (isActivityError || !activityResult || !targetId) return undefined;
    return mcpActivityStatus(
      lookupMcpActivity(activityByTarget, targetType, targetId),
    );
  };
  const rowActivityStatus = (row: McpListingRow) => {
    const { targetType, targetId } = rowActivityTarget(row);
    return activityStatusFor(targetType, targetId);
  };
  const handleRefresh = () => {
    void toolsets.refetch();
    void refetchMcpServers();
    void refetchEndpoints();
    void refetchPlugins();
    void refetchActivity();
  };
  const isRefreshing =
    isFetchingMcpServers ||
    isFetchingEndpoints ||
    isFetchingActivity ||
    toolsets.isFetching;
  const listingRows = useMemo<McpListingRow[]>(() => {
    const toolsetById = new Map(toolsets.map((t) => [t.id, t] as const));
    return (mcpServersResult?.mcpServers ?? []).map((server) => {
      const toolset = server.toolsetId
        ? toolsetById.get(server.toolsetId)
        : undefined;
      return toolset
        ? { kind: "toolset", server, toolset }
        : { kind: "server", server };
    });
  }, [mcpServersResult, toolsets]);
  const endpointCountByServerId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      counts.set(
        endpoint.mcpServerId,
        (counts.get(endpoint.mcpServerId) ?? 0) + 1,
      );
    }
    return counts;
  }, [endpointsResult]);

  const isLoading =
    toolsets.isLoading || isLoadingMcpServers || isLoadingEndpoints;

  const hasRefreshError =
    toolsets.isError || isMcpServersError || isEndpointsError;

  const [viewMode, setViewMode] = useViewMode();
  const [newMcpDialogOpen, setNewMcpDialogOpen] = useState(false);
  const [newMcpServerName, setNewMcpServerName] = useState("");
  const [search, setSearch] = useState("");
  const mcpFilters = useMcpDimensionFilters(MCP_FILTERS);

  const plugins = useMemo(() => pluginsResult?.plugins ?? [], [pluginsResult]);
  const membership = useMemo(() => pluginMembership(plugins), [plugins]);
  const filterOptions = useMemo(
    () => ({ ...MCP_FILTER_OPTIONS, plugins: pluginFilterOptions(plugins) }),
    [plugins],
  );

  const filteredRows = useMemo(() => {
    const query = search.toLowerCase();
    return [...listingRows]
      .filter((row) => {
        if (!matchesMcpFilters(rowFacets(row, membership), mcpFilters.values))
          return false;
        if (!query) return true;
        return rowMatchesQuery(row, query);
      })
      .sort((a, b) => rowDisplayName(a).localeCompare(rowDisplayName(b)));
  }, [listingRows, search, mcpFilters.values, membership]);

  // Show the filter bar once there's anything to filter. Filters can drive the
  // result set to empty on their own, so the no-matches state must consider an
  // active filter, not just a search query.
  const hasItems = listingRows.length > 0;
  const showFilters = !isLoading && hasItems;
  const showNoMatches =
    !isLoading &&
    (search !== "" || hasActiveMcpFilters(mcpFilters.values)) &&
    filteredRows.length === 0;

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);

    routes.mcp.details.tools.goTo(result.slug);
  };

  const newMcpServerButton = (
    <RequireScope scope="mcp:write" level="component">
      <Button size="sm" onClick={() => setNewMcpDialogOpen(true)}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>New MCP Server</Button.Text>
      </Button>
    </RequireScope>
  );

  const refreshErrorIndicator = (
    <SimpleTooltip tooltip="We couldn't reach the server to refresh this list. Showing the most recently loaded data.">
      <Badge variant="warning">
        <Badge.LeftIcon>
          <Icon name="triangle-alert" className="inline-block" />
        </Badge.LeftIcon>
        <Badge.Text>Couldn&apos;t refresh</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );

  const newMcpServerDialog = (
    <InputDialog
      open={newMcpDialogOpen}
      onOpenChange={setNewMcpDialogOpen}
      title="Create MCP Server"
      description={`Create a new MCP server`}
      submitButtonText="Create"
      inputs={{
        label: "MCP server name",
        placeholder: "My MCP Server",
        value: newMcpServerName,
        onChange: setNewMcpServerName,
        onSubmit: () => void handleCreateMcpServerSubmit(),
        validate: (value) => value.length > 0 && value.length <= 40,
        hint: (value) => (
          <div className="flex w-full justify-between">
            <p className="text-destructive">
              {value.length > 40 && "Must be 40 characters or less"}
            </p>
            <p>{value.length}/40</p>
          </div>
        ),
      }}
    />
  );

  const builtInSection = (
    <Page.Section>
      <Page.Section.Title>Built-in MCP Servers</Page.Section.Title>
      <Page.Section.Description>
        Pre-configured MCP servers provided by the platform for your project.
        Connect from Claude Desktop, Cursor, or any MCP client.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {BUILT_IN_SERVERS.map((server) => (
            <BuiltInMCPCard key={server.slug} {...server} />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );

  if (!isLoading && !hasRefreshError && listingRows.length === 0) {
    return (
      <>
        <MCPEmptyState cta={newMcpServerButton} />
        {builtInSection}
        {newMcpServerDialog}
      </>
    );
  }

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Hosted MCP Servers</Page.Section.Title>
        {hasRefreshError ? (
          <Page.Section.CTA>{refreshErrorIndicator}</Page.Section.CTA>
        ) : null}
        <Page.Section.CTA>{newMcpServerButton}</Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Sources exposed as MCP servers. These include all types of sources
          such as OpenAPI, functions, third-party servers from the catalog, and
          custom remote MCPs imported by URL.
        </Page.Section.Description>
        <Page.Section.Body>
          {showFilters && (
            <Page.Toolbar className="mb-4">
              <Page.Toolbar.Search
                value={search}
                onChange={setSearch}
                placeholder="Search MCP servers..."
              />
              <Page.Toolbar.Filters
                schema={MCP_FILTERS}
                values={mcpFilters.values}
                optionsById={filterOptions}
                onChange={
                  mcpFilters.setValue as (
                    id: string,
                    value: FilterValue,
                  ) => void
                }
                onClear={mcpFilters.clearValue as (id: string) => void}
                onClearAll={mcpFilters.clearAll}
              />
              <Page.Toolbar.ViewAs value={viewMode} onChange={setViewMode} />
              <Page.Toolbar.Refresh
                onRefresh={handleRefresh}
                isRefreshing={isRefreshing}
              />
            </Page.Toolbar>
          )}
          {showNoMatches ? (
            <Text muted className="py-8 text-center">
              {search !== ""
                ? `No MCP servers matching “${search}”`
                : "No MCP servers match your filters"}
            </Text>
          ) : viewMode === "grid" ? (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {isLoading ? (
                <>
                  <MCPCardSkeleton />
                  <MCPCardSkeleton />
                </>
              ) : (
                filteredRows.map((row) => (
                  <McpListingCard
                    key={row.server.id}
                    row={row}
                    endpointCount={
                      endpointCountByServerId.get(row.server.id) ?? 0
                    }
                    activityStatus={rowActivityStatus(row)}
                    recentWindowDays={recentWindowDays}
                  />
                ))
              )}
            </div>
          ) : (
            <DotTable
              headers={[
                { label: "Name" },
                { label: "Visibility" },
                { label: "URL" },
                { label: "Tools" },
              ]}
            >
              {isLoading ? (
                <>
                  <MCPTableRowSkeleton />
                  <MCPTableRowSkeleton />
                </>
              ) : (
                filteredRows.map((row) => (
                  <McpListingTableRow
                    key={row.server.id}
                    row={row}
                    endpointCount={
                      endpointCountByServerId.get(row.server.id) ?? 0
                    }
                    activityStatus={rowActivityStatus(row)}
                    recentWindowDays={recentWindowDays}
                  />
                ))
              )}
            </DotTable>
          )}
        </Page.Section.Body>
      </Page.Section>
      {builtInSection}
      {newMcpServerDialog}
    </>
  );
}
