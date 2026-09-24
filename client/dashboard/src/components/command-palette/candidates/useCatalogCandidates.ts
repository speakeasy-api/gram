import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { CATALOG_STALE_TIME_MS } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import type { ExternalMCPServerEntry } from "@gram/client/models/components/externalmcpserverentry.js";
import { useListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

/**
 * Third-party servers offered by the registry catalog.
 *
 * A kind of its own rather than folded into `mcp_server`: a catalog hit is
 * something this project could run, not something it runs, and the two
 * legitimately share a name once an entry has been added. The registry
 * specifier rides along in the detail, so a row is never mistaken for one of
 * the project's own servers.
 *
 * listCatalog requires project:read, so the caller gates `enabled` on that
 * scope rather than on the looser any-of gate the catalog page renders behind.
 */
export function useCatalogCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Same request the catalog page makes, down to the freshness window:
  // staleness is per-observer, so without it this reader would refetch the
  // registry on every palette open despite sharing a cache entry the page
  // still considers fresh.
  const { data } = useListMCPCatalog({ gramProject }, undefined, {
    enabled,
    staleTime: CATALOG_STALE_TIME_MS,
    throwOnError: false,
  });

  return useMemo(() => {
    // A specifier is unique within a registry but not across them, and the
    // detail route is addressed by specifier alone — it resolves the first
    // entry that matches. Two registries publishing one server would
    // otherwise yield two identical rows that lead to the same page, so only
    // the row that page actually opens is offered.
    const bySpecifier = new Map<string, ExternalMCPServerEntry>();
    for (const server of data?.servers ?? []) {
      if (!bySpecifier.has(server.registrySpecifier)) {
        bySpecifier.set(server.registrySpecifier, server);
      }
    }
    return Array.from(bySpecifier.values()).map(
      (server): LauncherCandidate => ({
        id: `catalog:${server.registrySpecifier}`,
        kind: "catalog",
        title: server.title || server.registrySpecifier,
        // Dropped when the specifier is standing in as the title: an untitled
        // entry would otherwise print it twice.
        detail: server.title
          ? `Catalog entry · ${server.registrySpecifier}`
          : "Catalog entry",
        keywords: ["catalog", server.registrySpecifier],
        verbs: ["open"],
        icon: "store",
        group: "MCP Catalog",
        run: () =>
          routes.mcp.catalog.detail.goTo(
            encodeURIComponent(server.registrySpecifier),
          ),
      }),
    );
  }, [data, routes]);
}
