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
import { Navigate, useLocation, useParams } from "react-router";
import {
  serverTabForToolsetTab,
  toolsetTabSegment,
} from "./toolsetRedirectRouting";
import {
  mcpServerTabHref,
  standsInForToolset,
} from "./x/MCPServerDetailsRouting";

/** Old `/mcp/<toolsetSlug>/...` links open the matching tab of the toolset's MCP server page. */
export function ToolsetRedirectPage(): React.JSX.Element | null {
  const { toolsetSlug = "" } = useParams<{ toolsetSlug: string }>();
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

  if (!toolsetSlug || isNotFoundError(toolsetError)) {
    return <ToolsetNotFound title="MCP server not found" />;
  }
  if (toolsetError) throw toolsetError;
  if (serversError) throw serversError;
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
        description="No MCP server in this project serves this toolset. It may only be reachable through a gateway."
      />
    );
  }

  const target = serverTabForToolsetTab(
    toolsetTabSegment(location.pathname, toolsetSlug),
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
