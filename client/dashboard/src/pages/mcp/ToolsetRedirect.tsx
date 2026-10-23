import { Page } from "@/components/page-layout";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useToolset } from "@/hooks/toolTypes";
import { isNotFoundError } from "@/lib/route-errors";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Navigate, useLocation } from "react-router";
import { serverTabForToolsetTab } from "./toolsetRedirectRouting";
import {
  mcpServerTabHref,
  standsInForToolset,
  tabSegmentFromPath,
  withoutMcpXSegment,
} from "./x/MCPServerDetailsRouting";

/**
 * How `/mcp/<param>[/<tab>]` resolves once `param` is not a server slug (or
 * id) in this project. MCPServerDetails tries that first, so a server always
 * wins over a toolset with the same slug. Then, in order:
 *
 * 1. `param` is a toolset slug: open the matching tab of the server hosting
 *    that toolset (old `/mcp/<toolsetSlug>/...` bookmarks).
 * 2. `param` is `x`: the path is a retired `/mcp/x/<slug>` link whose slug
 *    happens to be a tab name, so drop the `x` and resolve again.
 * 3. Otherwise: not found.
 */
export function ToolsetRedirectPage({
  toolsetSlug,
}: {
  toolsetSlug: string;
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
    toolsetSlug === "x" ? withoutMcpXSegment(location.pathname) : undefined;
  if (legacyXTarget && isNotFoundError(toolsetError)) {
    return (
      <Navigate
        to={`${legacyXTarget}${location.search}${location.hash}`}
        replace
      />
    );
  }
  if (!toolsetSlug || isNotFoundError(toolsetError)) {
    return <ToolsetNotFound title="MCP server not found" />;
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
      <ToolsetNotFound
        title={`${toolset.name} has no MCP server`}
        description="No MCP server in this project serves these tools. They may only be reachable through a gateway."
      />
    );
  }

  // The server lookup already missed this slug; redirecting to it would loop.
  if (mcpServerRouteParam(host) === toolsetSlug) {
    return <ToolsetNotFound title="MCP server not found" />;
  }

  const target = serverTabForToolsetTab(
    tabSegmentFromPath(location.pathname, toolsetSlug),
    location.hash,
  );
  return (
    <Navigate
      to={`${mcpServerTabHref(routes, mcpServerRouteParam(host), target.tab)}${location.search}${target.hash}`}
      replace
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
