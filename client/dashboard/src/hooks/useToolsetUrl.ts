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
// under the matching `custom_domains.domain` value with the same suffix. A
// domain-root endpoint serves MCP at the bare custom domain, so that form is
// preferred (`/mcp/<slug>` stays a valid alternate path); its install page
// still lives under `/mcp/<slug>/install`.
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
    if (endpoint.isDomainRoot) {
      const rootUrl = `https://${match.domain}`;
      return {
        mcpUrl: rootUrl,
        installPageUrl: `${rootUrl}/mcp/${endpoint.slug}/install`,
      };
    }
    serverURL = `https://${match.domain}`;
  }

  const mcpUrl = `${serverURL}/mcp/${endpoint.slug}`;
  return { mcpUrl, installPageUrl: `${mcpUrl}/install` };
}

// useResolvedMcpServerUrl resolves the runtime MCP URL for an mcp_server from
// its endpoints, preferring a custom-domain endpoint. A custom-domain endpoint
// whose domain hasn't resolved yet never degrades to a platform-host URL built
// from its slug — that URL is not a registered endpoint. An actual
// platform-domain endpoint, when one exists, is the fallback.
export function useResolvedMcpServerUrl(
  endpoints: McpEndpoint[],
  isLoadingEndpoints: boolean,
): {
  mcpUrl: string | undefined;
  installPageUrl: string | undefined;
  loading: boolean;
} {
  const customEndpoint = useMemo(
    () => endpoints.find((e) => e.customDomainId),
    [endpoints],
  );
  const platformEndpoint = useMemo(
    () => endpoints.find((e) => !e.customDomainId),
    [endpoints],
  );
  const custom = useMcpEndpointUrl(customEndpoint);
  const platform = useMcpEndpointUrl(platformEndpoint);

  return {
    mcpUrl: custom.mcpUrl ?? platform.mcpUrl,
    installPageUrl: custom.installPageUrl ?? platform.installPageUrl,
    loading: isLoadingEndpoints,
  };
}

export function useMcpUrl(
  toolset:
    | Pick<ToolsetEntry, "customDomainId" | "mcpSlug" | "mcpIsPublic">
    | undefined,
): {
  url: string | undefined;
  customServerURL: string | undefined;
  installPageUrl: string | undefined;
} {
  // Only fetch domain data when the toolset actually has a custom domain
  // configured. This avoids a ~1s request on pages like Home where most
  // toolsets don't use custom domains.
  const { domain } = useCustomDomain(!!toolset?.customDomainId);

  if (!toolset)
    return {
      url: undefined,
      customServerURL: undefined,
      installPageUrl: undefined,
    };

  let customServerURL: string | undefined;
  if (domain && toolset.customDomainId && domain.id == toolset.customDomainId) {
    customServerURL = `https://${domain.domain}`;
  }

  // A toolset that was never published under an MCP slug has no runtime URL.
  if (!toolset.mcpSlug) {
    return { url: undefined, customServerURL, installPageUrl: undefined };
  }

  // A custom-domain toolset renders its custom-domain URL or nothing: until
  // the domain resolves there is no guarantee a platform-host alias exists.
  if (toolset.customDomainId && !customServerURL) {
    return { url: undefined, customServerURL, installPageUrl: undefined };
  }

  const mcpUrl = `${customServerURL ?? getServerURL()}/mcp/${toolset.mcpSlug}`;

  // Always use our URL for install page when server is private, even for
  // custom domains to ensure cookie is present
  const installPageUrl = toolset.mcpIsPublic
    ? `${mcpUrl}/install`
    : `${getServerURL()}/mcp/${toolset.mcpSlug}/install`;

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
  toolset: Pick<ToolsetEntry, "mcpSlug"> | undefined,
): string | undefined {
  if (!toolset) return undefined;
  return internalMcpUrl(toolset);
}

/**
 * Non-hook variant of {@link useInternalMcpUrl}. Use this when the toolset is
 * already in scope (e.g. when mapping over an array of toolsets). Returns
 * `undefined` for a toolset that was never published under an MCP slug.
 */
export function internalMcpUrl(
  toolset: Pick<ToolsetEntry, "mcpSlug">,
): string | undefined {
  if (!toolset.mcpSlug) return undefined;
  return `${getServerURL()}/mcp/${toolset.mcpSlug}`;
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
