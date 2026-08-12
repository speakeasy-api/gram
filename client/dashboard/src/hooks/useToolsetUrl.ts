import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useListDomains } from "@gram/client/react-query/listDomains.js";
import { useMemo } from "react";

export function useCustomDomain(enabled = true): {
  domain: CustomDomain | undefined;
  refetch: ReturnType<typeof useListDomains>["refetch"];
  isLoading: boolean;
} {
  const { data, isLoading, refetch } = useListDomains(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
    enabled,
  });

  return { domain: data?.domains[0], refetch, isLoading };
}

export function useCustomDomains(enabled = true): {
  domains: CustomDomain[];
  isLoading: boolean;
  refetch: ReturnType<typeof useListDomains>["refetch"];
} {
  const { data, isLoading, refetch } = useListDomains(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
    enabled,
  });

  return { domains: data?.domains ?? [], isLoading, refetch };
}

// useMcpEndpointUrl resolves the runtime install URL for a single mcp_endpoint
// row. Platform-domain endpoints (`custom_domain_id` empty) resolve under the
// Gram-hosted `/mcp/<slug>` runtime path; custom-domain endpoints resolve
// under the matching `custom_domains.domain` value with the same suffix.
// Returns `undefined` when the endpoint has no slug or when its custom domain
// hasn't resolved yet (loading or denied), so callers can gracefully render an
// empty state.
function useMcpEndpointUrl(endpoint: McpEndpoint | undefined): {
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

  const mcpUrl = `${serverURL}/mcp/${endpoint.slug}`;
  return { mcpUrl, installPageUrl: `${mcpUrl}/install` };
}

// useResolvedMcpServerUrl resolves the runtime MCP URL for an mcp_server from
// its endpoints, preferring a custom-domain endpoint. While the custom domain
// is still resolving it falls back to the Gram-hosted `/mcp/<slug>` path so
// callers always have a usable URL once a slug exists.
export function useResolvedMcpServerUrl(
  endpoints: McpEndpoint[],
  isLoadingEndpoints: boolean,
): {
  mcpUrl: string | undefined;
  installPageUrl: string | undefined;
  loading: boolean;
} {
  const endpoint = useMemo(
    () => endpoints.find((e) => e.customDomainId) ?? endpoints[0],
    [endpoints],
  );
  const { mcpUrl: resolvedUrl } = useMcpEndpointUrl(endpoint);
  const fallbackUrl = endpoint?.slug
    ? `${getServerURL()}/mcp/${endpoint.slug}`
    : undefined;
  const mcpUrl = resolvedUrl ?? fallbackUrl;

  return {
    mcpUrl,
    installPageUrl: mcpUrl ? `${mcpUrl}/install` : undefined,
    loading: isLoadingEndpoints,
  };
}

// Path suffix for a toolset-backed MCP URL. Prefers the custom mcpSlug; the
// legacy form requires the default environment — without both there is no
// routable MCP URL, so return undefined rather than an invalid
// /mcp/<project>/<toolset> path.
function mcpUrlSuffix(
  project: { slug: string },
  toolset: Pick<ToolsetEntry, "slug" | "mcpSlug" | "defaultEnvironmentSlug">,
): string | undefined {
  if (toolset.mcpSlug) return toolset.mcpSlug;
  if (!toolset.defaultEnvironmentSlug) return undefined;
  return [project.slug, toolset.slug, toolset.defaultEnvironmentSlug].join("/");
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

  const urlSuffix = mcpUrlSuffix(project, toolset);
  if (!urlSuffix) {
    return { url: undefined, customServerURL, installPageUrl: "" };
  }
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
 * Returns undefined when the toolset has no routable MCP URL (no mcpSlug and
 * no default environment).
 */
export function internalMcpUrl(
  project: { slug: string },
  toolset: Pick<ToolsetEntry, "slug" | "mcpSlug" | "defaultEnvironmentSlug">,
): string | undefined {
  const suffix = mcpUrlSuffix(project, toolset);
  return suffix ? `${getServerURL()}/mcp/${suffix}` : undefined;
}

/**
 * Formats the resolved URL for an MCP endpoint registered under a custom
 * domain. MCP endpoints are addressed at `https://<domain>/mcp/<slug>` — the
 * `/mcp/` segment is implicit and shared by both platform and custom-domain
 * endpoints.
 */
export function customDomainMcpEndpointUrl(
  domain: string,
  slug: string,
): string {
  return `https://${domain}/mcp/${slug}`;
}
