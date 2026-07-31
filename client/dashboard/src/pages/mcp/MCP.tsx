import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { mcpServerDisplayName } from "@/components/mcp/mcp-server-listing";
import {
  MCPServerCard,
  MCPServerCardSkeleton,
} from "@/components/mcp/MCPServerCard";
import {
  MCPServerTableRow,
  MCPServerTableRowSkeleton,
} from "@/components/mcp/MCPServerTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/DotTable";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  invalidateAllMcpServers,
  useMcpServers,
} from "@gram/client/react-query/mcpServers.js";
import {
  indexMcpActivity,
  lookupMcpActivity,
  mcpActivityStatus,
  type McpActivityTargetType,
} from "@/components/mcp/mcp-activity";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
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

// Telemetry attributes toolset-backed servers by their toolset slug, a
// tunneled mcp_servers row as "tunneled_mcp_server", and a remote-backed one
// as "hosted_mcp_server" under the mcp_servers slug.
function serverActivityTarget(server: McpServer): {
  targetType: McpActivityTargetType;
  targetId: string | undefined;
} {
  if (server.toolsetSummary) {
    return {
      targetType: "hosted_mcp_server",
      targetId: server.toolsetSummary.slug,
    };
  }
  return {
    targetType: server.tunneledMcpServerId
      ? "tunneled_mcp_server"
      : "hosted_mcp_server",
    targetId: server.slug,
  };
}

function serverMatchesQuery(server: McpServer, query: string): boolean {
  const haystack = [
    server.name,
    server.slug,
    server.toolsetSummary?.name,
    server.toolsetSummary?.slug,
  ];
  return haystack.some((value) => value?.toLowerCase().includes(query));
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

const NO_ENDPOINTS: McpEndpoint[] = [];

function MCPOverview() {
  const routes = useRoutes();
  const client = useSdkClient();
  const queryClient = useQueryClient();

  // mcp_servers is the single listing source: every published toolset has a
  // wrapper mcp_servers row carrying a toolset summary, so the grid renders
  // mcp_servers only.
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
  const serverActivityStatus = (server: McpServer) => {
    const { targetType, targetId } = serverActivityTarget(server);
    if (isActivityError || !activityResult || !targetId) return undefined;
    return mcpActivityStatus(
      lookupMcpActivity(activityByTarget, targetType, targetId),
    );
  };
  const handleRefresh = () => {
    void refetchMcpServers();
    void refetchEndpoints();
    void refetchPlugins();
    void refetchActivity();
  };
  const isRefreshing =
    isFetchingMcpServers || isFetchingEndpoints || isFetchingActivity;
  const servers = useMemo(
    () => mcpServersResult?.mcpServers ?? [],
    [mcpServersResult],
  );
  const endpointsByServerId = useMemo(() => {
    const grouped = new Map<string, McpEndpoint[]>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      const existing = grouped.get(endpoint.mcpServerId);
      if (existing) existing.push(endpoint);
      else grouped.set(endpoint.mcpServerId, [endpoint]);
    }
    return grouped;
  }, [endpointsResult]);

  const isLoading = isLoadingMcpServers || isLoadingEndpoints;

  const hasRefreshError = isMcpServersError || isEndpointsError;

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

  const filteredServers = useMemo(() => {
    const query = search.toLowerCase();
    return [...servers]
      .filter((server) => {
        if (
          !matchesMcpFilters(
            mcpServerFacets(server, membership),
            mcpFilters.values,
          )
        )
          return false;
        if (!query) return true;
        return serverMatchesQuery(server, query);
      })
      .sort((a, b) =>
        mcpServerDisplayName(a).localeCompare(mcpServerDisplayName(b)),
      );
  }, [servers, search, mcpFilters.values, membership]);

  // Show the filter bar once there's anything to filter. Filters can drive the
  // result set to empty on their own, so the no-matches state must consider an
  // active filter, not just a search query.
  const hasItems = servers.length > 0;
  const showFilters = !isLoading && hasItems;
  const showNoMatches =
    !isLoading &&
    (search !== "" || hasActiveMcpFilters(mcpFilters.values)) &&
    filteredServers.length === 0;

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);
    void invalidateAllMcpServers(queryClient, { refetchType: "all" });

    // Creating a toolset also creates its wrapper mcp_servers row; resolve it
    // so navigation lands on the server details page.
    try {
      const listing = await client.mcpServers.list({
        gramProject,
        toolsetId: result.id,
      });
      const wrapper = listing.mcpServers[0];
      if (wrapper) {
        routes.mcp.x.tools.goTo(mcpServerRouteParam(wrapper));
        return;
      }
    } catch {
      // Fall through to the listing below.
    }
    routes.mcp.goTo();
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

  if (!isLoading && !hasRefreshError && servers.length === 0) {
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
                  <MCPServerCardSkeleton />
                  <MCPServerCardSkeleton />
                </>
              ) : (
                filteredServers.map((server) => (
                  <MCPServerCard
                    key={server.id}
                    server={server}
                    endpoints={
                      endpointsByServerId.get(server.id) ?? NO_ENDPOINTS
                    }
                    isLoadingEndpoints={isLoadingEndpoints}
                    activityStatus={serverActivityStatus(server)}
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
                  <MCPServerTableRowSkeleton />
                  <MCPServerTableRowSkeleton />
                </>
              ) : (
                filteredServers.map((server) => (
                  <MCPServerTableRow
                    key={server.id}
                    server={server}
                    endpoints={
                      endpointsByServerId.get(server.id) ?? NO_ENDPOINTS
                    }
                    isLoadingEndpoints={isLoadingEndpoints}
                    activityStatus={serverActivityStatus(server)}
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
