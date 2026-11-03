import { normalizeRemoteUrl } from "@/pages/catalog/remotes";

// tools:externalmcp:<slug>:<tool name>
const EXTERNAL_MCP_TOOL_URN = /^tools:externalmcp:([^:]+):/;

export function externalMcpSlugFromToolUrn(urn: string): string | undefined {
  const slug = EXTERNAL_MCP_TOOL_URN.exec(urn)?.[1];
  return slug || undefined;
}

export type CatalogExternalMcp = {
  slug: string;
  registryServerSpecifier: string;
};

export type CatalogInstallEvidence = {
  installedUrls: ReadonlySet<string>;
  originSpecifiers: ReadonlySet<string>;
  referencedExternalMcpSlugs: ReadonlySet<string>;
};

type CatalogToolset = {
  origin?: { registrySpecifier?: string | undefined } | null | undefined;
  toolUrns?: readonly string[] | null | undefined;
};

/**
 * Evidence that a catalog entry is actually installed.
 *
 * A deployment attachment whose slug no toolset still selects is not an
 * installation: deleting the MCP server leaves that row behind, and the
 * catalog used to treat it as "Added".
 */
export function collectCatalogInstallEvidence(input: {
  urls: readonly string[];
  toolsets: readonly CatalogToolset[];
}): CatalogInstallEvidence {
  const installedUrls = new Set<string>();
  for (const url of input.urls) {
    const normalized = normalizeRemoteUrl(url);
    if (normalized !== "") installedUrls.add(normalized);
  }

  const originSpecifiers = new Set<string>();
  const referencedExternalMcpSlugs = new Set<string>();
  for (const toolset of input.toolsets) {
    const specifier = toolset.origin?.registrySpecifier;
    if (specifier) originSpecifiers.add(specifier);
    for (const urn of toolset.toolUrns ?? []) {
      const slug = externalMcpSlugFromToolUrn(urn);
      if (slug) referencedExternalMcpSlugs.add(slug);
    }
  }

  return { installedUrls, originSpecifiers, referencedExternalMcpSlugs };
}

export function liveCatalogExternalMcp<T extends CatalogExternalMcp>(
  server: { registrySpecifier: string },
  externalMcps: readonly T[],
  referencedSlugs: ReadonlySet<string>,
): T | undefined {
  return externalMcps.find(
    (mcp) =>
      mcp.registryServerSpecifier === server.registrySpecifier &&
      referencedSlugs.has(mcp.slug),
  );
}

export function catalogServerIsInstalled(
  server: {
    registrySpecifier: string;
    remotes?: readonly { url: string }[] | null | undefined;
  },
  evidence: CatalogInstallEvidence,
  externalMcps: readonly CatalogExternalMcp[],
): boolean {
  if (evidence.originSpecifiers.has(server.registrySpecifier)) return true;
  if (
    liveCatalogExternalMcp(
      server,
      externalMcps,
      evidence.referencedExternalMcpSlugs,
    )
  ) {
    return true;
  }
  return (server.remotes ?? []).some((remote) =>
    evidence.installedUrls.has(normalizeRemoteUrl(remote.url)),
  );
}
