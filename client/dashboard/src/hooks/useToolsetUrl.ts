import { getServerURL } from "@/lib/utils";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServerVisibility } from "@gram/client/models/components/mcpserver.js";
import { useListDomains } from "@gram/client/react-query/listDomains.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
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

export type ToolsetMcpServerInfo = {
  visibility: McpServerVisibility;
  /**
   * The Gram-origin runtime URL from the wrapper server's platform-domain
   * endpoint, or undefined when no such endpoint is registered.
   */
  platformUrl: string | undefined;
};

/**
 * Server-keyed lookup for toolset surfaces: maps each toolset id to the
 * mcp_server wrapping it. When a toolset backs several servers, the one with
 * a platform-domain endpoint wins so URL consumers stay routable.
 */
export function useToolsetMcpServers(gramProject?: string): {
  byToolsetId: Map<string, ToolsetMcpServerInfo>;
  isLoading: boolean;
} {
  const request = useMemo(
    () => (gramProject ? { gramProject } : undefined),
    [gramProject],
  );
  const { data: serversData, isLoading: isLoadingServers } = useMcpServers(
    request,
    undefined,
    { throwOnError: false },
  );
  const { data: endpointsData, isLoading: isLoadingEndpoints } =
    useMcpEndpoints(request, undefined, { throwOnError: false });

  const byToolsetId = useMemo(() => {
    const platformSlugByServerId = new Map<string, string>();
    for (const endpoint of endpointsData?.mcpEndpoints ?? []) {
      if (endpoint.customDomainId) continue;
      if (!platformSlugByServerId.has(endpoint.mcpServerId)) {
        platformSlugByServerId.set(endpoint.mcpServerId, endpoint.slug);
      }
    }

    const map = new Map<string, ToolsetMcpServerInfo>();
    for (const server of serversData?.mcpServers ?? []) {
      if (!server.toolsetId) continue;
      const slug = platformSlugByServerId.get(server.id);
      const info: ToolsetMcpServerInfo = {
        visibility: server.visibility,
        platformUrl: slug ? `${getServerURL()}/mcp/${slug}` : undefined,
      };
      const existing = map.get(server.toolsetId);
      if (!existing || (!existing.platformUrl && info.platformUrl)) {
        map.set(server.toolsetId, info);
      }
    }
    return map;
  }, [serversData, endpointsData]);

  return { byToolsetId, isLoading: isLoadingServers || isLoadingEndpoints };
}

/**
 * The MCP URL for a single toolset's wrapper server that always uses the Gram
 * domain, ignoring any custom domain. Use this for internal tools like the
 * playground where we want consistent routing.
 */
export function useToolsetPlatformMcpUrl(
  toolsetId: string | undefined,
): string | undefined {
  const { byToolsetId } = useToolsetMcpServers();
  return toolsetId ? byToolsetId.get(toolsetId)?.platformUrl : undefined;
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
