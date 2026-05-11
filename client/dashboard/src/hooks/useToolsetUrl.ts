import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import { McpEndpoint, ToolsetEntry } from "@gram/client/models/components";
import { useGetDomain } from "@gram/client/react-query";

export function useCustomDomain(enabled = true) {
  const {
    data: domain,
    isLoading,
    refetch,
  } = useGetDomain(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
    enabled,
  });

  return { domain: domain, refetch: refetch, isLoading };
}

// useCustomDomains is a forward-compatible shim around useGetDomain that
// returns the org's custom domains as an array. The backend currently enforces
// a single custom domain per organization (see custom_domains_organization_id_key
// in schema.sql), so the array has at most one entry today. Tracked under
// AGE-2227 (DB migration to drop the unique index) and AGE-2229 (list RPC +
// dashboard call-site updates) — once both ship this shim swaps to a real
// list-backed implementation without touching any callsites that already iterate
// over `domains`.
export function useCustomDomains(enabled = true): {
  domains: ReturnType<typeof useGetDomain>["data"][];
  isLoading: boolean;
  refetch: ReturnType<typeof useGetDomain>["refetch"];
} {
  const { domain, isLoading, refetch } = useCustomDomain(enabled);

  const domains = domain ? [domain] : [];

  if (
    import.meta.env.DEV &&
    Array.isArray(domain) &&
    (domain as unknown[]).length > 1
  ) {
    // Defensive logging for the AGE-2229 swap: if useGetDomain ever starts
    // returning a list, callsites that still assume `domains[0]` semantics
    // need an audit pass.
    console.warn(
      "useCustomDomains: useGetDomain returned multiple domains; audit callers assuming single-domain semantics (AGE-2229).",
    );
  }

  return { domains, isLoading, refetch };
}

// useMcpEndpointUrl resolves the runtime install URL for a single mcp_endpoint
// row. Platform-domain endpoints (`custom_domain_id` empty) resolve under the
// Gram-hosted `/x/mcp/<slug>` runtime path; custom-domain endpoints resolve
// under the matching `custom_domains.domain` value with the same suffix.
// Returns `undefined` when the endpoint has no slug or when its custom domain
// hasn't resolved yet (loading or denied), so callers can gracefully render an
// empty state.
export function useMcpEndpointUrl(endpoint: McpEndpoint | undefined): {
  mcpUrl: string | undefined;
  installPageUrl: string | undefined;
} {
  // Only fetch domain data when the endpoint actually has a custom domain so
  // platform-domain endpoints don't pay the round trip.
  const { domains } = useCustomDomains(!!endpoint?.customDomainId);

  if (!endpoint || !endpoint.slug) {
    return { mcpUrl: undefined, installPageUrl: undefined };
  }

  let serverURL = getServerURL();
  if (endpoint.customDomainId) {
    const match = domains.find((d) => d?.id === endpoint.customDomainId);
    if (!match) {
      // Domain not yet resolved (loading or denied); avoid emitting a partial
      // URL that points at the Gram domain when the customer expected their
      // custom domain.
      return { mcpUrl: undefined, installPageUrl: undefined };
    }
    serverURL = `https://${match.domain}`;
  }

  const mcpUrl = `${serverURL}/x/mcp/${endpoint.slug}`;
  return { mcpUrl, installPageUrl: `${mcpUrl}/install` };
}

export function useMcpUrl(
  toolset:
    | Pick<
        ToolsetEntry,
        | "slug"
        | "customDomainId"
        | "mcpSlug"
        | "defaultEnvironmentSlug"
        | "mcpIsPublic"
      >
    | undefined,
): {
  url: string | undefined;
  customServerURL: string | undefined;
  installPageUrl: string;
} {
  // Only fetch domain data when the toolset actually has a custom domain
  // configured. This avoids a ~1s request on pages like Home where most
  // toolsets don't use custom domains.
  const { domain } = useCustomDomain(!!toolset?.customDomainId);
  const project = useProject();

  if (!toolset)
    return { url: undefined, customServerURL: undefined, installPageUrl: "" };

  // Determine which server URL to use
  let customServerURL: string | undefined;
  if (domain && toolset.customDomainId && domain.id == toolset.customDomainId) {
    customServerURL = `https://${domain.domain}`;
  }

  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
  const mcpUrl = `${
    toolset.mcpSlug && customServerURL ? customServerURL : getServerURL()
  }/mcp/${urlSuffix}`;

  // Always use our URL for install page when server is private, even for
  // custom domains to ensure cookie is present
  const installPageUrl = toolset.mcpIsPublic
    ? `${mcpUrl}/install`
    : `${getServerURL()}/mcp/${urlSuffix}/install`;

  return {
    url: mcpUrl,
    customServerURL,
    installPageUrl,
  };
}

/**
 * Returns an MCP URL that always uses the Gram domain, ignoring any custom domain.
 * Use this for internal tools like the playground where we want consistent routing.
 */
export function useInternalMcpUrl(
  toolset:
    | Pick<ToolsetEntry, "slug" | "mcpSlug" | "defaultEnvironmentSlug">
    | undefined,
): string | undefined {
  const project = useProject();
  if (!toolset) return undefined;
  return internalMcpUrl({ slug: project.slug }, toolset);
}

/**
 * Non-hook variant of {@link useInternalMcpUrl}. Use this when the project and
 * toolset are already in scope (e.g. when mapping over an array of toolsets).
 */
export function internalMcpUrl(
  project: { slug: string },
  toolset: Pick<ToolsetEntry, "slug" | "mcpSlug" | "defaultEnvironmentSlug">,
): string {
  const urlSuffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : `${project.slug}/${toolset.slug}/${toolset.defaultEnvironmentSlug}`;
  return `${getServerURL()}/mcp/${urlSuffix}`;
}
