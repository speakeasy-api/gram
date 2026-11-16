import { normalizeRemoteUrl } from "@/pages/catalog/remotes";

export type CatalogInstallState = "added" | "not-added" | "unknown";

export function normalizedUrlSet(urls: readonly string[]): Set<string> {
  const urlsNormalized = new Set<string>();
  for (const url of urls) {
    const normalized = normalizeRemoteUrl(url);
    if (normalized !== "") urlsNormalized.add(normalized);
  }
  return urlsNormalized;
}

export type CatalogInstallEvidence = {
  remoteUrls: ReadonlySet<string>;
  unproxiedUrls: ReadonlySet<string>;
  originSpecifiers: ReadonlySet<string>;
};

/** Which lookups have data. A lookup that is still loading or failed is not ready. */
export type CatalogInstallReady = {
  remote: boolean;
  unproxied: boolean;
  servers: boolean;
};

/**
 * "not-added" only when every lookup that could still show an install has
 * succeeded and none matched. A loading or failed lookup stays "unknown" so
 * the catalog does not flash Add for a server that is already installed.
 */
export function catalogServerInstallState(
  server: {
    registrySpecifier: string;
    remotes?: readonly { url: string }[] | null | undefined;
  },
  evidence: CatalogInstallEvidence,
  ready: CatalogInstallReady,
): CatalogInstallState {
  const urls = (server.remotes ?? []).map((remote) =>
    normalizeRemoteUrl(remote.url),
  );
  if (
    ready.remote &&
    urls.some((url) => url !== "" && evidence.remoteUrls.has(url))
  ) {
    return "added";
  }
  if (
    ready.unproxied &&
    urls.some((url) => url !== "" && evidence.unproxiedUrls.has(url))
  ) {
    return "added";
  }
  if (
    ready.servers &&
    evidence.originSpecifiers.has(server.registrySpecifier)
  ) {
    return "added";
  }
  if (ready.remote && ready.unproxied && ready.servers) {
    return "not-added";
  }
  return "unknown";
}

/** The parts of an MCP server row that can tie it to a catalog entry. */
export type CatalogInstanceCandidate = {
  remoteMcpServerId?: string | undefined;
  unproxiedMcpServerId?: string | undefined;
  toolset?: { registrySpecifier?: string | undefined } | undefined;
};

/**
 * The project's MCP servers created from this catalog entry, by the same
 * evidence `catalogServerInstallState` uses. The listings only return live
 * rows, so deleted servers never match.
 */
export function catalogInstances<T extends CatalogInstanceCandidate>(
  server: {
    registrySpecifier: string;
    remotes?: readonly { url: string }[] | null | undefined;
  },
  mcpServers: readonly T[],
  remoteUrlById: ReadonlyMap<string, string>,
  unproxiedUrlById: ReadonlyMap<string, string>,
): T[] {
  const urls = normalizedUrlSet((server.remotes ?? []).map((r) => r.url));
  const urlMatches = (
    id: string | undefined,
    byId: ReadonlyMap<string, string>,
  ) => {
    const url = id ? byId.get(id) : undefined;
    return url !== undefined && urls.has(normalizeRemoteUrl(url));
  };
  return mcpServers.filter(
    (candidate) =>
      urlMatches(candidate.remoteMcpServerId, remoteUrlById) ||
      urlMatches(candidate.unproxiedMcpServerId, unproxiedUrlById) ||
      candidate.toolset?.registrySpecifier === server.registrySpecifier,
  );
}
