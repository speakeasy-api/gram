import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { GatewayCard } from "@/components/mcp/GatewayCard";
import { GatewayTableRow } from "@/components/mcp/GatewayTableRow";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { MCPServerTableRow } from "@/components/mcp/MCPServerTableRow";
import { MCPTableRow, MCPTableRowSkeleton } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/DotTable";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useViewMode } from "@/components/ui/ViewToggle/use-view-mode";
import {
  useProjectSlugForRequests,
  useSdkClient,
  useSlugs,
} from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { createDefaultGatewayEndpoint } from "@/lib/mcpEndpoints";
import { getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { useGetMcpServerActivity } from "@gram/client/react-query/getMcpServerActivity.js";
import {
  invalidateAllMcpEndpoints,
  useMcpEndpoints,
} from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import {
  invalidateAllMetaMcpServers,
  useMetaMcpServers,
} from "@gram/client/react-query/metaMcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  indexMcpActivity,
  lookupMcpActivity,
  mcpActivityStatus,
  type McpActivityTargetType,
} from "@/components/mcp/mcp-activity";
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

// A tunnelled mcp_servers row attributes its telemetry as "tunneled_mcp_server";
// a remote-backed one attributes as "hosted_mcp_server" (same as hosted
// toolsets). Deriving the type here lets the activity lookup disambiguate a
// tunnelled server from a hosted toolset that happens to share its slug.
// Unproxied servers have no matcher in this mechanism at all (the backend
// correlates their usage separately, by canonical URL, via the dedicated
// unproxied usage endpoints), so this returns undefined and callers skip the
// lookup instead of misclassifying them as hosted.
function mcpServerTargetType(server: {
  tunneledMcpServerId?: string;
  unproxiedMcpServerId?: string;
}): McpActivityTargetType | undefined {
  if (server.unproxiedMcpServerId) {
    return undefined;
  }
  return server.tunneledMcpServerId
    ? "tunneled_mcp_server"
    : "hosted_mcp_server";
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
  const queryClient = useQueryClient();
  const { orgSlug } = useSlugs();

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
  const {
    data: endpointsResult,
    isLoading: isLoadingEndpoints,
    isFetching: isFetchingEndpoints,
    isError: isEndpointsError,
    refetch: refetchEndpoints,
  } = useMcpEndpoints({ gramProject }, undefined, {
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
    targetType: McpActivityTargetType | undefined,
    targetId: string | undefined,
  ) => {
    if (isActivityError || !activityResult || !targetId || !targetType) {
      return undefined;
    }
    return mcpActivityStatus(
      lookupMcpActivity(activityByTarget, targetType, targetId),
    );
  };
  const handleRefresh = () => {
    void toolsets.refetch();
    void refetchMcpServers();
    void refetchEndpoints();
    void refetchPlugins();
    void refetchActivity();
    if (gatewaysEnabled) void refetchGateways();
  };
  const isRefreshing =
    isFetchingMcpServers ||
    isFetchingEndpoints ||
    isFetchingActivity ||
    isFetchingGateways ||
    toolsets.isFetching;
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
  const endpointCountByServerId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      // Meta-MCP-backed endpoints have no generic server to count against.
      if (!endpoint.mcpServerId) continue;
      counts.set(
        endpoint.mcpServerId,
        (counts.get(endpoint.mcpServerId) ?? 0) + 1,
      );
    }
    return counts;
  }, [endpointsResult]);
  // Platform address per gateway, reusing the endpoints this page already
  // loads. Custom-domain endpoints are skipped: their host isn't known here.
  const gatewayUrlById = useMemo(() => {
    const urls = new Map<string, string>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      if (!endpoint.metaMcpServerId || endpoint.customDomainId) continue;
      if (urls.has(endpoint.metaMcpServerId)) continue;
      urls.set(
        endpoint.metaMcpServerId,
        `${getServerURL()}/mcp/${endpoint.slug}`,
      );
    }
    return urls;
  }, [endpointsResult]);
  const endpointCountByGatewayId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      if (!endpoint.metaMcpServerId) continue;
      counts.set(
        endpoint.metaMcpServerId,
        (counts.get(endpoint.metaMcpServerId) ?? 0) + 1,
      );
    }
    return counts;
  }, [endpointsResult]);

  const isLoading =
    toolsets.isLoading ||
    isLoadingMcpServers ||
    isLoadingEndpoints ||
    isLoadingGateways;

  const hasRefreshError =
    toolsets.isError ||
    isMcpServersError ||
    isEndpointsError ||
    isGatewaysError;

  const [viewMode, setViewMode] = useViewMode();
  const [newMcpDialogOpen, setNewMcpDialogOpen] = useState(false);
  const [newMcpServerName, setNewMcpServerName] = useState("");
  const [newGatewayDialogOpen, setNewGatewayDialogOpen] = useState(false);
  const [newGatewayName, setNewGatewayName] = useState("");
  const [search, setSearch] = useState("");
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
  const showFilters = !isLoading && hasItems;
  const showNoMatches =
    !isLoading &&
    (search !== "" || hasActiveMcpFilters(mcpFilters.values)) &&
    filteredToolsets.length === 0 &&
    filteredMcpServers.length === 0 &&
    filteredGateways.length === 0;

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);

    routes.mcp.details.tools.goTo(result.slug);
  };

  // Creation is two calls: the gateway, then its default address. The address
  // is best-effort (createDefaultGatewayEndpoint warns rather than throwing),
  // so a failure there still lands the user on a usable gateway page.
  const handleCreateGatewaySubmit = async () => {
    const gateway = await client.metaMcp.create({
      createMetaMcpServerForm: { name: newGatewayName },
    });

    if (orgSlug) {
      await createDefaultGatewayEndpoint(
        client,
        gateway.id,
        gateway.name,
        orgSlug,
      );
    }

    await Promise.all([
      invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
      invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
    ]);
    toast.success(`Gateway "${gateway.name}" created`);
    routes.mcp.gateway.members.goTo(gateway.id);
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

  const newGatewayButton = gatewaysEnabled ? (
    <RequireScope scope="mcp:write" level="component">
      <Button
        size="sm"
        variant="secondary"
        onClick={() => setNewGatewayDialogOpen(true)}
      >
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>New Gateway</Button.Text>
      </Button>
    </RequireScope>
  ) : null;

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

  const newGatewayDialog = (
    <InputDialog
      open={newGatewayDialogOpen}
      onOpenChange={setNewGatewayDialogOpen}
      title="Create Gateway"
      description="One MCP endpoint fronting a set of MCP servers. Add members after creating it."
      submitButtonText="Create"
      inputs={{
        label: "Gateway name",
        placeholder: "My Gateway",
        value: newGatewayName,
        onChange: setNewGatewayName,
        onSubmit: () => void handleCreateGatewaySubmit(),
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
        {newMcpServerDialog}
        {newGatewayDialog}
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
        <Page.Section.CTA>{newGatewayButton}</Page.Section.CTA>
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
                <>
                  {filteredGateways.map((gateway) => (
                    <GatewayCard
                      key={gateway.id}
                      gateway={gateway}
                      url={gatewayUrlById.get(gateway.id)}
                    />
                  ))}
                  {filteredToolsets.map((toolset) => (
                    <MCPCard
                      key={toolset.id}
                      toolset={toolset}
                      activityStatus={activityStatusFor(
                        "hosted_mcp_server",
                        toolset.slug,
                      )}
                      recentWindowDays={recentWindowDays}
                    />
                  ))}
                  {filteredMcpServers.map((server) => (
                    <MCPServerCard
                      key={server.id}
                      server={server}
                      endpointCount={
                        endpointCountByServerId.get(server.id) ?? 0
                      }
                      activityStatus={activityStatusFor(
                        mcpServerTargetType(server),
                        server.slug,
                      )}
                      recentWindowDays={recentWindowDays}
                    />
                  ))}
                </>
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
                <>
                  {filteredGateways.map((gateway) => (
                    <GatewayTableRow
                      key={gateway.id}
                      gateway={gateway}
                      endpointCount={
                        endpointCountByGatewayId.get(gateway.id) ?? 0
                      }
                      url={gatewayUrlById.get(gateway.id)}
                    />
                  ))}
                  {filteredToolsets.map((toolset) => (
                    <MCPTableRow
                      key={toolset.id}
                      toolset={toolset}
                      activityStatus={activityStatusFor(
                        "hosted_mcp_server",
                        toolset.slug,
                      )}
                      recentWindowDays={recentWindowDays}
                    />
                  ))}
                  {filteredMcpServers.map((server) => (
                    <MCPServerTableRow
                      key={server.id}
                      server={server}
                      endpointCount={
                        endpointCountByServerId.get(server.id) ?? 0
                      }
                      activityStatus={activityStatusFor(
                        mcpServerTargetType(server),
                        server.slug,
                      )}
                      recentWindowDays={recentWindowDays}
                    />
                  ))}
                </>
              )}
            </DotTable>
          )}
        </Page.Section.Body>
      </Page.Section>
      {builtInSection}
      {newMcpServerDialog}
      {newGatewayDialog}
    </>
  );
}
