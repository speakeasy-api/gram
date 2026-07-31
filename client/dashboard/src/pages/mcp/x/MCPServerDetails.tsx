import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useToolset } from "@/hooks/toolTypes";
import { getMcpServerArgs } from "@/lib/sources";
import { MCPAuthenticationTab } from "@/pages/mcp/MCPEnvironmentSettings";
import { MCPPerformanceTab } from "@/pages/mcp/MCPPerformanceTab";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Stack } from "@/components/ui/Stack";
import { Navigate, useLocation, useParams } from "react-router";
import { MCPTeamAccessTab } from "../MCPTeamAccessTab";
import {
  activeTabFromPath,
  initialTabFromHash,
  MCP_SERVER_TAB_URLS,
  mcpServerTabHref,
  resolveTabForBackend,
  type TabValue,
} from "./MCPServerDetailsRouting";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import { InspectTab } from "./tabs/InspectTab";
import { MCP_AUTHENTICATION_SECTION_ID } from "./tabs/settings/sections/authentication/AuthenticationSection";
import { SettingsTab } from "./tabs/settings/SettingsTab";
import { ToolsetPromptsTab } from "./tabs/toolset/ToolsetPromptsTab";
import { ToolsetResourcesTab } from "./tabs/toolset/ToolsetResourcesTab";
import { ToolsetToolsTab } from "./tabs/toolset/ToolsetToolsTab";

function isToolsetBackedServer(server: McpServer): boolean {
  return server.backendKind === "toolset";
}

// Placeholder while the backing toolset (or the server itself) loads: rough
// shape of a details tab, only visible for a brief flash.
function DetailsTabLoading() {
  return (
    <Stack gap={6} className="mb-4">
      <div className="bg-muted/30 h-40 w-full animate-pulse rounded-xl" />
      <div className="bg-muted/30 h-64 w-full animate-pulse rounded-lg" />
      <div className="bg-muted/30 h-48 w-full animate-pulse rounded-lg" />
    </Stack>
  );
}

