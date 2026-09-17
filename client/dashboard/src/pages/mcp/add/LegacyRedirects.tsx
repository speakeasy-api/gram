import { Skeleton } from "@/components/ui/Skeleton";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { useRoutes } from "@/routes";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { useUnproxiedMcpServers } from "@gram/client/react-query/unproxiedMcpServers.js";
import { Navigate, useParams } from "react-router";
import {
  legacySourceKindLookups,
  parseLegacySourceKind,
  resolveLegacySourceRedirect,
  type LegacySourceTarget,
} from "./legacySourceRedirect";

/**
 * S-853 folded Sources and the catalog under /mcp: MCP is the inventory, and
 * every way of adding a server now starts from the MCP page. These redirects
 * keep the old top-level URLs resolving — bookmarks, docs links, and anything
 * shared before the move — and are registered in App.tsx rather than in the
 * route structure so they stay out of the sidebar, breadcrumbs, and the
 * command palette.
 */

// Sources moved under MCP rather than going away: the CLI links here after a
// push, so the old top-level URL lands on the same shelf.
// Deployments joined MCP as a tab; the old top-level listing keeps resolving.
// The deployment *detail* URL is untouched — it is what the CLI prints after a
// push.
export function RedirectToDeployments(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.deployments.href()} replace />;
}

export function RedirectToSources(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.sources.href()} replace />;
}

function legacySourceTargetHref(
  routes: ReturnType<typeof useRoutes>,
  target: LegacySourceTarget,
): string {
  switch (target.kind) {
    case "source":
      return routes.mcp.sources.detail.href(target.assetId);
    case "mcp-server":
      return routes.mcp.x.overview.href(target.routeParam);
    case "toolset":
      return routes.mcp.details.href(target.slug);
    case "sources-list":
      return routes.mcp.sources.href();
    case "mcp-list":
      return routes.mcp.href();
  }
}

// The per-kind source pages are gone, but what they showed still has a home:
// OpenAPI and function sources at /mcp/sources/:sourceId, remote, tunneled and
// unproxied sources on the /mcp/x page of the server fronting them, and an
// external MCP on the toolset carrying its tools. The old URL carried a slug,
// so the new address has to be looked up before redirecting.
export function RedirectToSourceDetail(): JSX.Element {
  const routes = useRoutes();
  const { sourceKind, sourceSlug } = useParams<{
    sourceKind: string;
    sourceSlug: string;
  }>();
  const kind = parseLegacySourceKind(sourceKind);
  const lookups = legacySourceKindLookups(kind);

  const deploymentQuery = useActiveDeployment({ enabled: lookups.deployment });
  const mcpServersQuery = useMcpServers(undefined, undefined, {
    enabled: lookups.mcpServers,
  });
  const remoteQuery = useRemoteMcpServers(undefined, undefined, {
    enabled: kind === "remotemcp",
  });
  const tunneledQuery = useTunneledMcpServers(undefined, undefined, {
    enabled: kind === "tunneledmcp",
  });
  const unproxiedQuery = useUnproxiedMcpServers(undefined, undefined, {
    enabled: kind === "unproxiedmcp",
  });
  const toolsetsQuery = useListToolsets(undefined, undefined, {
    enabled: lookups.toolsets,
  });

  // isLoading is false for disabled queries, so only the lookups this kind
  // enabled hold the redirect. A failed lookup falls through to the fallback.
  const resolving = [
    deploymentQuery,
    mcpServersQuery,
    remoteQuery,
    tunneledQuery,
    unproxiedQuery,
    toolsetsQuery,
  ].some((query) => query.isLoading);

  if (resolving) {
    return (
      <div className="mx-auto w-full max-w-[1270px] space-y-3 px-8 py-8">
        <Skeleton className="h-6 w-64" />
        <Skeleton className="h-4 w-96" />
      </div>
    );
  }

  const target = resolveLegacySourceRedirect(kind, sourceSlug ?? "", {
    deployment: deploymentQuery.data?.deployment,
    mcpServers: mcpServersQuery.data?.mcpServers ?? [],
    remoteMcpServers: remoteQuery.data?.remoteMcpServers ?? [],
    tunneledMcpServers: tunneledQuery.data?.tunneledMcpServers ?? [],
    unproxiedMcpServers: unproxiedQuery.data?.unproxiedMcpServers ?? [],
    toolsets: toolsetsQuery.data?.toolsets ?? [],
  });

  return <Navigate to={legacySourceTargetHref(routes, target)} replace />;
}

export function RedirectToAddRemoteMcp(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.remote.href()} replace />;
}

export function RedirectToAddTunneledMcp(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.tunneled.href()} replace />;
}

// Unproxied servers no longer have their own page: the remote form asks
// whether Speakeasy sits in the request path instead.
export function RedirectToAddUnproxiedMcp(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.remote.href()} replace />;
}

export function RedirectToAddOpenAPI(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.openapi.href()} replace />;
}

export function RedirectToAddFunction(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.function.href()} replace />;
}

// The catalog is a tab of the MCP index rather than a step inside the add
// flow, so `/mcp/add/catalog` joins `/catalog` and `/sources/add-from-catalog`
// in redirecting to it.
export function RedirectToCatalog(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.catalog.href()} replace />;
}

export function RedirectToCatalogDetail(): JSX.Element {
  const routes = useRoutes();
  const { serverSpecifier } = useParams();
  if (!serverSpecifier) {
    return <Navigate to={routes.mcp.catalog.href()} replace />;
  }
  // useParams decodes the segment; the catalog builds its hrefs from an
  // encoded specifier, so re-encode rather than hand the router a raw one.
  return (
    <Navigate
      to={routes.mcp.catalog.detail.href(encodeURIComponent(serverSpecifier))}
      replace
    />
  );
}
