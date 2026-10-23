import { AppRouteContentErrorBoundary } from "@/components/app-route-content-error-boundary";
import {
  RedirectFromMcpXNamespace,
  RedirectToAddFunction,
  RedirectToAddOpenAPI,
  RedirectToAddRemoteMcp,
  RedirectToAddTunneledMcp,
  RedirectToAddUnproxiedMcp,
  RedirectToCatalog,
  RedirectToCatalogDetail,
  RedirectToSourceDetail,
  RedirectToSources,
} from "@/pages/mcp/add/LegacyRedirects";
import { Route } from "react-router";
import type { AppRoute } from "./routes";

// S-853 moved Sources and the catalog under /mcp. These keep the old URLs
// resolving. They live here rather than in the route structure so they stay
// out of the sidebar, breadcrumbs, and the command palette.
export const legacyMcpRedirects = (
  <>
    <Route path="sources">
      <Route index element={<RedirectToSources />} />
      <Route path="add-openapi" element={<RedirectToAddOpenAPI />} />
      <Route path="add-function" element={<RedirectToAddFunction />} />
      <Route path="add-from-catalog" element={<RedirectToCatalog />} />
      <Route path="add-remote-mcp" element={<RedirectToAddRemoteMcp />} />
      <Route path="add-tunneled-mcp" element={<RedirectToAddTunneledMcp />} />
      <Route path="add-unproxied-mcp" element={<RedirectToAddUnproxiedMcp />} />
      <Route
        path=":sourceKind/:sourceSlug"
        element={<RedirectToSourceDetail />}
      />
    </Route>
    <Route path="catalog">
      <Route index element={<RedirectToCatalog />} />
      <Route path=":serverSpecifier" element={<RedirectToCatalogDetail />} />
    </Route>
    {/* The catalog briefly lived inside the add flow before becoming a tab of
        the MCP index. */}
    <Route path="mcp/add/catalog">
      <Route index element={<RedirectToCatalog />} />
      <Route path=":serverSpecifier" element={<RedirectToCatalogDetail />} />
    </Route>
    {/* Server pages lived under /mcp/x until the toolset and server routes
        merged. A server or toolset slugged "x" still owns /mcp/x and
        /mcp/x/<tab>, which outrank this splat (see resolveMcpServerPath). */}
    <Route
      path="mcp/x/:mcpServerSlug/*"
      element={<RedirectFromMcpXNamespace />}
    />
  </>
);

export const routesWithSubroutes = (routes: AppRoute[]): JSX.Element[] => {
  return routes
    .filter((item) => !item.external)
    .map((item) => (
      <Route
        key={item.title}
        path={item.url}
        element={
          item.component ? (
            <AppRouteContentErrorBoundary
              fallback={<div className="p-8 text-sm">Loading…</div>}
            >
              <item.component />
            </AppRouteContentErrorBoundary>
          ) : null
        }
      >
        {item.indexComponent && (
          <Route
            index
            element={
              <AppRouteContentErrorBoundary
                fallback={<div className="p-8 text-sm">Loading…</div>}
              >
                <item.indexComponent />
              </AppRouteContentErrorBoundary>
            }
          />
        )}
        {/* Check for any children routes stored on this item */}
        {routesWithSubroutes(
          Object.values(item).filter(
            (value) =>
              value &&
              typeof value === "object" &&
              "title" in value &&
              "url" in value,
          ) as unknown as AppRoute[],
        )}
      </Route>
    ));
};
