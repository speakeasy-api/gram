import type { CatalogAuthType } from "@/pages/catalog/hooks/serverMetadata";

export type ExternalMcpOAuthConfigStatus =
  | "required-unconfigured"
  | "configured"
  | "unknown"
  | "not-required"
  | "not-external-mcp";

/** The external MCP slug a toolset's OAuth state is judged by: the first in sorted order. */
export function primaryExternalMcpSlug(
  toolUrns: readonly string[] | undefined,
): string | undefined {
  const slugs = (toolUrns ?? [])
    .filter((urn) => urn.includes(":externalmcp:"))
    .map((urn) => urn.split(":")[2])
    .filter((slug): slug is string => !!slug)
    .sort();
  return slugs[0];
}

export function externalMcpOAuthConfigStatus({
  externalMcpSlug,
  externalMcps,
  catalogAuthBySpecifier,
  hasOAuthServer,
  hasSessionIssuer,
}: {
  externalMcpSlug: string | undefined;
  externalMcps: ReadonlyArray<{
    slug: string;
    registryServerSpecifier?: string | undefined;
  }>;
  catalogAuthBySpecifier: ReadonlyMap<string, CatalogAuthType>;
  hasOAuthServer: boolean;
  hasSessionIssuer: boolean;
}): ExternalMcpOAuthConfigStatus {
  if (!externalMcpSlug) return "not-external-mcp";
  if (hasOAuthServer || hasSessionIssuer) return "configured";
  const specifier = externalMcps.find(
    (mcp) => mcp.slug === externalMcpSlug,
  )?.registryServerSpecifier;
  const authType = specifier
    ? catalogAuthBySpecifier.get(specifier)
    : undefined;
  if (!authType || authType === "unknown") return "unknown";
  if (authType !== "oauth") return "not-required";
  return "required-unconfigured";
}
