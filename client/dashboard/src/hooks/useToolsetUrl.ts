import { useProject } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";
import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { DomainDNSConfig } from "@gram/client/models/components/domaindnsconfig.js";
import { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { useListDomains } from "@gram/client/react-query/listDomains.js";
import { useCallback, useMemo } from "react";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";

export function useCustomDomain(enabled = true): {
  domain: CustomDomain | undefined;
  dnsConfig: DomainDNSConfig | undefined;
  refetch: ReturnType<typeof useListDomains>["refetch"];
  isLoading: boolean;
} {
  const { data, isLoading, refetch } = useListDomains(undefined, undefined, {
    refetchOnWindowFocus: false,
    retry: false,
    throwOnError: false,
    enabled,
  });

  return {
    domain: data?.domains[0],
    dnsConfig: data?.dnsConfig,
    refetch,
    isLoading,
  };
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
// Speakeasy-hosted `/mcp/<slug>` runtime path; custom-domain endpoints resolve
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
      // URL that points at the Speakeasy domain when the customer expected their
      // custom domain.
      return { mcpUrl: undefined, installPageUrl: undefined };
    }
    serverURL = `https://${match.domain}`;
  }

  const mcpUrl = `${serverURL}/mcp/${endpoint.slug}`;
  return { mcpUrl, installPageUrl: `${mcpUrl}/install` };
}

// Slug registered on the Speakeasy origin. Custom-domain endpoints share the slug
// column but live in another namespace, so using one of those slugs on
// getServerURL() 404s — including the first-party connect route.
export function platformEndpointSlug(
  endpoints: Array<Pick<McpEndpoint, "slug" | "customDomainId">>,
): string | undefined {
  return endpoints.find((endpoint) => endpoint.slug && !endpoint.customDomainId)
    ?.slug;
}

// Gateway install pages are session-gated and the session cookie is host-only,
// so they open on the Speakeasy origin; ?domain=custom resolves a custom-domain slug there.
export function gatewayInstallPageUrl(
  endpoints: Array<Pick<McpEndpoint, "slug" | "customDomainId">>,
): string | undefined {
  const platformSlug = platformEndpointSlug(endpoints);
  if (platformSlug) return `${getServerURL()}/mcp/${platformSlug}/install`;
  const customSlug = endpoints.find((endpoint) => endpoint.slug)?.slug;
  if (!customSlug) return undefined;
  return `${getServerURL()}/mcp/${customSlug}/install?domain=custom`;
}

// useResolvedMcpServerUrl resolves the runtime MCP URL for an mcp_server from
// its endpoints, preferring a custom-domain endpoint. While that domain is
// unresolved, it falls back only to a separately registered platform endpoint;
// custom-domain slugs are not valid on the Speakeasy origin. First-party connect
// still needs a platform slug directly — use platformEndpointSlug.
export function useResolvedMcpServerUrl(
  endpoints: McpEndpoint[],
  isLoadingEndpoints: boolean,
): {
  mcpUrl: string | undefined;
  installPageUrl: string | undefined;
  loading: boolean;
} {
  const customEndpoint = useMemo(
    () => endpoints.find((endpoint) => endpoint.customDomainId),
    [endpoints],
  );
  const platformEndpoint = useMemo(
    () => endpoints.find((endpoint) => !endpoint.customDomainId),
    [endpoints],
  );
  const { mcpUrl: customUrl } = useMcpEndpointUrl(customEndpoint);
  const { mcpUrl: platformUrl } = useMcpEndpointUrl(platformEndpoint);
  const mcpUrl = customUrl ?? platformUrl;

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

type ToolsetConnectionSource = Pick<
  ToolsetEntry,
  "id" | "slug" | "mcpSlug" | "defaultEnvironmentSlug"
> & { userSessionIssuerId?: string };

/** Resolve identity before deriving URLs or minting. Never choose from an
 * authorization-filtered list, or alias a disabled canonical route to a sibling.
 * A 404 means no wrapper exists; only that case can retain the legacy route.
 */
export function useToolsetMcpTarget(
  toolset: ToolsetConnectionSource | undefined,
): {
  url: string | undefined;
  serverId: string | undefined;
  userSessionIssuerId: string | undefined;
  legacy: boolean;
  status: "idle" | "loading" | "error" | "unavailable" | "ready";
  isLoading: boolean;
  refetch: () => void;
} {
  const project = useProject();
  const server = useGetMcpServer({ toolsetId: toolset?.id }, undefined, {
    enabled: !!toolset?.id,
    retry: false,
    throwOnError: false,
  });
  // Refetch failures may retain stale data; it must not supply a former issuer.
  const selected = toolset && !server.isError ? server.data : undefined;
  const enabledServer =
    selected?.visibility !== "disabled" ? selected : undefined;
  const legacy =
    !!toolset &&
    server.error instanceof ServiceError &&
    server.error.statusCode === 404;
  const status = !toolset
    ? "idle"
    : server.isLoading ||
        (server.isFetching &&
          (server.isError || !enabledServer?.platformEndpointSlug))
      ? "loading"
      : server.isError && !legacy
        ? "error"
        : selected?.visibility === "disabled"
          ? "unavailable"
          : enabledServer || legacy
            ? "ready"
            : "loading";
  const refetchServer = server.refetch;
  const refetch = useCallback(() => {
    if (toolset?.id)
      void refetchServer({ throwOnError: false, cancelRefetch: false });
  }, [toolset?.id, refetchServer]);
  const platformSlug = enabledServer?.platformEndpointSlug;
  // Playground/connect require the platform origin (session cookie and CSP).
  // Custom-only servers remain unavailable here; never reuse their slug there.
  const url =
    enabledServer && platformSlug
      ? `${getServerURL()}/mcp/${platformSlug}`
      : legacy
        ? internalMcpUrl(project, toolset)
        : undefined;
  return {
    url,
    serverId: enabledServer?.id,
    userSessionIssuerId:
      enabledServer?.userSessionIssuerId ??
      (legacy ? toolset.userSessionIssuerId : undefined),
    legacy,
    status,
    isLoading: status === "loading",
    refetch,
  };
}

/**
 * Formats the legacy toolset address without resolving a server identity.
 * Live playground connections use useToolsetMcpTarget instead.
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
