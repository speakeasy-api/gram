import { useCallback, useMemo } from "react";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";

import type { DeploymentExternalMCP } from "@gram/client/models/components/deploymentexternalmcp.js";
import { ExternalMCPServerEntry } from "@gram/client/models/components/externalmcpserverentry.js";
import {
  listedMcpServers,
  type ListedMcpServer,
} from "@/components/mcp/mcp-server-kind";
import {
  catalogInstances,
  catalogServerInstallState,
  liveCatalogExternalMcp,
  normalizedUrlSet,
  type CatalogInstallState,
} from "@/pages/catalog/catalogInstall";
import { queryKeyListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useQuery } from "@tanstack/react-query";
import { useRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { useUnproxiedMcpServers } from "@gram/client/react-query/unproxiedMcpServers.js";

interface ServerMeta {
  [namespace: string]: unknown;
  "io.modelcontextprotocol.registry/official"?: {
    status?: string;
    publishedAt?: string;
    updatedAt?: string;
  };
  /** Backend-owned publication time for native catalog records. */
  "com.speakeasy.ai/registry"?: {
    publishedAt?: string;
  };
}

export type CatalogServer = Omit<ExternalMCPServerEntry, "meta"> & {
  meta?: ServerMeta;
};

// The catalog is small and the backend returns the full list in a single
// response, so a plain query over the whole catalog is enough — searching,
// sorting, and filtering all happen client-side. An optional `search` is still
// forwarded so callers that only need a specific server (e.g. the detail page)
// can narrow the response.
type CatalogQueryOptions = {
  projectSlug?: string;
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
  const currentProjectSlug = useProjectSlugForRequests();
  const projectSlug = options?.projectSlug ?? currentProjectSlug;

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
 * Current installs are remote or unproxied MCP servers matched by endpoint
 * URL. Older installs are MCP servers whose toolset records the catalog
 * specifier, or a deployment attachment one of those servers still selects.
 * An attachment left behind after that server is deleted does not count.
 *
 * Lookups are keyed by project. A lookup that is still loading or has failed
 * leaves the state "unknown" instead of claiming the server is not added.
 */
export function useCatalogInstallations(projectSlug?: string): {
  installState: (server: CatalogServer) => CatalogInstallState;
  liveExternalMcp: (server: CatalogServer) => DeploymentExternalMCP | undefined;
  /** Undefined until every lookup has data. */
  instances: (server: CatalogServer) => ListedMcpServer[] | undefined;
} {
  const routeProject = useProjectSlugForRequests();
  const gramProject = projectSlug || routeProject;
  const projectRequest = gramProject ? { gramProject } : undefined;
  const queryOptions = { throwOnError: false, enabled: !!gramProject } as const;
  const remoteQuery = useRemoteMcpServers(
    projectRequest,
    undefined,
    queryOptions,
  );
  const unproxiedQuery = useUnproxiedMcpServers(
    projectRequest,
    undefined,
    queryOptions,
  );
  const deploymentQuery = useLatestDeployment(
    projectRequest,
    undefined,
    queryOptions,
  );
  // Server summaries carry the catalog specifier and external-MCP slugs.
  // The full toolset list includes every tool definition, which this check
  // does not need.
  const serversQuery = useMcpServers(projectRequest, undefined, queryOptions);

  const remoteUrls = useMemo(
    () =>
      normalizedUrlSet(
        (remoteQuery.data?.remoteMcpServers ?? []).map((server) => server.url),
      ),
    [remoteQuery.data?.remoteMcpServers],
  );
  const unproxiedUrls = useMemo(
    () =>
      normalizedUrlSet(
        (unproxiedQuery.data?.unproxiedMcpServers ?? []).map(
          (server) => server.url,
        ),
      ),
    [unproxiedQuery.data?.unproxiedMcpServers],
  );
  const serverEvidence = useMemo(() => {
    const originSpecifiers = new Set<string>();
    const referencedExternalMcpSlugs = new Set<string>();
    for (const server of serversQuery.data?.mcpServers ?? []) {
      const specifier = server.toolset?.registrySpecifier;
      if (specifier) originSpecifiers.add(specifier);
      for (const slug of server.toolset?.externalMcpSlugs ?? []) {
        if (slug) referencedExternalMcpSlugs.add(slug);
      }
    }
    return { originSpecifiers, referencedExternalMcpSlugs };
  }, [serversQuery.data?.mcpServers]);

  const externalMcps = deploymentQuery.data?.deployment?.externalMcps;
  const ready = useMemo(
    () => ({
      remote: remoteQuery.data !== undefined,
      unproxied: unproxiedQuery.data !== undefined,
      servers: serversQuery.data !== undefined,
      deployment: deploymentQuery.data !== undefined,
    }),
    [
      remoteQuery.data,
      unproxiedQuery.data,
      serversQuery.data,
      deploymentQuery.data,
    ],
  );
  const evidence = useMemo(
    () => ({
      remoteUrls,
      unproxiedUrls,
      originSpecifiers: serverEvidence.originSpecifiers,
      referencedExternalMcpSlugs: serverEvidence.referencedExternalMcpSlugs,
    }),
    [remoteUrls, unproxiedUrls, serverEvidence],
  );

  const installState = useCallback(
    (server: CatalogServer) =>
      catalogServerInstallState(server, evidence, externalMcps ?? [], ready),
    [evidence, externalMcps, ready],
  );
  const liveExternalMcp = useCallback(
    (server: CatalogServer) => {
      if (!ready.servers || !ready.deployment) return undefined;
      return liveCatalogExternalMcp(
        server,
        externalMcps ?? [],
        evidence.referencedExternalMcpSlugs,
      );
    },
    [
      evidence.referencedExternalMcpSlugs,
      externalMcps,
      ready.deployment,
      ready.servers,
    ],
  );

  const remoteUrlById = useMemo(
    () =>
      new Map(
        (remoteQuery.data?.remoteMcpServers ?? []).map((s) => [s.id, s.url]),
      ),
    [remoteQuery.data?.remoteMcpServers],
  );
  const unproxiedUrlById = useMemo(
    () =>
      new Map(
        (unproxiedQuery.data?.unproxiedMcpServers ?? []).map((s) => [
          s.id,
          s.url,
        ]),
      ),
    [unproxiedQuery.data?.unproxiedMcpServers],
  );
  const listed = useMemo(
    () => listedMcpServers(serversQuery.data?.mcpServers ?? []),
    [serversQuery.data?.mcpServers],
  );
  const instances = useCallback(
    (server: CatalogServer) => {
      if (
        !ready.remote ||
        !ready.unproxied ||
        !ready.servers ||
        !ready.deployment
      ) {
        return undefined;
      }
      const matched = new Set(
        catalogInstances(
          server,
          listed.map((entry) => entry.server),
          remoteUrlById,
          unproxiedUrlById,
          externalMcps ?? [],
        ),
      );
      return listed.filter((entry) => matched.has(entry.server));
    },
    [ready, listed, remoteUrlById, unproxiedUrlById, externalMcps],
  );

  return { installState, liveExternalMcp, instances };
}
