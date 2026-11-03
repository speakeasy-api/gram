import { useCallback, useMemo } from "react";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";

import type { DeploymentExternalMCP } from "@gram/client/models/components/deploymentexternalmcp.js";
import { ExternalMCPServerEntry } from "@gram/client/models/components/externalmcpserverentry.js";
import { ExternalMCPTool } from "@gram/client/models/components/externalmcptool.js";
import {
  catalogServerIsInstalled,
  collectCatalogInstallEvidence,
  liveCatalogExternalMcp,
} from "@/pages/catalog/catalogInstall";
import { queryKeyListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useQuery } from "@tanstack/react-query";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useUnproxiedMcpServers } from "@gram/client/react-query/unproxiedMcpServers.js";

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

/**
 * How long a fetched catalog stays fresh.
 *
 * Exported because staleness is per-observer in react-query: a second reader of
 * this query that does not set it refetches the registry on every mount, even
 * though the cache entry it shares is still fresh here.
 */
export const CATALOG_STALE_TIME_MS = 5 * 60 * 1000;

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
    staleTime: CATALOG_STALE_TIME_MS,
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
 * Informational "already installed" check for catalog entries. Installing
 * again is always allowed and just creates another server, so this only
 * drives indicators, never blocking.
 *
 * Current installs are remote (or, for a few entries, unproxied) MCP servers
 * matched by endpoint URL. Older installs are toolsets stamped with the
 * catalog specifier, or a deployment attachment a toolset still selects.
 * An attachment left behind after that toolset is deleted does not count.
 */
export function useCatalogInstallations(): {
  isInstalled: (server: PulseMCPServer) => boolean;
  liveExternalMcp: (
    server: PulseMCPServer,
  ) => DeploymentExternalMCP | undefined;
} {
  const gramProject = useProjectSlugForRequests();
  const { data: remoteServersData } = useRemoteMcpServers();
  // A failed lookup must not take down the catalog; the badge just stays off.
  // Key toolsets the same way as useToolsets so a project switch cannot reuse
  // another project's list.
  const { data: unproxiedServersData } = useUnproxiedMcpServers(
    gramProject ? { gramProject } : undefined,
    undefined,
    { throwOnError: false },
  );
  const { data: deploymentResult } = useLatestDeployment();
  const { data: toolsetsResult } = useListToolsets({ gramProject }, undefined, {
    throwOnError: false,
  });

  const externalMcps = deploymentResult?.deployment?.externalMcps;
  const evidence = useMemo(
    () =>
      collectCatalogInstallEvidence({
        urls: [
          ...(remoteServersData?.remoteMcpServers ?? []).map(
            (server) => server.url,
          ),
          ...(unproxiedServersData?.unproxiedMcpServers ?? []).map(
            (server) => server.url,
          ),
        ],
        toolsets: toolsetsResult?.toolsets ?? [],
      }),
    [
      remoteServersData?.remoteMcpServers,
      unproxiedServersData?.unproxiedMcpServers,
      toolsetsResult?.toolsets,
    ],
  );

  const isInstalled = useCallback(
    (server: PulseMCPServer) =>
      catalogServerIsInstalled(server, evidence, externalMcps ?? []),
    [evidence, externalMcps],
  );
  const liveExternalMcp = useCallback(
    (server: PulseMCPServer) =>
      liveCatalogExternalMcp(
        server,
        externalMcps ?? [],
        evidence.referencedExternalMcpSlugs,
      ),
    [evidence.referencedExternalMcpSlugs, externalMcps],
  );

  return { isInstalled, liveExternalMcp };
}
