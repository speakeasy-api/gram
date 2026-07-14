import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { MCPServerTableRow } from "@/components/mcp/MCPServerTableRow";
import { MCPTableRow, MCPTableRowSkeleton } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/dot-table";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Badge, Button, Icon } from "@speakeasy-api/moonshine";
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
    void refetchEndpoints();
    void refetchPlugins();
  };
  const isRefreshing =
    isFetchingMcpServers || isFetchingEndpoints || toolsets.isFetching;
  // Until AGE-1902 moves hosted rows here, this grid only renders mcp_servers-backed MCPs.
  const mcpServers = useMemo(
    () =>
      (mcpServersResult?.mcpServers ?? []).filter(
        (server) => !!server.remoteMcpServerId || !!server.tunneledMcpServerId,
      ),
    [mcpServersResult],
  );
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

  // Show the filter bar once there's anything to filter. Filters can drive the
  // result set to empty on their own, so the no-matches state must consider an
  // active filter, not just a search query.
  const hasItems = toolsets.length + mcpServers.length > 0;
  const showFilters = !isLoading && hasItems;
  const showNoMatches =
    !isLoading &&
    (search !== "" || hasActiveMcpFilters(mcpFilters.values)) &&
    filteredToolsets.length === 0 &&
    filteredMcpServers.length === 0;

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

  if (
    !isLoading &&
    !hasRefreshError &&
    toolsets.length === 0 &&
    mcpServers.length === 0
  ) {
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
            <Type muted className="py-8 text-center">
              {search !== ""
                ? `No MCP servers matching “${search}”`
                : "No MCP servers match your filters"}
            </Type>
          ) : viewMode === "grid" ? (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {isLoading ? (
                <>
                  <MCPCardSkeleton />
                  <MCPCardSkeleton />
                </>
              ) : (
                <>
                  {filteredToolsets.map((toolset) => (
                    <MCPCard key={toolset.id} toolset={toolset} />
                  ))}
                  {filteredMcpServers.map((server) => (
                    <MCPServerCard
                      key={server.id}
                      server={server}
                      endpointCount={
                        endpointCountByServerId.get(server.id) ?? 0
                      }
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
                  {filteredToolsets.map((toolset) => (
                    <MCPTableRow key={toolset.id} toolset={toolset} />
                  ))}
                  {filteredMcpServers.map((server) => (
                    <MCPServerTableRow
                      key={server.id}
                      server={server}
                      endpointCount={
                        endpointCountByServerId.get(server.id) ?? 0
                      }
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
    </>
  );
}
