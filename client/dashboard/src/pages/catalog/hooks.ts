import { useCallback, useMemo } from "react";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";

import { ExternalMCPServerEntry } from "@gram/client/models/components/externalmcpserverentry.js";
import { ExternalMCPTool } from "@gram/client/models/components/externalmcptool.js";
import { normalizeRemoteUrl } from "@/pages/catalog/remotes";
import { queryKeyListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
import { useQuery } from "@tanstack/react-query";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";

interface ServerMeta {
  "com.pulsemcp/server"?: {
    visitorsEstimateMostRecentWeek?: number;
    visitorsEstimateLastFourWeeks?: number;
    visitorsEstimateTotal?: number;
    isOfficial?: boolean;
  };
  "com.pulsemcp/server-version"?: {
    source?: string;
    status?: string;
    publishedAt?: string;
    updatedAt?: string;
    isLatest?: boolean;
    // The catalog list response strips per-tool definitions from the meta blob
    // to stay small; only the lightweight auth info is kept. Full tools come
    // from getServerDetails (see `tools` below, populated by enrichment).
    "remotes[0]"?: {
      authOptions?: {
        type?: string;
      }[];
    };
  };
}

// The catalog list returns `ExternalMCPServerEntry` (no tools). `tools` is
// optional and populated client-side by enrichment (getServerDetails) for the
// add/release flow that needs per-tool URNs.
export type PulseMCPServer = Omit<ExternalMCPServerEntry, "meta"> & {
  meta: ServerMeta;
  tools?: ExternalMCPTool[];
};

// The catalog is small and the backend returns the full list in a single
// response, so a plain query over the whole catalog is enough — searching,
// sorting, and filtering all happen client-side. An optional `search` is still
// forwarded so callers that only need a specific server (e.g. the detail page)
// can narrow the response.
type CatalogQueryOptions = {
  enabled?: boolean;
  throwOnError?: boolean;
};

function useListMCPCatalogImpl(
  search?: string,
  registryId?: string,
  options?: CatalogQueryOptions,
) {
  const client = useSdkClient();
  const projectSlug = useProjectSlugForRequests();

  return useQuery({
    queryKey: queryKeyListMCPCatalog({
      search: search || undefined,
      registryId: registryId || undefined,
      gramProject: projectSlug,
    }),
    queryFn: async () =>
      client.mcpRegistries.listCatalog({
        search: search || undefined,
        registryId: registryId || undefined,
        gramProject: projectSlug,
      }),
    staleTime: 5 * 60 * 1000, // 5 minutes - won't refetch if data is fresh
    enabled: options?.enabled ?? true,
    throwOnError: options?.throwOnError,
  });
}

export function useListMCPCatalog(
  search?: string,
  registryId?: string,
  // Callers that only need the catalog conditionally (e.g. only for
  // external-MCP-backed toolsets) can defer the fetch entirely rather than
  // always paying for it on mount.
  options?: CatalogQueryOptions,
): ReturnType<typeof useListMCPCatalogImpl> {
  return useListMCPCatalogImpl(search, registryId, options);
}

/**
 * Informational "already installed" check for catalog entries: true when a
 * remote MCP server with one of the entry's endpoint URLs already exists in
 * the project. Installing again is always allowed and just creates another
 * server, so this only drives indicators, never blocking.
 */
export function useIsCatalogServerInstalled(): (
  server: PulseMCPServer,
) => boolean {
  const { data: remoteServersData } = useRemoteMcpServers();
  const installedUrls = useMemo(
    () =>
      new Set(
        (remoteServersData?.remoteMcpServers ?? []).map((server) =>
          normalizeRemoteUrl(server.url),
        ),
      ),
    [remoteServersData?.remoteMcpServers],
  );

  return useCallback(
    (server: PulseMCPServer) =>
      (server.remotes ?? []).some((remote) =>
        installedUrls.has(normalizeRemoteUrl(remote.url)),
      ),
    [installedUrls],
  );
}
