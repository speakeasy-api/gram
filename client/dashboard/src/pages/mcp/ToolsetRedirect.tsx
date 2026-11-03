import { Page } from "@/components/page-layout";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useToolset } from "@/hooks/toolTypes";
import { getHttpStatusCode, isNotFoundError } from "@/lib/route-errors";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Navigate, useLocation, useParams } from "react-router";
import { serverTabForToolsetTab } from "./toolsetRedirectRouting";
import {
  mcpServerTabHref,
  standsInForToolset,
  toolsetRouteTabSegment,
  withoutMcpXSegment,
} from "./x/MCPServerDetailsRouting";

// Precedence for /mcp/<param>: a server slug or id (MCPServerDetails), then a
// toolset slug (its hosting server), then a retired /mcp/x/<slug> link.
export function ToolsetRedirectPage({
  toolsetSlug,
  tabSegment,
  serverLookupMissed = false,
}: {
  toolsetSlug: string;
  tabSegment: string | undefined;
  serverLookupMissed?: boolean;
}): React.JSX.Element | null {
  const location = useLocation();
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();

  const {
    data: toolset,
    error: toolsetError,
    isLoading: isLoadingToolset,
  } = useToolset(toolsetSlug || undefined, { throwOnError: false });
  const {
    data: serversResult,
    error: serversError,
    isLoading: isLoadingServers,
  } = useMcpServers({ gramProject, toolsetId: toolset?.id }, undefined, {
    enabled: toolset !== undefined,
    throwOnError: false,
  });

  const legacyXTarget =
    serverLookupMissed && toolsetSlug === "x"
      ? withoutMcpXSegment(location.pathname)
      : undefined;
  const toLegacyX = legacyXTarget ? (
    <Navigate
      to={`${legacyXTarget}${location.search}${location.hash}`}
      replace
    />
  ) : null;

  const unreadable =
    isNotFoundError(toolsetError) || getHttpStatusCode(toolsetError) === 403;
  if (!toolsetSlug || unreadable) {
    return toLegacyX ?? <ToolsetNotFound title="MCP server not found" />;
  }
  if (toolsetError) throw toolsetError;
  if (serversError) {
    return (
      <ToolsetNotFound
        title="MCP server could not be loaded"
        description="Reload the page to try again."
      />
    );
  }
  if (isLoadingToolset || isLoadingServers || !toolset || !serversResult) {
    return null;
  }

  const host = serversResult.mcpServers.find(
    (server) => server.toolsetId === toolset.id && standsInForToolset(server),
  );
  if (!host) {
    return (
      toLegacyX ?? (
        <ToolsetNotFound
          title={`${toolset.name} has no MCP server`}
          description="No MCP server in this project serves these tools. They may only be reachable through a gateway."
        />
      )
    );
  }
  // The server lookup already missed this slug; redirecting to it would loop.
  if (serverLookupMissed && mcpServerRouteParam(host) === toolsetSlug) {
    return <ToolsetNotFound title="MCP server not found" />;
  }

  const target = serverTabForToolsetTab(tabSegment, location.hash);
  return (
    <Navigate
      to={`${mcpServerTabHref(routes, mcpServerRouteParam(host), target.tab)}${location.search}${target.hash}`}
      replace
    />
  );
}

/** `/toolsets/<toolsetSlug>[/<tab>]`: in-app links that know a toolset, not its server. */
export function ToolsetSlugRedirectPage(): React.JSX.Element | null {
  const { toolsetSlug = "" } = useParams<{ toolsetSlug: string }>();
  const location = useLocation();
  return (
    <ToolsetRedirectPage
      toolsetSlug={toolsetSlug}
      tabSegment={toolsetRouteTabSegment(location.pathname)}
    />
  );
}

function ToolsetNotFound({
  title,
  description = "This MCP server may have been deleted, renamed, or moved out of this project.",
}: {
  title: string;
  description?: string;
}) {
  const routes = useRoutes();
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RouteNotFoundState
          title={title}
          description={description}
          action={
            <routes.mcp.Link>
              <SecondaryRouteAction>Back to MCP servers</SecondaryRouteAction>
            </routes.mcp.Link>
          }
        />
      </Page.Body>
    </Page>
  );
}
