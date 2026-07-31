import { Page } from "@/components/page-layout";
import {
  RouteNotFoundState,
  SecondaryRouteAction,
} from "@/components/route-not-found-state";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import {
  mcpServerTabHref,
  resolveTabForBackend,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { Stack } from "@/components/ui/Stack";
import { Navigate, useLocation, useParams } from "react-router";
import { activeTabFromPath, initialTabFromHash } from "./MCPDetailsRouting";

// The /mcp/:toolsetSlug detail pages moved to the mcp_servers-backed details
// route (/mcp/x/:serverSlug). This component keeps old bookmarks and in-app
// links working by resolving the toolset slug to its wrapper server via the
// servers list and redirecting to the matching tab.
export default function ToolsetDetailRedirect(): JSX.Element {
  const { toolsetSlug } = useParams<{ toolsetSlug: string }>();
  const location = useLocation();
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();

  const { data, isLoading, isFetching } = useMcpServers(
    { gramProject },
    undefined,
    { throwOnError: false },
  );

  if (!toolsetSlug) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  const server = data?.mcpServers.find(
    (candidate) => candidate.toolsetSummary?.slug === toolsetSlug,
  );

  if (server) {
    const requestedTab =
      activeTabFromPath(location.pathname, toolsetSlug) ??
      initialTabFromHash(location.hash);
    const resolved = resolveTabForBackend(
      requestedTab,
      server.backendKind === "toolset",
    );
    const hash = resolved.hash ? `#${resolved.hash}` : "";
    return (
      <Navigate
        to={`${mcpServerTabHref(routes, mcpServerRouteParam(server), resolved.tab)}${hash}`}
        replace
      />
    );
  }

  // A just-created toolset may not be in a cached listing yet; only conclude
  // "not found" after a fresh fetch has settled.
  if (isLoading || isFetching) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body fullWidth className="gap-0">
          <div className="mx-auto w-full max-w-[1270px] flex-1">
            <Stack gap={6} className="mb-4">
              <div className="bg-muted/30 h-40 w-full animate-pulse rounded-xl" />
              <div className="bg-muted/30 h-64 w-full animate-pulse rounded-lg" />
            </Stack>
          </div>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RouteNotFoundState
          title="MCP server not found"
          description="This MCP server may have been deleted, renamed, or moved out of this project."
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
