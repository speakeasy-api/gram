import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { GatewayCard } from "@/components/mcp/GatewayCard";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import {
  GatewayTableRow,
  MCPServerTableRow,
  MCPTableRow,
  MCPTableRowSkeleton,
} from "@/components/mcp/mcp-table-rows";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/DotTable";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { useToolsets } from "../toolsets/useToolsets";
import { McpTabs } from "./McpTabs";
import { MCPEmptyState } from "./MCPEmptyState";
import {
  useFilterState as useMcpDimensionFilters,
  type FilterValue,
} from "@/components/filters";
import {
  gatewayFacets,
  hasActiveMcpFilters,
  matchesMcpFilters,
  mcpServerFacets,
  MCP_FILTERS,
  MCP_FILTER_OPTIONS,
  pluginFilterOptions,
  pluginMembership,
  toolsetFacets,
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
          <Page.Section>
            <Page.Section.Title>MCP Servers</Page.Section.Title>
            <Page.Section.Description className="max-w-2xl">
              The servers your agents can reach, what they are built from, and
              the versions those arrive in.
            </Page.Section.Description>
          </Page.Section>
          <McpTabs active="servers" />
          <MCPOverview />
        </RequireScope>
      </Page.Body>
    </Page>
  );
};

function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();

  // TODO(AGE-1902): collapse this fetch with useToolsets() once Hosted
  // (toolset-backed) MCP servers also source from mcp_servers. Until then the
  // listing merges two parallel collections — toolsets (Hosted) and
  // mcp_servers (Remote-MCP-backed today) — in the same grid.
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
  // Gateways (meta MCP servers) are behind a rollout flag: opt-in, so an
  // unresolved flag keeps them hidden. The flag gates discoverability only —
  // the backend enforces mcp:read/mcp:write regardless.
  const gatewayFlag = useFeatureFlag(FEATURE_FLAGS.gatewayEndpoints);
  const gatewaysEnabled = gatewayFlag.status === "enabled";
  const {
    data: gatewaysResult,
    isLoading: isLoadingGateways,
    isFetching: isFetchingGateways,
    isError: isGatewaysError,
    refetch: refetchGateways,
  } = useMetaMcpServers({ gramProject }, undefined, {
    throwOnError: false,
    enabled: gatewaysEnabled,
  });
  // Plugin membership only drives the "Included in plugins" filter, so a failed
  // fetch degrades to an empty option list rather than breaking the listing.
  const { data: pluginsResult, refetch: refetchPlugins } = usePlugins(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const handleRefresh = () => {
    void toolsets.refetch();
    void refetchMcpServers();
    void refetchPlugins();
    if (gatewaysEnabled) void refetchGateways();
  };
  const isRefreshing =
    isFetchingMcpServers || isFetchingGateways || toolsets.isFetching;
  // Until AGE-1902 moves hosted rows here, this grid only renders mcp_servers-backed MCPs.
  const mcpServers = useMemo(
    () =>
      (mcpServersResult?.mcpServers ?? []).filter(
        (server) =>
          !!server.remoteMcpServerId ||
          !!server.tunneledMcpServerId ||
          !!server.unproxiedMcpServerId,
      ),
    [mcpServersResult],
  );
  const gateways = useMemo(
    () => gatewaysResult?.metaMcpServers ?? [],
    [gatewaysResult],
  );

  const isLoading =
    toolsets.isLoading || isLoadingMcpServers || isLoadingGateways;

  const hasRefreshError =
    toolsets.isError || isMcpServersError || isGatewaysError;

  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode();
  const mcpFilters = useMcpDimensionFilters(MCP_FILTERS);

  const plugins = useMemo(() => pluginsResult?.plugins ?? [], [pluginsResult]);
  const membership = useMemo(() => pluginMembership(plugins), [plugins]);
  const filterOptions = useMemo(
    () => ({ ...MCP_FILTER_OPTIONS, plugins: pluginFilterOptions(plugins) }),
    [plugins],
  );

  const filteredToolsets = useMemo(() => {
    const query = search.toLowerCase();
    return [...toolsets]
      .filter((toolset) => {
        if (
          !matchesMcpFilters(
            toolsetFacets(toolset, membership),
            mcpFilters.values,
          )
        )
          return false;
        if (!query) return true;
        return (
          toolset.name.toLowerCase().includes(query) ||
          toolset.slug.toLowerCase().includes(query)
        );
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [toolsets, search, mcpFilters.values, membership]);

  const filteredMcpServers = useMemo(() => {
    const query = search.toLowerCase();
    return [...mcpServers]
      .filter((server) => {
        if (
          !matchesMcpFilters(
            mcpServerFacets(server, membership),
            mcpFilters.values,
          )
        )
          return false;
        if (!query) return true;
        return (
          (server.name?.toLowerCase().includes(query) ?? false) ||
          (server.slug?.toLowerCase().includes(query) ?? false)
        );
      })
      .sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
  }, [mcpServers, search, mcpFilters.values, membership]);

  const filteredGateways = useMemo(() => {
    const query = search.toLowerCase();
    return [...gateways]
      .filter((gateway) => {
        if (!matchesMcpFilters(gatewayFacets(), mcpFilters.values))
          return false;
        if (!query) return true;
        return gateway.name.toLowerCase().includes(query);
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [gateways, search, mcpFilters.values]);

  // Show the filter bar once there's anything to filter. Filters can drive the
  // result set to empty on their own, so the no-matches state must consider an
  // active filter, not just a search query.
  const hasItems = toolsets.length + mcpServers.length + gateways.length > 0;
  // Also shown when the list failed to load: the toolbar carries the retry and
  // "Add new", which are exactly what you need when nothing came back.
  const showFilters = !isLoading && (hasItems || hasRefreshError);
  const showNoMatches =
    !isLoading &&
    (search !== "" || hasActiveMcpFilters(mcpFilters.values)) &&
    filteredToolsets.length === 0 &&
    filteredMcpServers.length === 0 &&
    filteredGateways.length === 0;

  const newMcpServerButton = (
    <RequireScope scope="mcp:write" level="component">
      {/* h-10 matches the toolbar's own controls, which Toolbar.Actions
          leaves to its children to size. */}
      <Button size="sm" className="h-10" onClick={() => routes.mcp.add.goTo()}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>Add new</Button.Text>
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

  const builtInSection = (
    <Page.Section>
      {/* Section heading, not a second page title: no eyebrow, smaller serif. */}
      <Page.Section.Title area="" className="text-display-xs">
        Built-in MCP Servers
      </Page.Section.Title>
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

  if (
    !isLoading &&
    !hasRefreshError &&
    toolsets.length === 0 &&
    mcpServers.length === 0 &&
    gateways.length === 0
  ) {
    return (
      <>
        <MCPEmptyState cta={newMcpServerButton} />
        {builtInSection}
      </>
    );
  }

  return (
    <>
      {/* No section heading or description: the page title above the tabs
          already says what this list is, and repeating it pushed the servers
          themselves below the fold. */}
      {/* Deliberately not a Page.Section: its top margin put this toolbar
          12px below where the sibling tabs put theirs, so switching tabs
          nudged the controls. */}
      <div>
        {showFilters && (
          <Page.Toolbar className="mb-8">
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
                mcpFilters.setValue as (id: string, value: FilterValue) => void
              }
              onClear={mcpFilters.clearValue as (id: string) => void}
              onClearAll={mcpFilters.clearAll}
            />
            <Page.Toolbar.ViewAs value={viewMode} onChange={setViewMode} />
            <Page.Toolbar.Refresh
              onRefresh={handleRefresh}
              isRefreshing={isRefreshing}
            />
            {/* The one thing you come here to do sits with the controls that
                  filter what you are looking at. */}
            <Page.Toolbar.Actions>
              {hasRefreshError ? refreshErrorIndicator : null}
              {newMcpServerButton}
            </Page.Toolbar.Actions>
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
              <>
                {filteredGateways.map((gateway) => (
                  <GatewayCard key={gateway.id} gateway={gateway} />
                ))}
                {filteredToolsets.map((toolset) => (
                  <MCPCard key={toolset.id} toolset={toolset} />
                ))}
                {filteredMcpServers.map((server) => (
                  <MCPServerCard key={server.id} server={server} />
                ))}
              </>
            )}
          </div>
        ) : (
          <DotTable
            headers={[
              { label: "Name" },
              { label: "Kind" },
              { label: "Address" },
              // Kind-dependent: tool count for hosted servers, member count
              // for gateways, nothing yet for mcp_servers-backed rows.
              { label: "Contents" },
            ]}
          >
            {isLoading ? (
              <>
                <MCPTableRowSkeleton />
                <MCPTableRowSkeleton />
              </>
            ) : (
              <>
                {filteredGateways.map((gateway) => (
                  <GatewayTableRow key={gateway.id} gateway={gateway} />
                ))}
                {filteredToolsets.map((toolset) => (
                  <MCPTableRow key={toolset.id} toolset={toolset} />
                ))}
                {filteredMcpServers.map((server) => (
                  <MCPServerTableRow key={server.id} server={server} />
                ))}
              </>
            )}
          </DotTable>
        )}
      </div>
      {builtInSection}
    </>
  );
}
