import { useRoutes } from "@/routes";
import { Navigate, useParams } from "react-router";

/**
 * S-853 folded Sources and the catalog under /mcp: MCP is the inventory, and
 * every way of adding a server now starts from the MCP page. These redirects
 * keep the old top-level URLs resolving — bookmarks, docs links, and anything
 * shared before the move — and are registered in App.tsx rather than in the
 * route structure so they stay out of the sidebar, breadcrumbs, and the
 * command palette.
 */

// Sources no longer has a page of its own; the add flow is where its two
// remaining kinds are reached.
export function RedirectToSources(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.href()} replace />;
}

// Source detail pages are gone: the servers they described live on the MCP
// inventory, so old links land there.
export function RedirectToSourceDetail(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.href()} replace />;
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

export function RedirectToCatalog(): JSX.Element {
  const routes = useRoutes();
  return <Navigate to={routes.mcp.add.catalog.href()} replace />;
}

export function RedirectToCatalogDetail(): JSX.Element {
  const routes = useRoutes();
  const { serverSpecifier } = useParams();
  if (!serverSpecifier) {
    return <Navigate to={routes.mcp.add.catalog.href()} replace />;
  }
  // useParams decodes the segment; the catalog builds its hrefs from an
  // encoded specifier, so re-encode rather than hand the router a raw one.
  return (
    <Navigate
      to={routes.mcp.add.catalog.detail.href(
        encodeURIComponent(serverSpecifier),
      )}
      replace
    />
  );
}
