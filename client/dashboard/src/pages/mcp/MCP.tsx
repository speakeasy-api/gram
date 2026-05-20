import { InputDialog } from "@/components/input-dialog";
import { RequireScope } from "@/components/require-scope";
import { BuiltInMCPCard } from "@/components/mcp/BuiltInMCPCard";
import { MCPCard, MCPCardSkeleton } from "@/components/mcp/MCPCard";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { MCPServerTableRow } from "@/components/mcp/MCPServerTableRow";
import { MCPTableRow, MCPTableRowSkeleton } from "@/components/mcp/MCPTableRow";
import { Page } from "@/components/page-layout";
import { DotTable } from "@/components/ui/dot-table";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useViewMode } from "@/components/ui/use-view-mode";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import {
  useMcpEndpoints,
  useMcpServers,
} from "@gram/client/react-query/index.js";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { useToolsets } from "../toolsets/useToolsets";
import { MCPEmptyState } from "./MCPEmptyState";

const BUILT_IN_SERVERS = [
  {
    name: "MCP Logs",
    description:
      "Search and analyze your project's MCP server logs, tool calls, and agent sessions.",
    slug: "logs",
  },
];

export function MCPRoot() {
  return <Outlet />;
}

export const MCPPage = () => {
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

export function MCPOverview() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const isRemoteMcpEnabled =
    telemetry.isFeatureEnabled("gram-remote-mcp") ?? false;

  // TODO(AGE-1902): collapse this fetch with useToolsets() once Hosted
  // (toolset-backed) MCP servers also source from mcp_servers. Until then the
  // listing merges two parallel collections — toolsets (Hosted) and
  // mcp_servers (Remote-MCP-backed today) — in the same grid.
  const { data: mcpServersResult, isLoading: isLoadingMcpServers } =
    useMcpServers({}, undefined, { enabled: isRemoteMcpEnabled });
  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({}, undefined, { enabled: isRemoteMcpEnabled });
  // Filter the listing to Remote-MCP-backed rows for now — the AGE-1902
  // cutover will introduce toolset-backed rows that today still render
  // through the existing Hosted MCPCard path via useToolsets().
  const mcpServers = useMemo(
    () =>
      (mcpServersResult?.mcpServers ?? []).filter(
        (server) => !!server.remoteMcpServerId,
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
    toolsets.isLoading ||
    (isRemoteMcpEnabled && (isLoadingMcpServers || isLoadingEndpoints));

  const [viewMode, setViewMode] = useViewMode();
  const [newMcpDialogOpen, setNewMcpDialogOpen] = useState(false);
  const [newMcpServerName, setNewMcpServerName] = useState("");
  const [search, setSearch] = useState("");

  const filteredToolsets = useMemo(() => {
    const query = search.toLowerCase();
    return [...toolsets]
      .filter((toolset) => {
        if (!query) return true;
        return (
          toolset.name.toLowerCase().includes(query) ||
          toolset.slug.toLowerCase().includes(query)
        );
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [toolsets, search]);

  const filteredMcpServers = useMemo(() => {
    const query = search.toLowerCase();
    return [...mcpServers]
      .filter((server) => {
        if (!query) return true;
        return (
          (server.name?.toLowerCase().includes(query) ?? false) ||
          (server.slug?.toLowerCase().includes(query) ?? false)
        );
      })
      .sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
  }, [mcpServers, search]);

  const showSearch = !isLoading && toolsets.length + mcpServers.length > 6;
  const showNoMatches =
    !isLoading &&
    search !== "" &&
    filteredToolsets.length === 0 &&
    filteredMcpServers.length === 0;

  const handleCreateMcpServerSubmit = async () => {
    const result = await client.toolsets.create({
      createToolsetRequestBody: {
        name: newMcpServerName,
      },
    });

    toast.success(`MCP server "${result.name}" created`);

    navigate(routes.mcp.details.href(result.slug) + "#tools");
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
        onSubmit: handleCreateMcpServerSubmit,
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
        Pre-configured MCP servers provided by Gram for your project. Connect
        from Claude Desktop, Cursor, or any MCP client.
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

  if (!isLoading && toolsets.length === 0 && mcpServers.length === 0) {
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
        <Page.Section.CTA>
          <ViewToggle value={viewMode} onChange={setViewMode} />
        </Page.Section.CTA>
        <Page.Section.CTA>{newMcpServerButton}</Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Sources exposed as MCP servers. These include all types of sources
          such as OpenAPI, functions, third-party servers from the catalog, and
          custom remote MCPs imported by URL.
        </Page.Section.Description>
        <Page.Section.Body>
          {showSearch && (
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search MCP servers..."
              className="mb-4"
            />
          )}
          {showNoMatches ? (
            <Type muted className="py-8 text-center">
              No MCP servers matching &ldquo;{search}&rdquo;
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