export default function MCPServerDetails(): JSX.Element {
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const idOrSlug = mcpServerSlug ?? "";

  const {
    data: mcpServer,
    isLoading,
    isError,
  } = useGetMcpServer(getMcpServerArgs(idOrSlug), undefined, {
    enabled: idOrSlug !== "",
  });

  const mcpServerId = mcpServer?.id ?? "";
  const isToolsetBacked = !!mcpServer && isToolsetBackedServer(mcpServer);
  const toolsetSlug = mcpServer?.toolsetSummary?.slug;

  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];

  // Toolset-backed servers hydrate their tool bundle for the toolset-owned
  // tabs (tools, resources, prompts, authentication, performance).
  const { data: toolset } = useToolset(
    isToolsetBacked ? toolsetSlug : undefined,
  );

  if (!idOrSlug) {
    return <Navigate to={routes.mcp.href()} replace />;
  }
  if (isError || (!isLoading && !mcpServer)) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  const rawTab = activeTabFromPath(location.pathname, idOrSlug);

  // Tab redirects depend on the backend kind, so hold routing decisions until
  // the server record resolves.
  if (!mcpServer) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs
            skipSegments={["x", ...MCP_SERVER_TAB_URLS]}
          />
        </Page.Header>
        <Page.Body fullWidth className="gap-0">
          <div className="mx-auto w-full max-w-[1270px] flex-1">
            <DetailsTabLoading />
          </div>
        </Page.Body>
      </Page>
    );
  }

  if (!rawTab) {
    const initialTab = initialTabFromHash(location.hash, isToolsetBacked);
    const hash =
      location.hash === `#${MCP_AUTHENTICATION_SECTION_ID}` &&
      initialTab === "settings"
        ? `#${MCP_AUTHENTICATION_SECTION_ID}`
        : "";

    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, initialTab)}${hash}`}
        replace
      />
    );
  }

  const resolved = resolveTabForBackend(rawTab, isToolsetBacked);
  if (resolved.tab !== rawTab) {
    const hash = resolved.hash ? `#${resolved.hash}` : "";
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, idOrSlug, resolved.tab)}${hash}`}
        replace
      />
    );
  }
  const activeTab: TabValue = resolved.tab;

  const renderToolsetTab = (
    render: (loaded: NonNullable<typeof toolset>) => React.ReactNode,
  ) => {
    if (!toolset) return <DetailsTabLoading />;
    return render(toolset);
  };

  const renderTabContent = () => {
    switch (activeTab) {
      case "overview":
        // Telemetry and plugin membership for toolset-backed servers still
        // attribute by the backing toolset, so the overview keys off it.
        if (isToolsetBacked && mcpServer.toolsetId && toolsetSlug) {
          return (
            <MCPOverviewTab
              server={{
                kind: "toolset",
                id: mcpServer.toolsetId,
                slug: toolsetSlug,
                name:
                  mcpServer.name ??
                  mcpServer.toolsetSummary?.name ??
                  "MCP Server",
              }}
            />
          );
        }
        return (
          mcpServer.slug && (
            <MCPOverviewTab
              server={{
                kind: "mcp-server",
                id: mcpServer.id,
                slug: mcpServer.slug,
                name: mcpServer.name ?? "MCP Server",
              }}
            />
          )
        );
      case "inspect":
        return (
          <InspectTab
            mcpServer={mcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
      case "tools":
        return renderToolsetTab((loaded) => (
          <ToolsetToolsTab toolset={loaded} />
        ));
      case "resources":
        return renderToolsetTab((loaded) => (
          <ToolsetResourcesTab toolset={loaded} />
        ));
      case "prompts":
        return renderToolsetTab((loaded) => (
          <ToolsetPromptsTab toolset={loaded} />
        ));
      case "authentication":
        return renderToolsetTab((loaded) => (
          <RequireScope scope="mcp:write" level="page">
            <MCPAuthenticationTab
              toolset={loaded}
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
            />
          </RequireScope>
        ));
      case "performance":
        return renderToolsetTab((loaded) => (
          <RequireScope scope="mcp:write" level="page">
            <MCPPerformanceTab toolset={loaded} />
          </RequireScope>
        ));
      case "team-access":
        return (
          <RequireScope scope="org:read" level="page">
            <RequireScope
              scope="mcp:read"
              resourceId={mcpServer.id}
              level="page"
            >
              {/* mcp_servers-backed servers grant under the same `mcp:*`
                  scope kind as toolset-backed ones (see selector.go). The
                  backing toolset's tools are passed when available so
                  toolset-backed servers keep per-tool grant display. */}
              <MCPTeamAccessTab
                resourceId={mcpServer.id}
                tools={isToolsetBacked ? toolset?.tools : undefined}
              />
            </RequireScope>
          </RequireScope>
        );
      case "settings":
        // Hold for the backing toolset so toolset-owned settings (tool
        // filtering, delete cascade) never mount without their write target.
        if (isToolsetBacked) {
          return renderToolsetTab((loaded) => (
            <SettingsTab
              mcpServer={mcpServer}
              endpoints={endpoints}
              isLoadingEndpoints={isLoadingEndpoints}
              backingToolset={loaded}
            />
          ));
        }
        return (
          <SettingsTab
            mcpServer={mcpServer}
            endpoints={endpoints}
            isLoadingEndpoints={isLoadingEndpoints}
          />
        );
    }
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [idOrSlug]:
              mcpServer.name ?? mcpServer.toolsetSummary?.name ?? "MCP Server",
          }}
          skipSegments={[
            "x",
            // skipSegments matches by literal value, not position — if the
            // server's own slug happens to collide with a tab name (e.g. a
            // server slugged "settings"), guard against also skipping the
            // server's own breadcrumb crumb.
            ...MCP_SERVER_TAB_URLS.filter((tab) => tab !== idOrSlug),
          ]}
        />
      </Page.Header>

      <Page.Body fullWidth className="gap-0">
        <div className="mx-auto w-full max-w-[1270px] flex-1">
          {renderTabContent()}
        </div>
      </Page.Body>
    </Page>
  );
}
